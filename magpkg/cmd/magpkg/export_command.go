package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	graphjsonnet "github.com/andrewchambers/magnet-linux/magpkg/pkggraph/jsonnet"
	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
	"github.com/spf13/pflag"
)

func runExport(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	flags := pflag.NewFlagSet("export", pflag.ContinueOnError)
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
	output := flags.StringP(
		"output",
		"o",
		"",
		"output archive path, or - for standard output (required)",
	)
	formatFlag := flags.String(
		"format",
		"",
		"archive format: tar, tar.gz, tar.xz, or tar.zst (inferred from output path)",
	)
	jsonnetFileFlag := flags.StringP(
		"file",
		"f",
		"",
		"Jsonnet package graph file (may instead be the first positional argument)",
	)
	noBuild := flags.Bool(
		"no-build",
		false,
		"prohibit source fetching and local builds",
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
			"usage: magpkg export --output ARCHIVE [options] FILE [ROOT ...]",
		)
		fmt.Fprintln(
			stderr,
			"       magpkg export --output ARCHIVE [options] --file FILE [ROOT ...]",
		)
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(
			stderr,
			"Exports the selected roots' runtime closures as one tar archive.",
		)
		fmt.Fprintln(
			stderr,
			"Use --output - with an explicit --format to write the archive to stdout.",
		)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return nil
		}
		return err
	}
	if *output == "" {
		flags.Usage()
		return errors.New("export requires --output")
	}
	format, err := resolveExportFormat(*output, *formatFlag)
	if err != nil {
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
	if *output != "-" {
		if _, err := os.Lstat(*output); err == nil {
			return fmt.Errorf("export output %q already exists", *output)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect export output %q: %w", *output, err)
		}
	}

	positional := flags.Args()
	jsonnetFile := *jsonnetFileFlag
	if jsonnetFile == "" {
		if len(positional) == 0 {
			flags.Usage()
			return errors.New("export requires a Jsonnet file")
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

	export := func(writer io.Writer) error {
		_, err := store.ExportArchive(
			ctx,
			graph,
			rootIDs,
			writer,
			pkgstore.ExportOptions{Format: format},
		)
		return err
	}
	if *output == "-" {
		return export(stdout)
	}
	return writeExportFile(*output, export)
}

func resolveExportFormat(
	output string,
	explicit string,
) (pkgstore.ExportFormat, error) {
	if explicit != "" {
		switch explicit {
		case string(pkgstore.ExportFormatTar):
			return pkgstore.ExportFormatTar, nil
		case string(pkgstore.ExportFormatTarGzip):
			return pkgstore.ExportFormatTarGzip, nil
		case string(pkgstore.ExportFormatTarXZ):
			return pkgstore.ExportFormatTarXZ, nil
		case string(pkgstore.ExportFormatTarZstd):
			return pkgstore.ExportFormatTarZstd, nil
		default:
			return "", fmt.Errorf(
				"unsupported export format %q "+
					"(want tar, tar.gz, tar.xz, or tar.zst)",
				explicit,
			)
		}
	}
	if output == "-" {
		return "", errors.New(
			"export to standard output requires an explicit --format",
		)
	}
	lower := strings.ToLower(output)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"),
		strings.HasSuffix(lower, ".tgz"):
		return pkgstore.ExportFormatTarGzip, nil
	case strings.HasSuffix(lower, ".tar.xz"),
		strings.HasSuffix(lower, ".txz"):
		return pkgstore.ExportFormatTarXZ, nil
	case strings.HasSuffix(lower, ".tar.zst"):
		return pkgstore.ExportFormatTarZstd, nil
	case strings.HasSuffix(lower, ".tar"):
		return pkgstore.ExportFormatTar, nil
	default:
		return "", fmt.Errorf(
			"cannot infer export format from %q; use --format",
			output,
		)
	}
}

func writeExportFile(
	destination string,
	write func(io.Writer) error,
) (retErr error) {
	directory := filepath.Dir(destination)
	temporary, err := os.CreateTemp(
		directory,
		"."+filepath.Base(destination)+"-",
	)
	if err != nil {
		return fmt.Errorf("create temporary export archive: %w", err)
	}
	temporaryPath := temporary.Name()
	temporaryOpen := true
	defer func() {
		if temporaryOpen {
			retErr = errors.Join(retErr, temporary.Close())
		}
		if err := os.Remove(temporaryPath); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			retErr = errors.Join(
				retErr,
				fmt.Errorf("remove temporary export archive: %w", err),
			)
		}
	}()

	if err := write(temporary); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync export archive: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close export archive: %w", err)
	}
	temporaryOpen = false

	if err := os.Link(temporaryPath, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("export output %q already exists", destination)
		}
		return fmt.Errorf("publish export archive: %w", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		return fmt.Errorf("remove temporary export archive: %w", err)
	}
	if err := syncCommandDirectory(directory); err != nil {
		return fmt.Errorf("sync export output directory: %w", err)
	}
	return nil
}

func syncCommandDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
