package pkgstore

import (
	"archive/tar"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

var normalizedArchiveTime = time.Unix(0, 0).UTC()

func createPackageArchive(
	ctx context.Context,
	sourceRoot string,
	destination string,
) (retErr error) {
	output, err := os.OpenFile(
		destination,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if err != nil {
		return err
	}
	outputOpen := true
	defer func() {
		if outputOpen {
			retErr = errors.Join(retErr, output.Close())
		}
	}()

	if err := writeArchive(
		ctx,
		sourceRoot,
		output,
		ExportFormatTarZstd,
	); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return fmt.Errorf("sync package archive: %w", err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close package archive: %w", err)
	}
	outputOpen = false
	return nil
}

func writeArchive(
	ctx context.Context,
	sourceRoot string,
	output io.Writer,
	format ExportFormat,
) (retErr error) {
	if err := validateExportFormat(format); err != nil {
		return err
	}
	if output == nil {
		return errors.New("archive output is nil")
	}

	archiveOutput := output
	var encoder io.WriteCloser
	var encoderName string
	switch format {
	case ExportFormatTar:
	case ExportFormatTarGzip:
		gzipEncoder := gzip.NewWriter(output)
		gzipEncoder.Header.ModTime = normalizedArchiveTime
		gzipEncoder.Header.OS = 255
		encoder = gzipEncoder
		encoderName = "gzip"
		archiveOutput = encoder
	case ExportFormatTarXZ:
		xzEncoder, err := xz.NewWriter(output)
		if err != nil {
			return fmt.Errorf("create xz encoder: %w", err)
		}
		encoder = xzEncoder
		encoderName = "xz"
		archiveOutput = encoder
	case ExportFormatTarZstd:
		zstdEncoder, err := zstd.NewWriter(
			output,
			zstd.WithEncoderConcurrency(1),
			zstd.WithEncoderLevel(zstd.SpeedDefault),
		)
		if err != nil {
			return fmt.Errorf("create zstd encoder: %w", err)
		}
		encoder = zstdEncoder
		encoderName = "zstd"
		archiveOutput = encoder
	}
	encoderOpen := encoder != nil
	defer func() {
		if encoderOpen {
			if err := encoder.Close(); err != nil {
				retErr = errors.Join(
					retErr,
					fmt.Errorf("finish %s archive: %w", encoderName, err),
				)
			}
		}
	}()

	tarWriter := tar.NewWriter(archiveOutput)
	tarOpen := true
	defer func() {
		if tarOpen {
			if err := tarWriter.Close(); err != nil {
				retErr = errors.Join(
					retErr,
					fmt.Errorf("finish tar archive: %w", err),
				)
			}
		}
	}()

	var paths []string
	err := filepath.WalkDir(sourceRoot, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current != sourceRoot {
			paths = append(paths, current)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk package output: %w", err)
	}
	sort.Strings(paths)

	for _, current := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("inspect package output %q: %w", current, err)
		}
		var linkTarget string
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err = os.Readlink(current)
			if err != nil {
				return fmt.Errorf("read package symlink %q: %w", current, err)
			}
		}
		header, err := tar.FileInfoHeader(info, linkTarget)
		if err != nil {
			return fmt.Errorf("archive package output %q: %w", current, err)
		}
		relative, err := filepath.Rel(sourceRoot, current)
		if err != nil {
			return fmt.Errorf("relativize package output %q: %w", current, err)
		}
		header.Name = filepath.ToSlash(relative)
		if info.IsDir() {
			header.Name += "/"
		}
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""
		header.ModTime = normalizedArchiveTime
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		header.PAXRecords = nil
		header.Xattrs = nil
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("write package archive header %q: %w", header.Name, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		file, err := os.Open(current)
		if err != nil {
			return fmt.Errorf("open package output %q: %w", current, err)
		}
		_, copyErr := copyContext(ctx, tarWriter, file)
		closeErr := file.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return fmt.Errorf("archive package output %q: %w", current, err)
		}
	}

	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("finish tar archive: %w", err)
	}
	tarOpen = false
	if encoder != nil {
		if err := encoder.Close(); err != nil {
			return fmt.Errorf("finish %s archive: %w", encoderName, err)
		}
		encoderOpen = false
	}
	return nil
}

