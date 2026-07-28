package pkgstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	tempDirectoryName = "tmp"
	tempLockSuffix    = ".lock"
)

// tempLease is an exclusively locked temporary directory under a store's tmp
// root.
//
// The lock is created and acquired before the directory exists and unlinked
// after the directory is removed, so a temporary path only ever exists while
// its lock is held by its owner or by cleanup. Lock acquisition verifies the
// locked inode is still current, so a cleanup racing with acquisition cannot
// leave the path unprotected.
type tempLease struct {
	path string
	lock *fileLock
}

// acquireTempDir reserves a locked temporary directory under storePath's tmp
// root. Callers must Close the lease to remove the directory and its lock.
func acquireTempDir(
	ctx context.Context,
	storePath string,
	prefix string,
) (*tempLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root := filepath.Join(storePath, tempDirectoryName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create store temporary root: %w", err)
	}
	reservation, err := os.CreateTemp(root, prefix+"*"+tempLockSuffix)
	if err != nil {
		return nil, fmt.Errorf("reserve temporary path: %w", err)
	}
	lockPath := reservation.Name()
	if err := reservation.Close(); err != nil {
		return nil, fmt.Errorf("reserve temporary path: %w", err)
	}
	lock, err := acquireLock(ctx, lockPath)
	if err != nil {
		return nil, fmt.Errorf("lock temporary path: %w", err)
	}
	path := strings.TrimSuffix(lockPath, tempLockSuffix)
	if err := os.RemoveAll(path); err != nil {
		return nil, errors.Join(
			fmt.Errorf("remove stale temporary path: %w", err),
			lock.Unlink(),
			lock.Close(),
		)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return nil, errors.Join(
			fmt.Errorf("create temporary directory: %w", err),
			lock.Unlink(),
			lock.Close(),
		)
	}
	return &tempLease{path: path, lock: lock}, nil
}

// Close removes the temporary directory and then its lock. The lock file is
// unlinked while the exclusive lock is still held.
func (lease *tempLease) Close() error {
	removeErr := os.RemoveAll(lease.path)
	unlinkErr := lease.lock.Unlink()
	closeErr := lease.lock.Close()
	return errors.Join(removeErr, unlinkErr, closeErr)
}

// CleanTempFiles removes temporary paths abandoned by interrupted operations.
//
// Live temporary paths are protected by an exclusive lock held by their
// owner, so any path whose lock can be acquired is abandoned and is removed
// regardless of age. For each abandoned path, its lock file is unlinked last
// while the exclusive lock is still held.
func (s *Store) CleanTempFiles(ctx context.Context, options CleanOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateStore(s); err != nil {
		return err
	}

	root := filepath.Join(s.path, tempDirectoryName)
	entries, err := readDirectoryEntries(root)
	if err != nil {
		return fmt.Errorf("read temporary paths: %w", err)
	}
	names := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		names[strings.TrimSuffix(entry.Name(), tempLockSuffix)] = struct{}{}
	}
	orderedNames := make([]string, 0, len(names))
	for name := range names {
		orderedNames = append(orderedNames, name)
	}
	sort.Strings(orderedNames)

	for _, name := range orderedNames {
		if err := ctx.Err(); err != nil {
			return err
		}

		path := filepath.Join(root, name)
		lock, busy, err := tryLock(ctx, path+tempLockSuffix)
		if err != nil {
			return fmt.Errorf("lock temporary path %s for cleanup: %w", name, err)
		}
		if busy {
			continue
		}

		var removeErr error
		removed := false
		if _, err := os.Lstat(path); err == nil {
			removeErr = os.RemoveAll(path)
			removed = removeErr == nil
		} else if !errors.Is(err, os.ErrNotExist) {
			removeErr = err
		}
		if removed {
			reportCleanEvent(options.OnRemove, CleanEvent{
				Kind: CleanKindTemp,
				Path: path,
			})
		}

		unlinkErr := lock.Unlink()
		closeErr := lock.Close()
		if err := errors.Join(removeErr, unlinkErr, closeErr); err != nil {
			return fmt.Errorf("clean up temporary path %s: %w", name, err)
		}
	}
	return nil
}
