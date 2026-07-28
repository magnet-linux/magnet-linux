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

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

// CleanKind identifies the kind of store entry removed by cleanup.
type CleanKind string

const (
	CleanKindFetch        CleanKind = "fetch"
	CleanKindFetchPartial CleanKind = "fetch-partial"
	CleanKindPackage      CleanKind = "package"
	CleanKindWorkspace    CleanKind = "workspace"
	CleanKindTemp         CleanKind = "temp"
)

// CleanEvent describes one useful store entry removed by cleanup.
type CleanEvent struct {
	Kind CleanKind
	// Path is the absolute path removed from the store.
	Path string
}

// CleanOptions controls cleanup reporting.
type CleanOptions struct {
	// OnRemove is called synchronously after an entry is successfully removed.
	// It should return quickly.
	OnRemove func(CleanEvent)
}

// CleanFetches removes cached sources and partial downloads that have not
// been used within maxAge.
//
// Cleanup skips active fetches rather than waiting for their locks. A maxAge
// of zero removes all idle fetches. For each idle checksum, its lock file is
// unlinked last while the exclusive lock is still held.
func (s *Store) CleanFetches(
	ctx context.Context,
	maxAge time.Duration,
	options CleanOptions,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateStore(s); err != nil {
		return err
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

	removeAll := maxAge == 0
	cutoff := time.Now().Add(-maxAge)
	for _, digest := range orderedDigests {
		if err := ctx.Err(); err != nil {
			return err
		}

		destination := filepath.Join(fetchRoot, digest)
		lock, busy, err := tryLock(ctx, destination+fetchLockSuffix)
		if err != nil {
			return fmt.Errorf("lock fetch %s for cleanup: %w", digest, err)
		}
		if busy {
			continue
		}

		removedFetch, fetchErr := removeExpiredRegularFile(
			destination,
			cutoff,
			removeAll,
		)
		if removedFetch {
			reportCleanEvent(options.OnRemove, CleanEvent{
				Kind: CleanKindFetch,
				Path: destination,
			})
		}
		partialPath := destination + fetchTempSuffix
		removedPartial, partialErr := removeExpiredRegularFile(
			partialPath,
			cutoff,
			removeAll,
		)
		if removedPartial {
			reportCleanEvent(options.OnRemove, CleanEvent{
				Kind: CleanKindFetchPartial,
				Path: partialPath,
			})
		}
		cleanupErr := errors.Join(fetchErr, partialErr)

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

// CleanPackages removes package artifacts and build workspaces that have not
// been used within maxAge. A maxAge of zero removes all idle packages.
//
// Active builders and package readers are skipped rather than waited on. For
// each idle package, its lock file is unlinked last while the exclusive lock
// is still held.
func (s *Store) CleanPackages(
	ctx context.Context,
	maxAge time.Duration,
	options CleanOptions,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateStore(s); err != nil {
		return err
	}
	if maxAge < 0 {
		return errors.New("pkgstore: package cleanup max age must not be negative")
	}

	packageRoot := filepath.Join(s.path, packageDirectoryName)
	entries, err := readDirectoryEntries(packageRoot)
	if err != nil {
		return fmt.Errorf("read package entries: %w", err)
	}
	keys := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if key, ok := packageEntryKey(entry.Name()); ok {
			keys[key] = struct{}{}
		}
	}

	orderedKeys := make([]string, 0, len(keys))
	for key := range keys {
		orderedKeys = append(orderedKeys, key)
	}
	sort.Strings(orderedKeys)

	removeAll := maxAge == 0
	cutoff := time.Now().Add(-maxAge)
	for _, key := range orderedKeys {
		if err := ctx.Err(); err != nil {
			return err
		}

		lock, busy, err := tryLock(
			ctx,
			filepath.Join(packageRoot, key+packageLockSuffix),
		)
		if err != nil {
			return fmt.Errorf("lock package %s for cleanup: %w", key, err)
		}
		if busy {
			continue
		}

		artifactPath := filepath.Join(packageRoot, key+packageArtifactSuffix)
		removedArtifact, artifactErr := removeExpiredRegularFile(
			artifactPath,
			cutoff,
			removeAll,
		)
		if removedArtifact {
			reportCleanEvent(options.OnRemove, CleanEvent{
				Kind: CleanKindPackage,
				Path: artifactPath,
			})
		}
		workspacePath := filepath.Join(packageRoot, key+packageWorkSuffix)
		removedWorkspace, workspaceErr := removeExpiredDirectory(
			workspacePath,
			cutoff,
			removeAll,
		)
		if removedWorkspace {
			reportCleanEvent(options.OnRemove, CleanEvent{
				Kind: CleanKindWorkspace,
				Path: workspacePath,
			})
		}

		unlinkErr := lock.Unlink()
		closeErr := lock.Close()
		if err := errors.Join(
			artifactErr,
			workspaceErr,
			unlinkErr,
			closeErr,
		); err != nil {
			return fmt.Errorf("clean up package %s: %w", key, err)
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

// packageEntryKey recognizes a package artifact, lock, or workspace entry
// name and returns its `[name-]id` key.
func packageEntryKey(name string) (string, bool) {
	for _, suffix := range []string{
		packageArtifactSuffix,
		packageLockSuffix,
		packageWorkSuffix,
	} {
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		key := strings.TrimSuffix(name, suffix)
		if _, ok := packageKeyID(key); !ok {
			return "", false
		}
		return key, true
	}
	return "", false
}

// packageKeyID validates a `[name-]id` package key and returns its package
// ID.
func packageKeyID(key string) (pkggraph.ID, bool) {
	if len(key) < pkggraph.IDLength {
		return "", false
	}
	packageID := pkggraph.ID(key[len(key)-pkggraph.IDLength:])
	if pkggraph.ValidateID(packageID) != nil {
		return "", false
	}
	namePrefix := strings.TrimSuffix(
		key[:len(key)-pkggraph.IDLength],
		"-",
	)
	if packageKey(packageID, namePrefix) != key {
		return "", false
	}
	if err := pkggraph.ValidateName(namePrefix); err != nil {
		return "", false
	}
	return packageID, true
}

func packageArtifactEntryID(name string) (pkggraph.ID, bool) {
	if !strings.HasSuffix(name, packageArtifactSuffix) {
		return "", false
	}
	return packageKeyID(strings.TrimSuffix(name, packageArtifactSuffix))
}

func readDirectoryEntries(path string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return entries, err
}

func removeExpiredRegularFile(
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
	if !info.Mode().IsRegular() ||
		!removeAll && info.ModTime().After(cutoff) {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
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
