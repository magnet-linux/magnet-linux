package pkgstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	syncMetadataDirectoryName = ".magpkg"
	syncManifestFilename      = "manifest.json"
	syncLockFilename          = "lock"
	syncManifestVersion       = 1
)

type syncEntryType string

const (
	syncEntryDirectory syncEntryType = "directory"
	syncEntryFile      syncEntryType = "file"
	syncEntrySymlink   syncEntryType = "symlink"
)

type syncManifest struct {
	Version  int                   `json:"version"`
	Roots    []PackageID           `json:"roots"`
	Packages []syncManifestPackage `json:"packages"`
	Entries  []syncManifestEntry   `json:"entries"`
}

type syncManifestPackage struct {
	ID   PackageID `json:"id"`
	Name string    `json:"name,omitempty"`
}

type syncManifestEntry struct {
	Path       string        `json:"path"`
	Type       syncEntryType `json:"type"`
	Mode       uint32        `json:"mode"`
	UID        uint32        `json:"uid"`
	GID        uint32        `json:"gid"`
	SHA256     string        `json:"sha256,omitempty"`
	LinkTarget string        `json:"linkTarget,omitempty"`
}

type desiredSyncEntry struct {
	Path       string
	Type       syncEntryType
	Mode       uint32
	SHA256     string
	LinkTarget string
}