// validatePackageArchive completely decodes a package archive and checks the
// same path and entry-type boundary enforced during extraction. Binary caches
// are trusted for artifact contents, but malformed or truncated artifacts are
// never published into the local store.
func validatePackageArchive(ctx context.Context, archivePath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	buffered := bufio.NewReader(file)
	magic, _ := buffered.Peek(4)
	if len(magic) < 4 ||
		magic[0] != 0x28 || magic[1] != 0xb5 ||
		magic[2] != 0x2f || magic[3] != 0xfd {
		return errors.New("package artifact is not a zstd archive")
	}
	decoder, err := zstd.NewReader(buffered)
	if err != nil {
		return fmt.Errorf("open package zstd archive: %w", err)
	}
	defer decoder.Close()

	reader := tar.NewReader(decoder)
	paths := make(map[string]struct{})
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read package archive: %w", err)
		}
		relative, skip, err := cleanArchiveMember(header.Name)
		if err != nil {
			return fmt.Errorf("invalid package archive path %q: %w", header.Name, err)
		}
		if skip {
			continue
		}
		if _, exists := paths[relative]; exists {
			return fmt.Errorf("duplicate package archive path %q", header.Name)
		}
		paths[relative] = struct{}{}

		switch header.Typeflag {
		case tar.TypeDir, tar.TypeSymlink:
		case tar.TypeReg:
			written, err := copyContext(
				ctx,
				io.Discard,
				io.LimitReader(reader, header.Size),
			)
			if err != nil {
				return fmt.Errorf(
					"read package archive entry %q: %w",
					header.Name,
					err,
				)
			}
			if written != header.Size {
				return fmt.Errorf(
					"package archive entry %q is truncated",
					header.Name,
				)
			}
		case tar.TypeLink:
			if _, skip, err := cleanArchiveMember(header.Linkname); err != nil || skip {
				if err == nil {
					err = errors.New("hard link target is the archive root")
				}
				return fmt.Errorf(
					"invalid package archive hard link %q: %w",
					header.Linkname,
					err,
				)
			}
		case tar.TypeXGlobalHeader:
		default:
			return fmt.Errorf(
				"unsupported package archive entry type %d",
				header.Typeflag,
			)
		}
	}
}

func extractArchive(ctx context.Context, archivePath string, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	buffered := bufio.NewReader(file)
	magic, _ := buffered.Peek(6)
	var reader io.Reader = buffered
	var closeReader func()
	switch {
	case len(magic) >= 4 &&
		magic[0] == 0x28 && magic[1] == 0xb5 &&
		magic[2] == 0x2f && magic[3] == 0xfd:
		decoder, err := zstd.NewReader(buffered)
		if err != nil {
			return fmt.Errorf("open zstd archive: %w", err)
		}
		reader = decoder
		closeReader = decoder.Close
	case len(magic) >= 2 && magic[0] == 0x1f && magic[1] == 0x8b:
		decoder, err := gzip.NewReader(buffered)
		if err != nil {
			return fmt.Errorf("open gzip archive: %w", err)
		}
		reader = decoder
		closeReader = func() {
			_ = decoder.Close()
		}
	case len(magic) >= 3 && string(magic[:3]) == "BZh":
		reader = bzip2.NewReader(buffered)
	case len(magic) >= 6 &&
		magic[0] == 0xfd && magic[1] == 0x37 &&
		magic[2] == 0x7a && magic[3] == 0x58 &&
		magic[4] == 0x5a && magic[5] == 0x00:
		decoder, err := xz.NewReader(buffered)
		if err != nil {
			return fmt.Errorf("open xz archive: %w", err)
		}
		reader = decoder
	}
	if closeReader != nil {
		defer closeReader()
	}

	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	tarReader := tar.NewReader(reader)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		if err := extractArchiveEntry(ctx, destination, header, tarReader); err != nil {
			return fmt.Errorf("extract %q: %w", header.Name, err)
		}
	}
}

