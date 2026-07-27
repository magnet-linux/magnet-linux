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

// CleanKind identifies the kind of store entry removed by cleanup.
type CleanKind string

const (
	CleanKindFetch        CleanKind = "fetch"
	CleanKindFetchPartial CleanKind = "fetch-partial"
	CleanKindPackage      CleanKind = "package"
	CleanKindWorkspace    CleanKind = "workspace"
)

// CleanEvent describes one useful store entry removed by cleanup.
type CleanEvent struct {
	Kind CleanKind
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
func (s *Store) CleanFetches(ctx context.Context, maxAge time.Duration) error {
	return s.CleanFetchesWithOptions(ctx, maxAge, CleanOptions{})
}

// CleanFetchesWithOptions is CleanFetches with removal reporting.
func (s *Store) CleanFetchesWithOptions(
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
// each idle package ID, its lock file is unlinked last while the exclusive
// lock is still held.
func (s *Store) CleanPackages(ctx context.Context, maxAge time.Duration) error {
	return s.CleanPackagesWithOptions(ctx, maxAge, CleanOptions{})
}

// CleanPackagesWithOptions is CleanPackages with removal reporting.
func (s *Store) CleanPackagesWithOptions(
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
	lockRoot := filepath.Join(s.path, buildLockDirectory)
	workRoot := filepath.Join(s.path, buildWorkDirectory)
	artifacts := make(map[PackageID][]string)
	packageIDs := make(map[PackageID]struct{})

	entries, err := readDirectoryEntries(packageRoot)
	if err != nil {
		return fmt.Errorf("read package artifacts: %w", err)
	}
	for _, entry := range entries {
		if packageID, ok := packageArtifactEntryID(entry.Name()); ok {
			packageIDs[packageID] = struct{}{}
			artifacts[packageID] = append(
				artifacts[packageID],
				filepath.Join(packageRoot, entry.Name()),
			)
		}
	}

	entries, err = readDirectoryEntries(lockRoot)
	if err != nil {
		return fmt.Errorf("read package locks: %w", err)
	}
	for _, entry := range entries {
		if packageID, ok := packageLockEntryID(entry.Name()); ok {
			packageIDs[packageID] = struct{}{}
		}
	}

	entries, err = readDirectoryEntries(workRoot)
	if err != nil {
		return fmt.Errorf("read package workspaces: %w", err)
	}
	for _, entry := range entries {
		packageID := PackageID(entry.Name())
		if validatePackageID(packageID) == nil {
			packageIDs[packageID] = struct{}{}
		}
	}

	if len(packageIDs) == 0 {
		return nil
	}
	if err := os.MkdirAll(lockRoot, 0o755); err != nil {
		return fmt.Errorf("create package lock directory: %w", err)
	}

	orderedIDs := make([]PackageID, 0, len(packageIDs))
	for packageID := range packageIDs {
		orderedIDs = append(orderedIDs, packageID)
	}
	sort.Slice(orderedIDs, func(first int, second int) bool {
		return orderedIDs[first] < orderedIDs[second]
	})

	removeAll := maxAge == 0
	cutoff := time.Now().Add(-maxAge)
	for _, packageID := range orderedIDs {
		if err := ctx.Err(); err != nil {
			return err
		}

		lock, busy, err := tryLock(ctx, s.packageLockPath(packageID))
		if err != nil {
			return fmt.Errorf("lock package %s for cleanup: %w", packageID, err)
		}
		if busy {
			continue
		}

		var cleanupErrors []error
		for _, artifactPath := range artifacts[packageID] {
			removed, err := removeExpiredRegularFile(
				artifactPath,
				cutoff,
				removeAll,
			)
			cleanupErrors = append(cleanupErrors, err)
			if removed {
				reportCleanEvent(options.OnRemove, CleanEvent{
					Kind: CleanKindPackage,
					Path: artifactPath,
				})
			}
		}
		workspacePath := filepath.Join(workRoot, string(packageID))
		removedWorkspace, workspaceErr := removeExpiredDirectory(
			workspacePath,
			cutoff,
			removeAll,
		)
		cleanupErrors = append(cleanupErrors, workspaceErr)
		if removedWorkspace {
			reportCleanEvent(options.OnRemove, CleanEvent{
				Kind: CleanKindWorkspace,
				Path: workspacePath,
			})
		}

		unlinkErr := lock.Unlink()
		closeErr := lock.Close()
		if err := errors.Join(
			errors.Join(cleanupErrors...),
			unlinkErr,
			closeErr,
		); err != nil {
			return fmt.Errorf("clean up package %s: %w", packageID, err)
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

func packageArtifactEntryID(name string) (PackageID, bool) {
	if !strings.HasSuffix(name, packageArtifactSuffix) {
		return "", false
	}
	base := strings.TrimSuffix(name, packageArtifactSuffix)
	if len(base) < packageIDLength {
		return "", false
	}
	packageID := PackageID(base[len(base)-packageIDLength:])
	if validatePackageID(packageID) != nil {
		return "", false
	}
	namePrefix := strings.TrimSuffix(
		base[:len(base)-packageIDLength],
		"-",
	)
	if packageArtifactFilename(packageID, namePrefix) != name {
		return "", false
	}
	if err := validatePackageName(namePrefix); err != nil {
		return "", false
	}
	return packageID, true
}

func packageLockEntryID(name string) (PackageID, bool) {
	if !strings.HasSuffix(name, buildLockSuffix) {
		return "", false
	}
	packageID := PackageID(strings.TrimSuffix(name, buildLockSuffix))
	return packageID, validatePackageID(packageID) == nil
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
