package pkgstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

// DestinationAction describes a filesystem change made by Sync or Uninstall.
type DestinationAction string

const (
	DestinationCreate           DestinationAction = "create"
	DestinationReplace          DestinationAction = "replace"
	DestinationRemove           DestinationAction = "remove"
	DestinationChmod            DestinationAction = "chmod"
	DestinationReplaceModified  DestinationAction = "replace-modified"
	DestinationRemoveModified   DestinationAction = "remove-modified"
	DestinationRepairModified   DestinationAction = "repair-modified"
	DestinationReplaceUnmanaged DestinationAction = "replace-unmanaged"
	DestinationDeleteUnmanaged  DestinationAction = "delete-unmanaged"
)

// DestinationChange is one planned filesystem change. Path is relative to the
// destination.
type DestinationChange struct {
	Action DestinationAction
	Path   string
}

type syncPlan struct {
	destination string
	desiredRoot string
	roots       []pkggraph.ID
	packages    []syncManifestPackage
	desired     map[string]desiredSyncEntry
	ownedDirs   map[string]bool

	createDirs    []string
	writeEntries  []string
	removeEntries []string
	removeDirs    []string
	chmodDirs     []string
	chownDirs     map[string]bool
	changes       []DestinationChange
}

type syncPlanOptions struct {
	force           bool
	deleteUnmanaged bool
}

type syncDrift struct {
	currentType syncEntryType
	exists      bool
}

type syncProblem struct {
	path    string
	message string
}

type syncPlanError struct {
	operation string
	problems  []syncProblem
}

func (err *syncPlanError) Error() string {
	var builder strings.Builder
	fmt.Fprintf(
		&builder,
		"%s plan has %d problem",
		err.operation,
		len(err.problems),
	)
	if len(err.problems) != 1 {
		builder.WriteByte('s')
	}
	for _, problem := range err.problems {
		fmt.Fprintf(&builder, "\n  %s: %s", problem.path, problem.message)
	}
	return builder.String()
}

