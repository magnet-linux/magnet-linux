package pkgstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

// ExportFormat identifies the tar compression used by ExportArchive.
type ExportFormat string

const (
	ExportFormatTar     ExportFormat = "tar"
	ExportFormatTarGzip ExportFormat = "tar.gz"
	ExportFormatTarXZ   ExportFormat = "tar.xz"
	ExportFormatTarZstd ExportFormat = "tar.zst"
)

// ExportOptions controls runtime-closure archive creation.
type ExportOptions struct {
	// Format is the required tar compression format.
	Format ExportFormat
}

// ExportArchive composes roots' runtime closure and writes it as a
// deterministic tar archive. Every artifact in the closure must already exist
// in the store. Dependencies are extracted before dependants, so later
// packages overlay earlier packages in the same order as ExtractRuntimeClosure.
// ExportArchive does not close output.
func (s *Store) ExportArchive(
	ctx context.Context,
	graph *pkggraph.Graph,
	roots []pkggraph.ID,
	output io.Writer,
	options ExportOptions,
) (closure []pkggraph.ID, retErr error) {
	if err := validateStore(s); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if graph == nil {
		return nil, errors.New("pkgstore: package graph is nil")
	}
	if output == nil {
		return nil, errors.New("pkgstore: export output is nil")
	}
	if err := validateExportFormat(options.Format); err != nil {
		return nil, err
	}

	staging, err := os.MkdirTemp("", "magpkg-export-")
	if err != nil {
		return nil, fmt.Errorf("create export staging root: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(staging); err != nil {
			retErr = errors.Join(
				retErr,
				fmt.Errorf("remove export staging root: %w", err),
			)
		}
	}()

	closure, err = s.ExtractRuntimeClosure(ctx, graph, roots, staging)
	if err != nil {
		return nil, fmt.Errorf("compose export closure: %w", err)
	}
	if err := writeArchive(ctx, staging, output, options.Format); err != nil {
		return nil, fmt.Errorf("write export archive: %w", err)
	}
	return closure, nil
}

func validateExportFormat(format ExportFormat) error {
	switch format {
	case ExportFormatTar,
		ExportFormatTarGzip,
		ExportFormatTarXZ,
		ExportFormatTarZstd:
		return nil
	default:
		return fmt.Errorf("pkgstore: unsupported export format %q", format)
	}
}
