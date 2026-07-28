package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strings"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
	graphjsonnet "github.com/andrewchambers/magnet-linux/magpkg/pkggraph/jsonnet"
	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
	"github.com/spf13/pflag"
)

func runBuild(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	flags := pflag.NewFlagSet("build", pflag.ContinueOnError)
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
	noBuild := flags.Bool(
		"no-build",
		false,
		"prohibit source fetching and local builds",
	)
	jsonnetFileFlag := flags.StringP(
		"file",
		"f",
		"",
		"Jsonnet package graph file (may instead be the first positional argument)",
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
			"usage: magpkg build [options] FILE [ROOT ...]",
		)
		fmt.Fprintln(
			stderr,
			"       magpkg build [options] --file FILE [ROOT ...]",
		)
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(
			stderr,
			"ROOT is an exported Jsonnet field name or a package ID.",
		)
		fmt.Fprintln(
			stderr,
			"When omitted, every exported root is built.",
		)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return nil
		}
		return err
	}

	positional := flags.Args()
	jsonnetFile := *jsonnetFileFlag
	if jsonnetFile == "" {
		if len(positional) == 0 {
			flags.Usage()
			return errors.New("build requires a Jsonnet file")
		}
		jsonnetFile = positional[0]
		positional = positional[1:]
	}
	if jsonnetFile == "" {
		return errors.New("Jsonnet filename is empty")
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
	packages, buildErr := store.BuildPackagesWithOptions(
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
	for _, pkg := range packages {
		fmt.Fprintln(stdout, pkg.StorePath)
	}
	return nil
}

func selectBuildRoots(
	graph *pkggraph.Graph,
	selectors []string,
) ([]pkggraph.ID, error) {
	if graph == nil {
		return nil, errors.New("package graph is nil")
	}
	if len(selectors) == 0 {
		rootNames := make([]string, 0, len(graph.Roots))
		for rootName := range graph.Roots {
			rootNames = append(rootNames, rootName)
		}
		slices.Sort(rootNames)
		if len(rootNames) == 0 {
			return nil, errors.New("package graph exports no roots")
		}
		rootIDs := make([]pkggraph.ID, 0, len(rootNames))
		seen := make(map[pkggraph.ID]struct{}, len(rootNames))
		for _, rootName := range rootNames {
			packageID := graph.Roots[rootName]
			if _, ok := seen[packageID]; ok {
				continue
			}
			seen[packageID] = struct{}{}
			rootIDs = append(rootIDs, packageID)
		}
		return rootIDs, nil
	}

	rootIDs := make([]pkggraph.ID, 0, len(selectors))
	seen := make(map[pkggraph.ID]struct{}, len(selectors))
	for _, selector := range selectors {
		packageID, ok := graph.Roots[selector]
		if !ok {
			packageID = pkggraph.ID(selector)
			_, ok = graph.Nodes[packageID]
		}
		if !ok {
			available := make([]string, 0, len(graph.Roots))
			for rootName := range graph.Roots {
				available = append(available, rootName)
			}
			slices.Sort(available)
			return nil, fmt.Errorf(
				"unknown package root %q (available roots: %s)",
				selector,
				strings.Join(available, ", "),
			)
		}
		if _, ok := seen[packageID]; ok {
			continue
		}
		seen[packageID] = struct{}{}
		rootIDs = append(rootIDs, packageID)
	}
	return rootIDs, nil
}