func planSyncDestination(
	ctx context.Context,
	destination string,
	desiredRoot string,
	desiredEntries []desiredSyncEntry,
	roots []pkggraph.ID,
	packages []syncManifestPackage,
	operation string,
	options syncPlanOptions,
) (*syncPlan, error) {
	manifest, found, err := readSyncManifest(destination)
	if err != nil {
		return nil, err
	}
	if operation == "uninstall" && !found {
		return nil, errors.New("destination has no magpkg manifest")
	}
	if operation == "uninstall" {
		roots = manifest.Roots
		packages = manifest.Packages
	}

	plan := &syncPlan{
		destination: destination,
		desiredRoot: desiredRoot,
		roots:       append([]pkggraph.ID(nil), roots...),
		packages:    append([]syncManifestPackage(nil), packages...),
		desired:     make(map[string]desiredSyncEntry, len(desiredEntries)),
		ownedDirs:   make(map[string]bool),
		chownDirs:   make(map[string]bool),
	}
	old := make(map[string]syncManifestEntry, len(manifest.Entries))
	modified := make(map[string]syncDrift)
	var problems []syncProblem
	for _, entry := range manifest.Entries {
		old[entry.Path] = entry
		if err := verifySyncManifestEntry(ctx, destination, entry); err != nil {
			if !options.force {
				problems = append(problems, syncProblem{
					path:    entry.Path,
					message: err.Error(),
				})
				continue
			}
			drift, driftErr := inspectSyncDrift(destination, entry.Path)
			if driftErr != nil {
				problems = append(problems, syncProblem{
					path:    entry.Path,
					message: driftErr.Error(),
				})
				continue
			}
			modified[entry.Path] = drift
		}
	}
	for _, entry := range desiredEntries {
		plan.desired[entry.Path] = entry
	}

	for _, entry := range desiredEntries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		oldEntry, wasManaged := old[entry.Path]
		drift, wasModified := modified[entry.Path]
		if entry.Type == syncEntryDirectory {
			switch {
			case wasManaged && oldEntry.Type != syncEntryDirectory:
				problems = append(problems, syncProblem{
					path: entry.Path,
					message: fmt.Sprintf(
						"managed %s cannot be replaced by a directory",
						oldEntry.Type,
					),
				})
			case wasManaged && wasModified &&
				drift.exists &&
				drift.currentType != syncEntryDirectory:
				problems = append(problems, syncProblem{
					path: entry.Path,
					message: fmt.Sprintf(
						"modified %s cannot safely be replaced by a directory",
						drift.currentType,
					),
				})
			case wasManaged && wasModified && !drift.exists:
				plan.ownedDirs[entry.Path] = true
				plan.createDirs = append(plan.createDirs, entry.Path)
				plan.chmodDirs = append(plan.chmodDirs, entry.Path)
				plan.changes = append(plan.changes, DestinationChange{
					Action: DestinationRepairModified,
					Path:   entry.Path,
				})
			case wasManaged:
				plan.ownedDirs[entry.Path] = true
				if wasModified {
					plan.chmodDirs = append(plan.chmodDirs, entry.Path)
					plan.chownDirs[entry.Path] = true
					plan.changes = append(plan.changes, DestinationChange{
						Action: DestinationRepairModified,
						Path:   entry.Path,
					})
				} else if oldEntry.Mode != entry.Mode {
					plan.chmodDirs = append(plan.chmodDirs, entry.Path)
					plan.changes = append(plan.changes, DestinationChange{
						Action: DestinationChmod,
						Path:   entry.Path,
					})
				}
			default:
				info, err := safeSyncLstat(destination, entry.Path)
				switch {
				case errors.Is(err, os.ErrNotExist):
					plan.ownedDirs[entry.Path] = true
					plan.createDirs = append(plan.createDirs, entry.Path)
					plan.chmodDirs = append(plan.chmodDirs, entry.Path)
					plan.changes = append(plan.changes, DestinationChange{
						Action: DestinationCreate,
						Path:   entry.Path,
					})
				case err != nil:
					problems = append(problems, syncProblem{
						path:    entry.Path,
						message: err.Error(),
					})
				case !info.IsDir():
					if options.force &&
						syncTypeFromMode(info.Mode()) != syncEntryDirectory &&
						syncTypeFromMode(info.Mode()) != syncEntryType("special") {
						problems = append(problems, syncProblem{
							path: entry.Path,
							message: fmt.Sprintf(
								"unmanaged %s cannot safely be replaced by a directory",
								syncTypeFromMode(info.Mode()),
							),
						})
					} else {
						problems = append(problems, syncProblem{
							path: entry.Path,
							message: fmt.Sprintf(
								"unmanaged %s already exists",
								syncTypeFromMode(info.Mode()),
							),
						})
					}
				}
			}
			continue
		}

		switch {
		case wasManaged && oldEntry.Type == syncEntryDirectory:
			problems = append(problems, syncProblem{
				path: entry.Path,
				message: fmt.Sprintf(
					"managed directory cannot be replaced by a %s",
					entry.Type,
				),
			})
		case wasManaged && wasModified &&
			drift.exists &&
			drift.currentType == syncEntryDirectory:
			problems = append(problems, syncProblem{
				path:    entry.Path,
				message: "modified directory cannot safely be replaced by a file or symlink",
			})
		case wasManaged:
			if wasModified || !desiredMatchesManifest(entry, oldEntry) {
				plan.writeEntries = append(plan.writeEntries, entry.Path)
				action := DestinationReplace
				if wasModified {
					action = DestinationReplaceModified
				}
				plan.changes = append(plan.changes, DestinationChange{
					Action: action,
					Path:   entry.Path,
				})
			}
		default:
			_, err := safeSyncLstat(destination, entry.Path)
			switch {
			case errors.Is(err, os.ErrNotExist):
				plan.writeEntries = append(plan.writeEntries, entry.Path)
				plan.changes = append(plan.changes, DestinationChange{
					Action: DestinationCreate,
					Path:   entry.Path,
				})
			case err != nil:
				problems = append(problems, syncProblem{
					path:    entry.Path,
					message: err.Error(),
				})
			default:
				if !options.force {
					problems = append(problems, syncProblem{
						path:    entry.Path,
						message: "unmanaged path already exists",
					})
					continue
				}
				info, infoErr := safeSyncLstat(destination, entry.Path)
				if infoErr != nil {
					problems = append(problems, syncProblem{
						path:    entry.Path,
						message: infoErr.Error(),
					})
					continue
				}
				actualType := syncTypeFromMode(info.Mode())
				if actualType == syncEntryDirectory ||
					actualType == syncEntryType("special") {
					problems = append(problems, syncProblem{
						path: entry.Path,
						message: fmt.Sprintf(
							"unmanaged %s cannot safely be replaced",
							actualType,
						),
					})
					continue
				}
				plan.writeEntries = append(plan.writeEntries, entry.Path)
				plan.changes = append(plan.changes, DestinationChange{
					Action: DestinationReplaceUnmanaged,
					Path:   entry.Path,
				})
			}
		}
	}

	for _, entry := range manifest.Entries {
		if _, ok := plan.desired[entry.Path]; ok {
			continue
		}
		if entry.Type == syncEntryDirectory {
			plan.removeDirs = append(plan.removeDirs, entry.Path)
		} else {
			plan.removeEntries = append(plan.removeEntries, entry.Path)
		}
		action := DestinationRemove
		if _, ok := modified[entry.Path]; ok {
			action = DestinationRemoveModified
		}
		plan.changes = append(plan.changes, DestinationChange{
			Action: action,
			Path:   entry.Path,
		})
	}

	if options.deleteUnmanaged {
		if _, err := os.Lstat(destination); errors.Is(err, os.ErrNotExist) {
			// A missing destination contains no unmanaged paths.
		} else if err != nil {
			return nil, err
		} else {
			unmanagedEntries, unmanagedDirs, err := findUnmanagedSyncPaths(
				ctx,
				destination,
				plan.desired,
				old,
			)
			if err != nil {
				return nil, err
			}
			plan.removeEntries = append(plan.removeEntries, unmanagedEntries...)
			plan.removeDirs = append(plan.removeDirs, unmanagedDirs...)
			for _, path := range append(
				append([]string(nil), unmanagedEntries...),
				unmanagedDirs...,
			) {
				plan.changes = append(plan.changes, DestinationChange{
					Action: DestinationDeleteUnmanaged,
					Path:   path,
				})
			}
		}
	}

	if len(problems) != 0 {
		slices.SortFunc(problems, func(a, b syncProblem) int {
			if result := strings.Compare(a.path, b.path); result != 0 {
				return result
			}
			return strings.Compare(a.message, b.message)
		})
		return nil, &syncPlanError{
			operation: operation,
			problems:  problems,
		}
	}

	sortSyncPathsShallowFirst(plan.createDirs)
	sortSyncPathsShallowFirst(plan.writeEntries)
	sortSyncPathsDeepFirst(plan.removeEntries)
	sortSyncPathsDeepFirst(plan.removeDirs)
	sortSyncPathsDeepFirst(plan.chmodDirs)
	return plan, nil
}

