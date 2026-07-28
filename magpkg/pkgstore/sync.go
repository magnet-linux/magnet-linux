package pkgstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

// SyncOptions controls synchronization of a runtime closure into a
// destination.
type SyncOptions struct {
	// DryRun returns the verified plan without changing the destination.
	DryRun bool
	// Force permits replacement or removal of modified manifest-owned paths
	// and replacement of unmanaged file or symlink collisions.
	Force bool
	// DeleteUnmanaged removes paths outside the desired runtime closure.
	// It requires Force. The .magpkg metadata tree and other mounts are
	// preserved.
	DeleteUnmanaged bool
}

// DestinationResult describes the plan produced by Sync or Uninstall.
type DestinationResult struct {
	// Packages is the dependency-first runtime closure installed or removed.
	Packages []pkggraph.ID
	// Changes contains the planned destination mutations.
	Changes []DestinationChange
	DryRun  bool
}

// Sync makes the magpkg-managed portion of destination match roots' runtime
// closure. Every artifact in the closure must already exist in the store.
// Unmanaged directories are reused without being claimed. By default, a
// collision with an unmanaged non-directory or modification to a manifest-owned
// entry fails before filesystem changes begin. Force permits replacement of
// those paths, while DeleteUnmanaged additionally removes unrelated paths
// without following symlinks or crossing mount points.
func (store *Store) Sync(
	ctx context.Context,
	graph *pkggraph.Graph,
	roots []pkggraph.ID,
	destination string,
	options SyncOptions,
) (*DestinationResult, error) {
	if err := validateStore(store); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if graph == nil {
		return nil, errors.New("pkgstore: package graph is nil")
	}
	if destination == "" {
		return nil, errors.New("pkgstore: sync destination is empty")
	}
	if options.DeleteUnmanaged && !options.Force {
		return nil, errors.New(
			"pkgstore: deleting unmanaged paths requires force",
		)
	}
	closure, err := graph.RuntimeClosure(roots...)
	if err != nil {
		return nil, err
	}

	staging, err := os.MkdirTemp("", "magpkg-sync-")
	if err != nil {
		return nil, fmt.Errorf("create sync staging root: %w", err)
	}
	defer os.RemoveAll(staging)
	if err := store.ExtractPackages(ctx, graph, closure, staging); err != nil {
		return nil, fmt.Errorf("compose sync closure: %w", err)
	}
	desiredEntries, err := scanDesiredSyncTree(ctx, staging)
	if err != nil {
		return nil, fmt.Errorf("inspect sync closure: %w", err)
	}
	packages := make([]syncManifestPackage, 0, len(closure))
	for _, packageID := range closure {
		packages = append(packages, syncManifestPackage{
			ID:   packageID,
			Name: graph.Nodes[packageID].Name,
		})
	}

	var result *DestinationResult
	err = withSyncDestinationLock(
		ctx,
		destination,
		true,
		options.DryRun,
		func(absoluteDestination string) error {
			plan, err := planSyncDestination(
				ctx,
				absoluteDestination,
				staging,
				desiredEntries,
				roots,
				packages,
				"sync",
				syncPlanOptions{
					force:           options.Force,
					deleteUnmanaged: options.DeleteUnmanaged,
				},
			)
			if err != nil {
				return err
			}
			result = &DestinationResult{
				Packages: append([]pkggraph.ID(nil), closure...),
				Changes:  append([]DestinationChange(nil), plan.changes...),
				DryRun:   options.DryRun,
			}
			if options.DryRun {
				return nil
			}
			if err := executeSyncPlan(ctx, plan); err != nil {
				return fmt.Errorf("execute sync plan: %w", err)
			}
			manifest, err := manifestAfterSync(ctx, plan)
			if err != nil {
				return fmt.Errorf("create sync manifest: %w", err)
			}
			if err := writeSyncManifest(absoluteDestination, manifest); err != nil {
				return fmt.Errorf("write sync manifest: %w", err)
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// UninstallOptions controls removal of a manifest-managed destination.
type UninstallOptions struct {
	// DryRun returns the verified plan without changing the destination.
	DryRun bool
	// Force permits removal of modified manifest-owned paths.
	Force bool
}

// Uninstall removes paths recorded in destination's magpkg manifest.
// Modified paths cause planning to fail unless Force is set. Unmanaged paths
// and non-empty directories are preserved.
func Uninstall(
	ctx context.Context,
	destination string,
	options UninstallOptions,
) (*DestinationResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if destination == "" {
		return nil, errors.New("pkgstore: uninstall destination is empty")
	}

	var result *DestinationResult
	err := withSyncDestinationLock(
		ctx,
		destination,
		false,
		options.DryRun,
		func(absoluteDestination string) error {
			plan, err := planSyncDestination(
				ctx,
				absoluteDestination,
				"",
				nil,
				nil,
				nil,
				"uninstall",
				syncPlanOptions{force: options.Force},
			)
			if err != nil {
				return err
			}
			result = &DestinationResult{
				Packages: syncManifestPackageIDs(plan.packages),
				Changes:  append([]DestinationChange(nil), plan.changes...),
				DryRun:   options.DryRun,
			}
			if options.DryRun {
				return nil
			}
			if err := executeSyncPlan(ctx, plan); err != nil {
				return fmt.Errorf("execute uninstall plan: %w", err)
			}
			if err := os.Remove(syncManifestPath(absoluteDestination)); err != nil {
				return fmt.Errorf("remove sync manifest: %w", err)
			}
			if err := syncDirectory(syncMetadataPath(absoluteDestination)); err != nil {
				return fmt.Errorf("sync manifest directory: %w", err)
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func withSyncDestinationLock(
	ctx context.Context,
	destination string,
	create bool,
	dryRun bool,
	use func(string) error,
) (retErr error) {
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	info, err := os.Lstat(absolute)
	switch {
	case errors.Is(err, os.ErrNotExist) && create && dryRun:
		return use(absolute)
	case errors.Is(err, os.ErrNotExist) && create:
		if err := os.MkdirAll(absolute, 0o755); err != nil {
			return fmt.Errorf("create sync destination: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		return errors.New("sync destination does not exist")
	case err != nil:
		return err
	case !info.IsDir():
		return errors.New("sync destination is not a directory")
	}

	metadataPath := syncMetadataPath(absolute)
	info, err = os.Lstat(metadataPath)
	switch {
	case errors.Is(err, os.ErrNotExist) && create && dryRun:
		return use(absolute)
	case errors.Is(err, os.ErrNotExist) && create:
		if err := os.Mkdir(metadataPath, 0o700); err != nil {
			return fmt.Errorf("create sync metadata directory: %w", err)
		}
	case errors.Is(err, os.ErrNotExist):
		return errors.New("destination has no magpkg metadata directory")
	case err != nil:
		return err
	case !info.IsDir():
		return errors.New("destination .magpkg path is not a directory")
	}

	if dryRun {
		info, err := os.Lstat(syncLockPath(absolute))
		if errors.Is(err, os.ErrNotExist) {
			return use(absolute)
		}
		if err != nil {
			return fmt.Errorf("inspect sync destination lock: %w", err)
		}
		if !info.Mode().IsRegular() {
			return errors.New("destination .magpkg lock is not a regular file")
		}
	}
	lock, busy, err := tryLock(ctx, syncLockPath(absolute))
	if err != nil {
		return fmt.Errorf("lock sync destination: %w", err)
	}
	if busy {
		return fmt.Errorf("sync destination %q is already being modified", absolute)
	}
	defer func() {
		retErr = errors.Join(retErr, lock.Close())
	}()
	return use(absolute)
}

func syncManifestPackageIDs(packages []syncManifestPackage) []pkggraph.ID {
	ids := make([]pkggraph.ID, len(packages))
	for index, pkg := range packages {
		ids[index] = pkg.ID
	}
	return ids
}

func executeSyncPlan(ctx context.Context, plan *syncPlan) error {
	for _, relative := range plan.createDirs {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(
			plan.destination,
			filepath.FromSlash(relative),
		)
		if err := os.Mkdir(target, 0o700); err != nil {
			return fmt.Errorf("create directory %q: %w", relative, err)
		}
	}
	for _, relative := range plan.writeEntries {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry := plan.desired[relative]
		source := filepath.Join(plan.desiredRoot, filepath.FromSlash(relative))
		target := filepath.Join(plan.destination, filepath.FromSlash(relative))
		switch entry.Type {
		case syncEntryFile:
			if err := installSyncRegularFile(ctx, source, target, entry.Mode); err != nil {
				return fmt.Errorf("install file %q: %w", relative, err)
			}
		case syncEntrySymlink:
			if err := installSyncSymlink(target, entry.LinkTarget); err != nil {
				return fmt.Errorf("install symlink %q: %w", relative, err)
			}
		default:
			return fmt.Errorf(
				"install %q: unsupported planned type %q",
				relative,
				entry.Type,
			)
		}
	}
	for _, relative := range plan.removeEntries {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(
			plan.destination,
			filepath.FromSlash(relative),
		)
		if err := os.Remove(target); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove path %q: %w", relative, err)
		}
	}
	for _, relative := range plan.removeDirs {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(
			plan.destination,
			filepath.FromSlash(relative),
		)
		err := os.Remove(target)
		if err != nil &&
			!errors.Is(err, os.ErrNotExist) &&
			!errors.Is(err, syscall.ENOTEMPTY) &&
			!errors.Is(err, syscall.EEXIST) {
			return fmt.Errorf("remove directory %q: %w", relative, err)
		}
	}
	for _, relative := range plan.chmodDirs {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry := plan.desired[relative]
		target := filepath.Join(
			plan.destination,
			filepath.FromSlash(relative),
		)
		if plan.chownDirs[relative] {
			if err := os.Chown(target, os.Geteuid(), os.Getegid()); err != nil {
				return fmt.Errorf("set directory ownership %q: %w", relative, err)
			}
		}
		if err := os.Chmod(target, fileModeFromUnix(entry.Mode)); err != nil {
			return fmt.Errorf("set directory mode %q: %w", relative, err)
		}
	}
	return nil
}

func installSyncRegularFile(
	ctx context.Context,
	source string,
	target string,
	mode uint32,
) (retErr error) {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	temporary, err := os.CreateTemp(filepath.Dir(target), ".magpkg-sync-")
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
	if _, err := copyContext(ctx, temporary, input); err != nil {
		return err
	}
	if err := temporary.Chmod(fileModeFromUnix(mode)); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	temporaryOpen = false
	return os.Rename(temporaryPath, target)
}

func installSyncSymlink(target string, linkTarget string) error {
	temporary, err := os.CreateTemp(filepath.Dir(target), ".magpkg-sync-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return err
	}
	if err := os.Symlink(linkTarget, temporaryPath); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		_ = os.Remove(temporaryPath)
		return err
	}
	return nil
}

func manifestAfterSync(
	ctx context.Context,
	plan *syncPlan,
) (syncManifest, error) {
	paths := make([]string, 0, len(plan.desired))
	for path, entry := range plan.desired {
		if entry.Type != syncEntryDirectory || plan.ownedDirs[path] {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	entries := make([]syncManifestEntry, 0, len(paths))
	for _, relative := range paths {
		if err := ctx.Err(); err != nil {
			return syncManifest{}, err
		}
		desired := plan.desired[relative]
		info, err := safeSyncLstat(plan.destination, relative)
		if err != nil {
			return syncManifest{}, err
		}
		uid, gid, err := syncOwnership(info)
		if err != nil {
			return syncManifest{}, err
		}
		entries = append(entries, syncManifestEntry{
			Path:       relative,
			Type:       desired.Type,
			Mode:       desired.Mode,
			UID:        uid,
			GID:        gid,
			SHA256:     desired.SHA256,
			LinkTarget: desired.LinkTarget,
		})
	}
	return syncManifest{
		Version:  syncManifestVersion,
		Roots:    append([]pkggraph.ID(nil), plan.roots...),
		Packages: append([]syncManifestPackage(nil), plan.packages...),
		Entries:  entries,
	}, nil
}
