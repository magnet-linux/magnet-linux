package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"

	graphjsonnet "github.com/andrewchambers/magnet-linux/magpkg/pkggraph/jsonnet"
	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
	"github.com/spf13/pflag"
)

func runSync(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	flags := pflag.NewFlagSet("sync", pflag.ContinueOnError)
	flags.SetOutput(stderr)
	storeFlag := flags.String(
		"store",
		"",
		"package store path (default: $MAGPKG_STORE or ~/.magpkg/store)",
	)
	binaryCache := flags.String(
		"binary-cache",
		"",
		"binary cache URL (default: $MAGPKG_BINARY_CACHE)",
	)
	noBinaryCache := flags.Bool(
		"no-binary-cache",
		false,
		"do not request missing artifacts from a binary cache",
	)
	destination := flags.String(
		"dest",
		"",
		"destination root to synchronize (required)",
	)
	jsonnetFileFlag := flags.StringP(
		"file",
		"f",
		"",
		"Jsonnet package graph file (may instead be the first positional argument)",
	)
	dryRun := flags.Bool(
		"dry-run",
		false,
		"plan and verify the synchronization without changing managed files",
	)
	noBuild := flags.Bool(
		"no-build",
		false,
		"prohibit source fetching and local builds",
	)
	force := flags.Bool(
		"force",
		false,
		"discard modified managed paths and replace unmanaged collisions",
	)
	deleteUnmanaged := flags.Bool(
		"delete-unmanaged",
		false,
		"delete paths outside the desired closure (requires --force)",
	)
	jobs := flags.Int(
		"jobs",
		1,
		"maximum number of package builds run concurrently",
	)
	fetchJobs := flags.Int(
		"fetch-jobs",
		pkgstore.DefaultFetchJobs,
		"maximum number of source fetches run concurrently",
	)
	parallelism := flags.Int(
		"parallelism",
		runtime.NumCPU(),
		"value exposed to build scripts as BUILD_PARALLELISM",
	)
	var extStrings stringMapFlag
	flags.Var(
		&extStrings,
		"ext-str",
		"bind a Jsonnet external string as NAME=VALUE (repeatable)",
	)
	flags.Usage = func() {
		fmt.Fprintln(
			stderr,
			"usage: magpkg sync --dest PATH [options] FILE [ROOT ...]",
		)
		fmt.Fprintln(
			stderr,
			"       magpkg sync --dest PATH [options] --file FILE [ROOT ...]",
		)
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(
			stderr,
			"Synchronizes magpkg-managed paths with the selected roots' runtime closure.",
		)
		fmt.Fprintln(
			stderr,
			"When ROOT is omitted, every exported root is selected.",
		)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return nil
		}
		return err
	}
	if *destination == "" {
		flags.Usage()
		return errors.New("sync requires --dest")
	}
	if *jobs < 1 {
		return errors.New("--jobs must be at least one")
	}
	if *fetchJobs < 1 {
		return errors.New("--fetch-jobs must be at least one")
	}
	if *parallelism < 1 {
		return errors.New("--parallelism must be at least one")
	}
	if *deleteUnmanaged && !*force {
		return errors.New("--delete-unmanaged requires --force")
	}
	positional := flags.Args()
	jsonnetFile := *jsonnetFileFlag
	if jsonnetFile == "" {
		if len(positional) == 0 {
			flags.Usage()
			return errors.New("sync requires a Jsonnet file")
		}
		jsonnetFile = positional[0]
		positional = positional[1:]
	}
	if jsonnetFile == "" {
		return errors.New("Jsonnet filename is empty")
	}

	storePath, err := resolveStorePath(*storeFlag)
	if err != nil {
		return err
	}
	store, err := pkgstore.OpenWithOptions(
		storePath,
		commandStoreOpenOptions(resolveBinaryCacheURL(
			*binaryCache,
			*noBinaryCache,
		)),
	)
	if err != nil {
		return fmt.Errorf(
			"open store %q (run `magpkg init` first): %w",
			storePath,
			err,
		)
	}
	graph, err := graphjsonnet.EvaluateFile(
		ctx,
		jsonnetFile,
		graphjsonnet.Options{ExtVars: extStrings},
	)
	if err != nil {
		return err
	}
	rootIDs, err := selectBuildRoots(graph, positional)
	if err != nil {
		return err
	}

	fetchProgress := newFetchProgressRenderer(ctx, stderr)
	_, buildErr := store.BuildPackagesWithOptions(
		ctx,
		graph,
		rootIDs,
		pkgstore.BuildOptions{
			NoBuild:          *noBuild,
			Jobs:             *jobs,
			FetchJobs:        *fetchJobs,
			BuildParallelism: *parallelism,
			Stdout:           stderr,
			Stderr:           stderr,
			FetchProgress:    fetchProgress.Report,
		},
	)
	fetchProgress.Finish()
	if buildErr != nil {
		return buildErr
	}

	result, err := store.Sync(
		ctx,
		graph,
		rootIDs,
		*destination,
		pkgstore.SyncOptions{
			DryRun:          *dryRun,
			Force:           *force,
			DeleteUnmanaged: *deleteUnmanaged,
		},
	)
	if err != nil {
		return err
	}
	writeDestinationChanges(stdout, result)
	return nil
}

func writeDestinationChanges(writer io.Writer, result *pkgstore.DestinationResult) {
	if result == nil {
		return
	}
	prefix := ""
	if result.DryRun {
		prefix = "would "
	}
	for _, change := range result.Changes {
		fmt.Fprintf(writer, "%s%s %s\n", prefix, change.Action, change.Path)
	}
}