func inspectSyncDrift(
	destination string,
	relative string,
) (syncDrift, error) {
	info, err := safeSyncLstat(destination, relative)
	if errors.Is(err, os.ErrNotExist) {
		return syncDrift{}, nil
	}
	if err != nil {
		return syncDrift{}, err
	}
	entryType := syncTypeFromMode(info.Mode())
	if entryType == syncEntryType("special") {
		return syncDrift{}, errors.New(
			"managed path became an unsupported special file",
		)
	}
	return syncDrift{
		currentType: entryType,
		exists:      true,
	}, nil
}

func findUnmanagedSyncPaths(
	ctx context.Context,
	destination string,
	desired map[string]desiredSyncEntry,
	managed map[string]syncManifestEntry,
) ([]string, []string, error) {
	rootMount, err := syncMountID(destination)
	if err != nil {
		return nil, nil, err
	}

	var entries []string
	var directories []string
	err = filepath.WalkDir(destination, func(
		current string,
		directoryEntry os.DirEntry,
		walkErr error,
	) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if current == destination {
			return nil
		}
		relative, err := filepath.Rel(destination, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == syncMetadataDirectoryName {
			return filepath.SkipDir
		}

		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		mountID, err := syncMountID(current)
		if err != nil {
			return err
		}
		if mountID != rootMount {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if _, ok := desired[relative]; ok {
			return nil
		}
		if _, ok := managed[relative]; ok {
			return nil
		}
		if info.IsDir() {
			directories = append(directories, relative)
		} else {
			entries = append(entries, relative)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sortSyncPathsDeepFirst(entries)
	sortSyncPathsDeepFirst(directories)
	return entries, directories, nil
}

func verifySyncManifestEntry(
	ctx context.Context,
	destination string,
	entry syncManifestEntry,
) error {
	info, err := safeSyncLstat(destination, entry.Path)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("managed path is missing")
	}
	if err != nil {
		return err
	}
	actualType := syncTypeFromMode(info.Mode())
	if actualType != entry.Type {
		return fmt.Errorf("expected %s, found %s", entry.Type, actualType)
	}
	if mode := unixMode(info.Mode()); mode != entry.Mode {
		return fmt.Errorf(
			"mode is %#o, expected %#o",
			mode,
			entry.Mode,
		)
	}
	uid, gid, err := syncOwnership(info)
	if err != nil {
		return err
	}
	if uid != entry.UID || gid != entry.GID {
		return fmt.Errorf(
			"ownership is %d:%d, expected %d:%d",
			uid,
			gid,
			entry.UID,
			entry.GID,
		)
	}
	switch entry.Type {
	case syncEntryFile:
		actual, err := syncFileSHA256(
			ctx,
			filepath.Join(destination, filepath.FromSlash(entry.Path)),
		)
		if err != nil {
			return err
		}
		if actual != entry.SHA256 {
			return fmt.Errorf(
				"content SHA-256 is %s, expected %s",
				actual,
				entry.SHA256,
			)
		}
	case syncEntrySymlink:
		actual, err := os.Readlink(
			filepath.Join(destination, filepath.FromSlash(entry.Path)),
		)
		if err != nil {
			return err
		}
		if actual != entry.LinkTarget {
			return fmt.Errorf(
				"link target is %q, expected %q",
				actual,
				entry.LinkTarget,
			)
		}
	}
	return nil
}

func desiredMatchesManifest(
	desired desiredSyncEntry,
	manifest syncManifestEntry,
) bool {
	return desired.Type == manifest.Type &&
		desired.Mode == manifest.Mode &&
		desired.SHA256 == manifest.SHA256 &&
		desired.LinkTarget == manifest.LinkTarget
}

func safeSyncLstat(destination string, relative string) (os.FileInfo, error) {
	if err := validateSyncRelativePath(relative); err != nil {
		return nil, err
	}
	current := destination
	components := strings.Split(filepath.FromSlash(relative), string(os.PathSeparator))
	for _, component := range components[:len(components)-1] {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf(
				"parent %q is a %s, not a directory",
				component,
				syncTypeFromMode(info.Mode()),
			)
		}
	}
	return os.Lstat(filepath.Join(destination, filepath.FromSlash(relative)))
}

func syncTypeFromMode(mode os.FileMode) syncEntryType {
	switch {
	case mode.IsDir():
		return syncEntryDirectory
	case mode.IsRegular():
		return syncEntryFile
	case mode&os.ModeSymlink != 0:
		return syncEntrySymlink
	default:
		return syncEntryType("special")
	}
}

func sortSyncPathsShallowFirst(paths []string) {
	slices.SortFunc(paths, func(a, b string) int {
		aDepth := strings.Count(a, "/")
		bDepth := strings.Count(b, "/")
		if aDepth != bDepth {
			return aDepth - bDepth
		}
		return strings.Compare(a, b)
	})
}

func sortSyncPathsDeepFirst(paths []string) {
	slices.SortFunc(paths, func(a, b string) int {
		aDepth := strings.Count(a, "/")
		bDepth := strings.Count(b, "/")
		if aDepth != bDepth {
			return bDepth - aDepth
		}
		return strings.Compare(a, b)
	})
}
