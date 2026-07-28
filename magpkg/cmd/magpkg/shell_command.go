package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"

	graphjsonnet "github.com/andrewchambers/magnet-linux/magpkg/pkggraph/jsonnet"
	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
	"github.com/andrewchambers/magnet-linux/magpkg/shellenv"
	"github.com/spf13/pflag"
)

func runShell(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) (retErr error) {
	flags := pflag.NewFlagSet("shell", pflag.ContinueOnError)
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
			"usage: magpkg shell [options] FILE [ROOT ...] [-- COMMAND [ARG ...]]",
		)
		fmt.Fprintln(
			stderr,
			"       magpkg shell [options] --file FILE [ROOT ...] [-- COMMAND [ARG ...]]",
		)
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(
			stderr,
			"Realizes and enters a package-defined shell environment.",
		)
		fmt.Fprintln(
			stderr,
			"When COMMAND is omitted, the package launcher selects an interactive shell.",
		)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return nil
		}
		return err
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

	positional := flags.Args()
	rootsEnd := len(positional)
	var command []string
	if atDash := flags.ArgsLenAtDash(); atDash >= 0 {
		rootsEnd = atDash
		command = positional[atDash:]
	}
	rootArguments := positional[:rootsEnd]
	jsonnetFile := *jsonnetFileFlag
	if jsonnetFile == "" {
		if len(rootArguments) == 0 {
			flags.Usage()
			return errors.New("shell requires a Jsonnet file")
		}
		jsonnetFile = rootArguments[0]
		rootArguments = rootArguments[1:]
	}
	if jsonnetFile == "" {
		return errors.New("Jsonnet filename is empty")
	}

	graph, err := graphjsonnet.EvaluateFile(
		ctx,
		jsonnetFile,
		graphjsonnet.Options{ExtVars: extStrings},
	)
	if err != nil {
		return err
	}
	rootIDs, err := selectBuildRoots(graph, rootArguments)
	if err != nil {
		return err
	}

	environmentsPath, err := resolveEnvironmentsPath()
	if err != nil {
		return err
	}
	environment, err := shellenv.Open(
		ctx,
		environmentsPath,
		graph,
		rootIDs,
	)
	if err != nil && !errors.Is(err, shellenv.ErrNotFound) {
		return err
	}
	if errors.Is(err, shellenv.ErrNotFound) {
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
		environment, err = shellenv.Acquire(
			ctx,
			environmentsPath,
			store,
			graph,
			rootIDs,
		)
		if err != nil {
			return err
		}
	}
	defer func() {
		retErr = errors.Join(retErr, environment.Close())
	}()

	workingDirectory, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determine working directory: %w", err)
	}
	return shellenv.Run(
		// Once the launcher owns the terminal, terminal-generated signals are
		// delivered directly to it and its children. Do not let the command's
		// already-notified parent context turn an interactive Ctrl-C into an
		// unconditional launcher kill.
		context.WithoutCancel(ctx),
		environment,
		command,
		shellenv.RunOptions{
			Stdin:  stdin,
			Stdout: stdout,
			Stderr: stderr,
			Env:    os.Environ(),
			Dir:    workingDirectory,
		},
	)
}
