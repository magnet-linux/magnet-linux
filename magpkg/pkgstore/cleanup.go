package pkgstore

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
)

// CleanupFetches removes cached sources and partial downloads that have not
// been used within maxAge.
//
// Cleanup skips active fetches rather than waiting for their locks. For each
// idle checksum, its lock file is unlinked last while the exclusive lock is
// still held.
func (s *Store) CleanupFetches(ctx context.Context, maxAge time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.path == "" {
		return errors.New("pkgstore: store is not initialized")
	}
	if maxAge < 0 {
		return errors.New("pkgstore: fetch cleanup max age must not be negative")
	}

	fetchRoot := filepath.Join(s.path, fetchDirectoryName)
	entries, err := os.ReadDir(fetchRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read fetch cache: %w", err)
	}

	digests := make(map[string]struct{})
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if digest, ok := fetchCacheEntryDigest(entry.Name()); ok {
			digests[digest] = struct{}{}
		}
	}

	orderedDigests := make([]string, 0, len(digests))
	for digest := range digests {
		orderedDigests = append(orderedDigests, digest)
	}
	sort.Strings(orderedDigests)

	cutoff := time.Now().Add(-maxAge)
	for _, digest := range orderedDigests {
		if err := ctx.Err(); err != nil {
			return err
		}

		destination := filepath.Join(fetchRoot, digest)
		lock, busy, err := tryAcquireFetchLock(ctx, destination+fetchLockSuffix)
		if err != nil {
			return fmt.Errorf("lock fetch %s for cleanup: %w", digest, err)
		}
		if busy {
			continue
		}

		cleanupErr := errors.Join(
			removeExpiredFetchFile(destination, cutoff),
			removeExpiredFetchFile(destination+fetchTempSuffix, cutoff),
		)

		// Unlinking is the final filesystem mutation for this digest. Fetch
		// waiters verify the locked inode after acquisition and retry if they
		// had opened this now-unlinked lock file.
		unlinkErr := lock.Unlink()
		closeErr := lock.Close()
		if err := errors.Join(cleanupErr, unlinkErr, closeErr); err != nil {
			return fmt.Errorf("clean up fetch %s: %w", digest, err)
		}
	}
	return nil
}

func fetchCacheEntryDigest(name string) (string, bool) {
	digest := name
	for _, suffix := range []string{fetchLockSuffix, fetchTempSuffix} {
		if strings.HasSuffix(digest, suffix) {
			digest = strings.TrimSuffix(digest, suffix)
			break
		}
	}
	if len(digest) != hex.EncodedLen(32) {
		return "", false
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || hex.EncodeToString(decoded) != digest {
		return "", false
	}
	return digest, true
}

func removeExpiredFetchFile(path string, cutoff time.Time) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.ModTime().After(cutoff) {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
