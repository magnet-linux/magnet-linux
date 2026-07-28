package shellenv

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/andrewchambers/magnet-linux/magpkg/internal/filelock"
)

// CleanKind identifies the kind of environment state removed by cleanup.
type CleanKind string

const (
	CleanKindEnvironment CleanKind = "environment"
	CleanKindWorkspace   CleanKind = "environment-workspace"
)

// CleanEvent describes one environment entry removed by cleanup.
type CleanEvent struct {
	Kind CleanKind
	Path string
}

// CleanOptions controls cleanup reporting.
type CleanOptions struct {
	OnRemove func(CleanEvent)
}

// Clean removes complete environments and abandoned workspaces unused within
// maxAge. Active environments and materializations are skipped. A maxAge of
// zero removes all idle recognized entries.
func Clean(
	ctx context.Context,
	environmentsPath string,
	maxAge time.Duration,
	options CleanOptions,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if environmentsPath == "" {
		return errors.New("shellenv: environments path is empty")
	}
	if maxAge < 0 {
		return errors.New("shellenv: cleanup max age must not be negative")
	}
	absolutePath, err := filepath.Abs(environmentsPath)
	if err != nil {
		return fmt.Errorf("shellenv: resolve environments path: %w", err)
	}
	if _, err := os.Lstat(absolutePath); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := validateRoot(absolutePath); err != nil {
		return fmt.Errorf("shellenv: open environments metadata: %w", err)
	}

	ids := make(map[string]struct{})
	for _, directory := range []struct {
		name   string
		locked bool
	}{
		{name: rootsDirectory},
		{name: locksDirectory, locked: true},
		{name: workDirectory},
	} {
		entries, err := readDirectory(filepath.Join(absolutePath, directory.name))
		if err != nil {
			return fmt.Errorf(
				"shellenv: read %s directory: %w",
				directory.name,
				err,
			)
		}
		for _, entry := range entries {
			name := entry.Name()
			if directory.locked {
				if !strings.HasSuffix(name, lockSuffix) {
					continue
				}
				name = strings.TrimSuffix(name, lockSuffix)
			}
			if validEnvironmentID(name) {
				ids[name] = struct{}{}
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	lockRoot := filepath.Join(absolutePath, locksDirectory)
	if err := os.MkdirAll(lockRoot, 0o755); err != nil {
		return fmt.Errorf("shellenv: create locks directory: %w", err)
	}

	ordered := make([]string, 0, len(ids))
	for environmentID := range ids {
		ordered = append(ordered, environmentID)
	}
	sort.Strings(ordered)
	removeAll := maxAge == 0
	cutoff := time.Now().Add(-maxAge)
	for _, environmentID := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		lock, busy, err := filelock.TryExclusive(
			ctx,
			environmentLockPath(absolutePath, environmentID),
		)
		if err != nil {
			return fmt.Errorf(
				"shellenv: lock environment %s for cleanup: %w",
				environmentID,
				err,
			)
		}
		if busy {
			continue
		}

		finalPath := environmentPath(absolutePath, environmentID)
		removedEnvironment, environmentErr := removeExpiredEnvironment(
			finalPath,
			cutoff,
			removeAll,
		)
		if removedEnvironment {
			reportCleanEvent(options.OnRemove, CleanEvent{
				Kind: CleanKindEnvironment,
				Path: finalPath,
			})
		}
		workPath := environmentWorkPath(absolutePath, environmentID)
		removedWork, workErr := removeExpiredDirectory(
			workPath,
			cutoff,
			removeAll,
		)
		if removedWork {
			reportCleanEvent(options.OnRemove, CleanEvent{
				Kind: CleanKindWorkspace,
				Path: workPath,
			})
		}
		unlinkErr := lock.Unlink()
		closeErr := lock.Close()
		if err := errors.Join(
			environmentErr,
			workErr,
			unlinkErr,
			closeErr,
		); err != nil {
			return fmt.Errorf(
				"shellenv: clean environment %s: %w",
				environmentID,
				err,
			)
		}
	}
	return nil
}

func validEnvironmentID(environmentID string) bool {
	if len(environmentID) != hex.EncodedLen(sha256Size) {
		return false
	}
	decoded, err := hex.DecodeString(environmentID)
	return err == nil && hex.EncodeToString(decoded) == environmentID
}

const sha256Size = 32

func readDirectory(path string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return entries, err
}

func removeExpiredEnvironment(
	path string,
	cutoff time.Time,
	removeAll bool,
) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	ageInfo := info
	metadataInfo, metadataErr := os.Lstat(
		filepath.Join(path, environmentMetadataFilename),
	)
	if metadataErr == nil && metadataInfo.Mode().IsRegular() {
		ageInfo = metadataInfo
	} else if metadataErr != nil && !errors.Is(metadataErr, os.ErrNotExist) {
		return false, metadataErr
	}
	if !info.IsDir() || !removeAll && ageInfo.ModTime().After(cutoff) {
		return false, nil
	}
	if err := os.RemoveAll(path); err != nil {
		return false, err
	}
	return true, nil
}

func removeExpiredDirectory(
	path string,
	cutoff time.Time,
	removeAll bool,
) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || !removeAll && info.ModTime().After(cutoff) {
		return false, nil
	}
	if err := os.RemoveAll(path); err != nil {
		return false, err
	}
	return true, nil
}

func reportCleanEvent(callback func(CleanEvent), event CleanEvent) {
	if callback != nil {
		callback(event)
	}
}