func extractArchiveEntry(
	ctx context.Context,
	root string,
	header *tar.Header,
	reader io.Reader,
) error {
	relative, skip, err := cleanArchiveMember(header.Name)
	if err != nil || skip {
		return err
	}
	target := filepath.Join(root, filepath.FromSlash(relative))
	if err := ensureArchiveParents(root, filepath.Dir(relative)); err != nil {
		return err
	}

	switch header.Typeflag {
	case tar.TypeDir:
		if err := replaceNonDirectory(target); err != nil {
			return err
		}
		if err := os.Mkdir(target, header.FileInfo().Mode().Perm()); err != nil &&
			!errors.Is(err, os.ErrExist) {
			return err
		}
		return os.Chmod(target, header.FileInfo().Mode())

	case tar.TypeReg:
		if err := removeArchiveTarget(target); err != nil {
			return err
		}
		output, err := os.OpenFile(
			target,
			os.O_WRONLY|os.O_CREATE|os.O_EXCL,
			header.FileInfo().Mode().Perm(),
		)
		if err != nil {
			return err
		}
		_, copyErr := copyContext(ctx, output, io.LimitReader(reader, header.Size))
		chmodErr := output.Chmod(header.FileInfo().Mode())
		closeErr := output.Close()
		return errors.Join(copyErr, chmodErr, closeErr)

	case tar.TypeSymlink:
		if err := removeArchiveTarget(target); err != nil {
			return err
		}
		// Absolute symlink targets are valid inside a package root (for
		// example /lib/ld-musl-x86_64.so.1 -> /lib/libc.so). Creating the
		// symlink does not follow it.
		return os.Symlink(header.Linkname, target)

	case tar.TypeLink:
		linkRelative, skip, err := cleanArchiveMember(header.Linkname)
		if err != nil {
			return fmt.Errorf("invalid hard link target: %w", err)
		}
		if skip {
			return errors.New("hard link target is the archive root")
		}
		if err := ensureArchiveParents(root, path.Dir(linkRelative)); err != nil {
			return fmt.Errorf("invalid hard link target: %w", err)
		}
		linkTarget := filepath.Join(root, filepath.FromSlash(linkRelative))
		info, err := os.Lstat(linkTarget)
		if err != nil {
			return fmt.Errorf("inspect hard link target: %w", err)
		}
		if !info.Mode().IsRegular() {
			return errors.New("hard link target is not a regular file")
		}
		if err := removeArchiveTarget(target); err != nil {
			return err
		}
		return os.Link(linkTarget, target)

	case tar.TypeXGlobalHeader:
		return nil

	default:
		return fmt.Errorf("unsupported archive entry type %d", header.Typeflag)
	}
}

func cleanArchiveMember(name string) (relative string, skip bool, err error) {
	name = strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(name, "/") {
		return "", false, errors.New("absolute archive path")
	}
	cleaned := path.Clean(name)
	if cleaned == "." {
		return "", true, nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false, errors.New("archive path escapes its destination")
	}
	return cleaned, false, nil
}

func ensureArchiveParents(root string, relativeDirectory string) error {
	if relativeDirectory == "." || relativeDirectory == "" {
		return nil
	}
	current := root
	for _, component := range strings.Split(filepath.FromSlash(relativeDirectory), string(os.PathSeparator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := os.Mkdir(current, 0o755); err != nil {
				return err
			}
		case err != nil:
			return err
		case !info.IsDir():
			return fmt.Errorf("archive parent %q is not a directory", component)
		}
	}
	return nil
}

func replaceNonDirectory(target string) error {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return nil
	}
	return os.Remove(target)
}

func removeArchiveTarget(target string) error {
	_, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.RemoveAll(target)
}