func scanDesiredSyncTree(
	ctx context.Context,
	root string,
) ([]desiredSyncEntry, error) {
	var entries []desiredSyncEntry
	err := filepath.WalkDir(root, func(
		current string,
		directoryEntry fs.DirEntry,
		walkErr error,
	) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if current == root {
			return nil
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == syncMetadataDirectoryName ||
			strings.HasPrefix(relative, syncMetadataDirectoryName+"/") {
			return fmt.Errorf(
				"package closure contains reserved path %q",
				relative,
			)
		}

		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		entry := desiredSyncEntry{
			Path: relative,
			Mode: unixMode(info.Mode()),
		}
		switch {
		case info.IsDir():
			entry.Type = syncEntryDirectory
		case info.Mode().IsRegular():
			entry.Type = syncEntryFile
			entry.SHA256, err = syncFileSHA256(ctx, current)
			if err != nil {
				return fmt.Errorf("hash %q: %w", relative, err)
			}
		case info.Mode()&os.ModeSymlink != 0:
			entry.Type = syncEntrySymlink
			entry.LinkTarget, err = os.Readlink(current)
			if err != nil {
				return fmt.Errorf("read symlink %q: %w", relative, err)
			}
		default:
			return fmt.Errorf(
				"package closure path %q has unsupported type %v",
				relative,
				info.Mode(),
			)
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func syncFileSHA256(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	buffer := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			if _, err := hasher.Write(buffer[:count]); err != nil {
				return "", err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func readSyncManifest(destination string) (syncManifest, bool, error) {
	path := syncManifestPath(destination)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return syncManifest{}, false, nil
	}
	if err != nil {
		return syncManifest{}, false, err
	}

	var manifest syncManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return syncManifest{}, false, fmt.Errorf("decode sync manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected trailing JSON value")
		}
		return syncManifest{}, false, fmt.Errorf("decode sync manifest: %w", err)
	}
	if err := validateSyncManifest(manifest); err != nil {
		return syncManifest{}, false, err
	}
	return manifest, true, nil
}

func validateSyncManifest(manifest syncManifest) error {
	if manifest.Version != syncManifestVersion {
		return fmt.Errorf(
			"unsupported sync manifest version %d",
			manifest.Version,
		)
	}
	seen := make(map[string]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if err := validateSyncRelativePath(entry.Path); err != nil {
			return fmt.Errorf("invalid manifest entry %q: %w", entry.Path, err)
		}
		if _, ok := seen[entry.Path]; ok {
			return fmt.Errorf("duplicate manifest entry %q", entry.Path)
		}
		seen[entry.Path] = struct{}{}
		if entry.Mode&^uint32(0o7777) != 0 {
			return fmt.Errorf(
				"manifest entry %q has invalid mode %#o",
				entry.Path,
				entry.Mode,
			)
		}
		switch entry.Type {
		case syncEntryDirectory:
			if entry.SHA256 != "" || entry.LinkTarget != "" {
				return fmt.Errorf(
					"directory manifest entry %q has file metadata",
					entry.Path,
				)
			}
		case syncEntryFile:
			if len(entry.SHA256) != sha256.Size*2 {
				return fmt.Errorf(
					"file manifest entry %q has invalid SHA-256",
					entry.Path,
				)
			}
			if _, err := hex.DecodeString(entry.SHA256); err != nil {
				return fmt.Errorf(
					"file manifest entry %q has invalid SHA-256: %w",
					entry.Path,
					err,
				)
			}
			if entry.LinkTarget != "" {
				return fmt.Errorf(
					"file manifest entry %q has a link target",
					entry.Path,
				)
			}
		case syncEntrySymlink:
			if entry.SHA256 != "" {
				return fmt.Errorf(
					"symlink manifest entry %q has a SHA-256",
					entry.Path,
				)
			}
		default:
			return fmt.Errorf(
				"manifest entry %q has invalid type %q",
				entry.Path,
				entry.Type,
			)
		}
	}
	return nil
}

func writeSyncManifest(destination string, manifest syncManifest) (retErr error) {
	slices.SortFunc(manifest.Entries, func(a, b syncManifestEntry) int {
		return strings.Compare(a.Path, b.Path)
	})
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode sync manifest: %w", err)
	}
	data = append(data, '\n')

	metadataPath := syncMetadataPath(destination)
	temporary, err := os.CreateTemp(metadataPath, ".manifest-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	temporaryOpen := true
	defer func() {
		if temporaryOpen {
			retErr = errors.Join(retErr, temporary.Close())
		}
		if retErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	temporaryOpen = false
	if err := os.Rename(temporaryPath, syncManifestPath(destination)); err != nil {
		return err
	}
	return syncDirectory(metadataPath)
}

func syncManifestPath(destination string) string {
	return filepath.Join(
		destination,
		syncMetadataDirectoryName,
		syncManifestFilename,
	)
}

func syncMetadataPath(destination string) string {
	return filepath.Join(destination, syncMetadataDirectoryName)
}

func syncLockPath(destination string) string {
	return filepath.Join(
		destination,
		syncMetadataDirectoryName,
		syncLockFilename,
	)
}

func validateSyncRelativePath(relative string) error {
	if relative == "" {
		return errors.New("path is empty")
	}
	if filepath.IsAbs(relative) {
		return errors.New("path is absolute")
	}
	if strings.Contains(relative, `\`) {
		return errors.New("path contains a backslash")
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
	if cleaned != relative || cleaned == "." || cleaned == ".." ||
		strings.HasPrefix(cleaned, "../") {
		return errors.New("path is not a clean relative path")
	}
	if relative == syncMetadataDirectoryName ||
		strings.HasPrefix(relative, syncMetadataDirectoryName+"/") {
		return errors.New("path uses the reserved .magpkg directory")
	}
	return nil
}

func unixMode(mode os.FileMode) uint32 {
	result := uint32(mode.Perm())
	if mode&os.ModeSetuid != 0 {
		result |= 0o4000
	}
	if mode&os.ModeSetgid != 0 {
		result |= 0o2000
	}
	if mode&os.ModeSticky != 0 {
		result |= 0o1000
	}
	return result
}

func fileModeFromUnix(mode uint32) os.FileMode {
	result := os.FileMode(mode & 0o777)
	if mode&0o4000 != 0 {
		result |= os.ModeSetuid
	}
	if mode&0o2000 != 0 {
		result |= os.ModeSetgid
	}
	if mode&0o1000 != 0 {
		result |= os.ModeSticky
	}
	return result
}

func syncOwnership(info os.FileInfo) (uint32, uint32, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.New("filesystem stat has no Unix ownership")
	}
	return stat.Uid, stat.Gid, nil
}

func syncMountID(path string) (uint64, error) {
	var stat unix.Statx_t
	if err := unix.Statx(
		unix.AT_FDCWD,
		path,
		unix.AT_SYMLINK_NOFOLLOW,
		unix.STATX_MNT_ID,
		&stat,
	); err != nil {
		return 0, err
	}
	if stat.Mask&unix.STATX_MNT_ID == 0 {
		return 0, errors.New("filesystem does not report a mount ID")
	}
	return stat.Mnt_id, nil
}
