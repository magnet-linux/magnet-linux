package pkgstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSyncAndUninstallPreserveUnmanagedPaths(t *testing.T) {
	store := openTestStore(t)
	definition, packageID := writeSyncTestPackage(
		t,
		store,
		"hello",
		func(root string) {
			writeSyncTestFile(t, root, "bin/hello", "hello\n", 0o755)
			writeSyncTestFile(
				t,
				root,
				"etc/hello.conf",
				"greeting=hello\n",
				0o640,
			)
			if err := os.Symlink(
				"hello",
				filepath.Join(root, "bin", "hi"),
			); err != nil {
				t.Fatal(err)
			}
		},
	)
	graph := syncTestGraph(packageID, definition)

	destination := t.TempDir()
	if err := os.Mkdir(filepath.Join(destination, "etc"), 0o750); err != nil {
		t.Fatal(err)
	}
	unmanaged := filepath.Join(destination, "etc", "local.conf")
	if err := os.WriteFile(unmanaged, []byte("local\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := store.Sync(
		context.Background(),
		graph,
		[]PackageID{packageID},
		destination,
		SyncOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.DryRun {
		t.Fatal("sync result was marked as a dry run")
	}
	if !slices.Contains(result.Packages, packageID) {
		t.Fatalf("sync packages = %v, want %s", result.Packages, packageID)
	}
	assertSyncFile(t, filepath.Join(destination, "bin", "hello"), "hello\n", 0o755)
	assertSyncFile(
		t,
		filepath.Join(destination, "etc", "hello.conf"),
		"greeting=hello\n",
		0o640,
	)
	target, err := os.Readlink(filepath.Join(destination, "bin", "hi"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "hello" {
		t.Fatalf("installed symlink target = %q, want hello", target)
	}
	if info, err := os.Stat(filepath.Join(destination, "etc")); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o750 {
		t.Fatalf(
			"pre-existing directory mode = %#o, want 0750",
			info.Mode().Perm(),
		)
	}

	manifest, found, err := readSyncManifest(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("sync did not write a manifest")
	}
	manifestPaths := make([]string, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		manifestPaths = append(manifestPaths, entry.Path)
	}
	for _, expected := range []string{
		"bin",
		"bin/hello",
		"bin/hi",
		"etc/hello.conf",
	} {
		if !slices.Contains(manifestPaths, expected) {
			t.Fatalf("manifest paths = %v, missing %q", manifestPaths, expected)
		}
	}
	if slices.Contains(manifestPaths, "etc") {
		t.Fatalf("manifest claimed pre-existing directory: %v", manifestPaths)
	}

	if _, err := Uninstall(
		context.Background(),
		destination,
		UninstallOptions{},
	); err != nil {
		t.Fatal(err)
	}
	assertSyncMissing(t, filepath.Join(destination, "bin", "hello"))
	assertSyncMissing(t, filepath.Join(destination, "bin", "hi"))
	assertSyncMissing(t, filepath.Join(destination, "bin"))
	assertSyncMissing(t, filepath.Join(destination, "etc", "hello.conf"))
	assertSyncFile(t, unmanaged, "local\n", 0o600)
	if _, err := os.Stat(filepath.Join(destination, "etc")); err != nil {
		t.Fatalf("pre-existing directory was removed: %v", err)
	}
	assertSyncMissing(t, syncManifestPath(destination))
	if _, err := os.Stat(syncLockPath(destination)); err != nil {
		t.Fatalf("persistent destination lock is missing: %v", err)
	}
}

func TestSyncRejectsModifiedManagedFilesBeforeChangingAnything(t *testing.T) {
	store := openTestStore(t)
	oldDefinition, oldID := writeSyncTestPackage(
		t,
		store,
		"old",
		func(root string) {
			writeSyncTestFile(t, root, "bin/tool", "old\n", 0o755)
		},
	)
	destination := t.TempDir()
	if _, err := store.Sync(
		context.Background(),
		syncTestGraph(oldID, oldDefinition),
		[]PackageID{oldID},
		destination,
		SyncOptions{},
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(destination, "bin", "tool"),
		[]byte("locally modified\n"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	newDefinition, newID := writeSyncTestPackage(
		t,
		store,
		"new",
		func(root string) {
			writeSyncTestFile(t, root, "bin/new", "new\n", 0o755)
		},
	)
	_, err := store.Sync(
		context.Background(),
		syncTestGraph(newID, newDefinition),
		[]PackageID{newID},
		destination,
		SyncOptions{},
	)
	if err == nil || !strings.Contains(err.Error(), "content SHA-256") {
		t.Fatalf("sync error = %v, want modified-content error", err)
	}
	assertSyncMissing(t, filepath.Join(destination, "bin", "new"))
	assertSyncFile(
		t,
		filepath.Join(destination, "bin", "tool"),
		"locally modified\n",
		0o755,
	)

	if _, err := Uninstall(
		context.Background(),
		destination,
		UninstallOptions{},
	); err == nil || !strings.Contains(err.Error(), "content SHA-256") {
		t.Fatalf("uninstall error = %v, want modified-content error", err)
	}
	assertSyncFile(
		t,
		filepath.Join(destination, "bin", "tool"),
		"locally modified\n",
		0o755,
	)
}

func TestSyncReconcilesManagedClosure(t *testing.T) {
	store := openTestStore(t)
	oldDefinition, oldID := writeSyncTestPackage(
		t,
		store,
		"old",
		func(root string) {
			writeSyncTestFile(t, root, "bin/old", "old\n", 0o755)
			writeSyncTestFile(t, root, "share/common", "common\n", 0o644)
		},
	)
	destination := t.TempDir()
	if _, err := store.Sync(
		context.Background(),
		syncTestGraph(oldID, oldDefinition),
		[]PackageID{oldID},
		destination,
		SyncOptions{},
	); err != nil {
		t.Fatal(err)
	}

	newDefinition, newID := writeSyncTestPackage(
		t,
		store,
		"new",
		func(root string) {
			writeSyncTestFile(t, root, "bin/new", "new\n", 0o755)
			writeSyncTestFile(t, root, "share/common", "changed\n", 0o600)
		},
	)
	result, err := store.Sync(
		context.Background(),
		syncTestGraph(newID, newDefinition),
		[]PackageID{newID},
		destination,
		SyncOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []SyncChange{
		{Action: SyncCreate, Path: "bin/new"},
		{Action: SyncReplace, Path: "share/common"},
		{Action: SyncRemove, Path: "bin/old"},
	} {
		if !slices.Contains(result.Changes, expected) {
			t.Fatalf("sync changes = %+v, missing %+v", result.Changes, expected)
		}
	}
	assertSyncMissing(t, filepath.Join(destination, "bin", "old"))
	assertSyncFile(t, filepath.Join(destination, "bin", "new"), "new\n", 0o755)
	assertSyncFile(
		t,
		filepath.Join(destination, "share", "common"),
		"changed\n",
		0o600,
	)
	manifest, found, err := readSyncManifest(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !found || len(manifest.Packages) != 1 ||
		manifest.Packages[0].ID != newID {
		t.Fatalf("updated manifest = %+v", manifest)
	}
}

func TestSyncRejectsUnmanagedFileCollision(t *testing.T) {
	store := openTestStore(t)
	definition, packageID := writeSyncTestPackage(
		t,
		store,
		"hello",
		func(root string) {
			writeSyncTestFile(t, root, "hello", "managed\n", 0o644)
		},
	)
	destination := t.TempDir()
	collision := filepath.Join(destination, "hello")
	if err := os.WriteFile(collision, []byte("unmanaged\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := store.Sync(
		context.Background(),
		syncTestGraph(packageID, definition),
		[]PackageID{packageID},
		destination,
		SyncOptions{},
	)
	if err == nil || !strings.Contains(err.Error(), "unmanaged path") {
		t.Fatalf("sync error = %v, want unmanaged-path error", err)
	}
	assertSyncFile(t, collision, "unmanaged\n", 0o600)
	assertSyncMissing(t, syncManifestPath(destination))
}

func TestSyncForceReplacesModifiedAndUnmanagedFiles(t *testing.T) {
	store := openTestStore(t)
	oldDefinition, oldID := writeSyncTestPackage(
		t,
		store,
		"old",
		func(root string) {
			writeSyncTestFile(t, root, "bin/managed", "old\n", 0o755)
		},
	)
	destination := t.TempDir()
	if _, err := store.Sync(
		context.Background(),
		syncTestGraph(oldID, oldDefinition),
		[]PackageID{oldID},
		destination,
		SyncOptions{},
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(destination, "bin", "managed"),
		[]byte("locally modified\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		outside,
		filepath.Join(destination, "bin", "collision"),
	); err != nil {
		t.Fatal(err)
	}

	newDefinition, newID := writeSyncTestPackage(
		t,
		store,
		"new",
		func(root string) {
			writeSyncTestFile(t, root, "bin/managed", "new\n", 0o755)
			writeSyncTestFile(t, root, "bin/collision", "claimed\n", 0o644)
		},
	)
	result, err := store.Sync(
		context.Background(),
		syncTestGraph(newID, newDefinition),
		[]PackageID{newID},
		destination,
		SyncOptions{Force: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []SyncChange{
		{Action: SyncReplaceModified, Path: "bin/managed"},
		{Action: SyncReplaceUnmanaged, Path: "bin/collision"},
	} {
		if !slices.Contains(result.Changes, expected) {
			t.Fatalf("sync changes = %+v, missing %+v", result.Changes, expected)
		}
	}
	assertSyncFile(
		t,
		filepath.Join(destination, "bin", "managed"),
		"new\n",
		0o755,
	)
	assertSyncFile(
		t,
		filepath.Join(destination, "bin", "collision"),
		"claimed\n",
		0o644,
	)
	assertSyncFile(t, outside, "keep\n", 0o600)
}

func TestSyncForceRemovesModifiedObsoleteFile(t *testing.T) {
	store := openTestStore(t)
	oldDefinition, oldID := writeSyncTestPackage(
		t,
		store,
		"old",
		func(root string) {
			writeSyncTestFile(t, root, "obsolete", "old\n", 0o644)
		},
	)
	destination := t.TempDir()
	if _, err := store.Sync(
		context.Background(),
		syncTestGraph(oldID, oldDefinition),
		[]PackageID{oldID},
		destination,
		SyncOptions{},
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(destination, "obsolete"),
		[]byte("modified\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	newDefinition, newID := writeSyncTestPackage(
		t,
		store,
		"new",
		func(root string) {
			writeSyncTestFile(t, root, "current", "new\n", 0o644)
		},
	)
	result, err := store.Sync(
		context.Background(),
		syncTestGraph(newID, newDefinition),
		[]PackageID{newID},
		destination,
		SyncOptions{Force: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.Changes, SyncChange{
		Action: SyncRemoveModified,
		Path:   "obsolete",
	}) {
		t.Fatalf("sync changes = %+v, missing remove-modified", result.Changes)
	}
	assertSyncMissing(t, filepath.Join(destination, "obsolete"))
	assertSyncFile(
		t,
		filepath.Join(destination, "current"),
		"new\n",
		0o644,
	)
}

func TestUninstallForceRemovesModifiedFiles(t *testing.T) {
	store := openTestStore(t)
	definition, packageID := writeSyncTestPackage(
		t,
		store,
		"hello",
		func(root string) {
			writeSyncTestFile(t, root, "hello", "hello\n", 0o644)
		},
	)
	destination := t.TempDir()
	if _, err := store.Sync(
		context.Background(),
		syncTestGraph(packageID, definition),
		[]PackageID{packageID},
		destination,
		SyncOptions{},
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(destination, "hello"),
		[]byte("modified\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	result, err := Uninstall(
		context.Background(),
		destination,
		UninstallOptions{Force: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(result.Changes, SyncChange{
		Action: SyncRemoveModified,
		Path:   "hello",
	}) {
		t.Fatalf(
			"uninstall changes = %+v, missing remove-modified",
			result.Changes,
		)
	}
	assertSyncMissing(t, filepath.Join(destination, "hello"))
	assertSyncMissing(t, syncManifestPath(destination))
}

func TestSyncDeleteUnmanagedRequiresForce(t *testing.T) {
	store := openTestStore(t)
	definition, packageID := writeSyncTestPackage(
		t,
		store,
		"hello",
		func(root string) {
			writeSyncTestFile(t, root, "hello", "hello\n", 0o644)
		},
	)
	destination := t.TempDir()
	_, err := store.Sync(
		context.Background(),
		syncTestGraph(packageID, definition),
		[]PackageID{packageID},
		destination,
		SyncOptions{DeleteUnmanaged: true},
	)
	if err == nil || !strings.Contains(err.Error(), "requires force") {
		t.Fatalf("sync error = %v, want force requirement", err)
	}
	assertSyncMissing(t, filepath.Join(destination, "hello"))
	assertSyncMissing(t, syncMetadataPath(destination))
}

func TestSyncDeleteUnmanagedDoesNotFollowSymlinks(t *testing.T) {
	store := openTestStore(t)
	definition, packageID := writeSyncTestPackage(
		t,
		store,
		"hello",
		func(root string) {
			writeSyncTestFile(t, root, "bin/hello", "hello\n", 0o755)
		},
	)
	graph := syncTestGraph(packageID, definition)
	destination := t.TempDir()
	if _, err := store.Sync(
		context.Background(),
		graph,
		[]PackageID{packageID},
		destination,
		SyncOptions{},
	); err != nil {
		t.Fatal(err)
	}

	writeSyncTestFile(t, destination, "bin/local", "local\n", 0o600)
	writeSyncTestFile(t, destination, "var/cache/data", "cache\n", 0o600)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(destination, "outside-link")); err != nil {
		t.Fatal(err)
	}

	result, err := store.Sync(
		context.Background(),
		graph,
		[]PackageID{packageID},
		destination,
		SyncOptions{
			Force:           true,
			DeleteUnmanaged: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"bin/local",
		"outside-link",
		"var/cache/data",
		"var/cache",
		"var",
	} {
		if !slices.Contains(result.Changes, SyncChange{
			Action: SyncDeleteUnmanaged,
			Path:   path,
		}) {
			t.Fatalf(
				"sync changes = %+v, missing delete-unmanaged %q",
				result.Changes,
				path,
			)
		}
		assertSyncMissing(t, filepath.Join(
			destination,
			filepath.FromSlash(path),
		))
	}
	assertSyncFile(
		t,
		filepath.Join(destination, "bin", "hello"),
		"hello\n",
		0o755,
	)
	assertSyncFile(t, outside, "keep\n", 0o600)
	if _, err := os.Stat(syncManifestPath(destination)); err != nil {
		t.Fatalf("sync manifest was deleted: %v", err)
	}
	if _, err := os.Stat(syncLockPath(destination)); err != nil {
		t.Fatalf("sync lock was deleted: %v", err)
	}
}

func TestSyncDeleteUnmanagedDoesNotCrossBindMounts(t *testing.T) {
	store := openTestStore(t)
	definition, packageID := writeSyncTestPackage(
		t,
		store,
		"empty",
		func(string) {},
	)
	destination := t.TempDir()
	mounted := filepath.Join(destination, "mounted")
	if err := os.Mkdir(mounted, 0o755); err != nil {
		t.Fatal(err)
	}
	mountSource := t.TempDir()
	writeSyncTestFile(t, mountSource, "keep", "keep\n", 0o600)
	if err := unix.Mount(
		mountSource,
		mounted,
		"",
		unix.MS_BIND,
		"",
	); err != nil {
		if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) {
			t.Skipf("bind mounts are not permitted: %v", err)
		}
		t.Fatalf("create bind mount: %v", err)
	}
	defer func() {
		if err := unix.Unmount(mounted, 0); err != nil {
			t.Errorf("unmount bind mount: %v", err)
		}
	}()

	result, err := store.Sync(
		context.Background(),
		syncTestGraph(packageID, definition),
		[]PackageID{packageID},
		destination,
		SyncOptions{
			Force:           true,
			DeleteUnmanaged: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range result.Changes {
		if change.Path == "mounted" ||
			strings.HasPrefix(change.Path, "mounted/") {
			t.Fatalf("sync planned a change inside bind mount: %+v", change)
		}
	}
	assertSyncFile(t, filepath.Join(mounted, "keep"), "keep\n", 0o600)
}

func TestSyncDryRunDoesNotInstallOrWriteManifest(t *testing.T) {
	store := openTestStore(t)
	definition, packageID := writeSyncTestPackage(
		t,
		store,
		"hello",
		func(root string) {
			writeSyncTestFile(t, root, "hello", "hello\n", 0o644)
		},
	)
	destination := t.TempDir()
	result, err := store.Sync(
		context.Background(),
		syncTestGraph(packageID, definition),
		[]PackageID{packageID},
		destination,
		SyncOptions{DryRun: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun {
		t.Fatal("dry-run result was not marked as a dry run")
	}
	if !slices.Contains(result.Changes, SyncChange{
		Action: SyncCreate,
		Path:   "hello",
	}) {
		t.Fatalf("dry-run changes = %+v", result.Changes)
	}
	assertSyncMissing(t, filepath.Join(destination, "hello"))
	assertSyncMissing(t, syncManifestPath(destination))
}

func TestSyncLeavesNonEmptyManagedDirectoryOnUninstall(t *testing.T) {
	store := openTestStore(t)
	definition, packageID := writeSyncTestPackage(
		t,
		store,
		"hello",
		func(root string) {
			writeSyncTestFile(t, root, "bin/hello", "hello\n", 0o755)
		},
	)
	destination := t.TempDir()
	if _, err := store.Sync(
		context.Background(),
		syncTestGraph(packageID, definition),
		[]PackageID{packageID},
		destination,
		SyncOptions{},
	); err != nil {
		t.Fatal(err)
	}
	unmanaged := filepath.Join(destination, "bin", "local")
	if err := os.WriteFile(unmanaged, []byte("local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(
		context.Background(),
		destination,
		UninstallOptions{},
	); err != nil {
		t.Fatal(err)
	}
	assertSyncFile(t, unmanaged, "local\n", 0o600)
	assertSyncMissing(t, filepath.Join(destination, "bin", "hello"))
}

func TestSyncFailsWhenDestinationLockIsBusy(t *testing.T) {
	store := openTestStore(t)
	definition, packageID := writeSyncTestPackage(
		t,
		store,
		"hello",
		func(root string) {
			writeSyncTestFile(t, root, "hello", "hello\n", 0o644)
		},
	)
	destination := t.TempDir()
	if err := os.Mkdir(syncMetadataPath(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	lock, busy, err := tryLock(
		context.Background(),
		syncLockPath(destination),
	)
	if err != nil {
		t.Fatal(err)
	}
	if busy {
		t.Fatal("fresh destination lock was busy")
	}
	defer lock.Close()

	_, err = store.Sync(
		context.Background(),
		syncTestGraph(packageID, definition),
		[]PackageID{packageID},
		destination,
		SyncOptions{},
	)
	if err == nil || !strings.Contains(err.Error(), "already being modified") {
		t.Fatalf("sync error = %v, want busy error", err)
	}
}

func writeSyncTestPackage(
	t *testing.T,
	store *Store,
	name string,
	populate func(string),
) (PackageDefinition, PackageID) {
	t.Helper()
	definition := PackageDefinition{Name: name}
	packageID := testPackageID(t, definition)
	root := t.TempDir()
	populate(root)
	if err := os.MkdirAll(
		filepath.Join(store.Path(), packageDirectoryName),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := createPackageArchive(
		context.Background(),
		root,
		store.packageArtifactPath(packageID, name),
	); err != nil {
		t.Fatal(err)
	}
	return definition, packageID
}

func syncTestGraph(
	packageID PackageID,
	definition PackageDefinition,
) *PackageGraph {
	return &PackageGraph{
		Nodes: map[PackageID]PackageDefinition{packageID: definition},
		Roots: map[string]PackageID{definition.Name: packageID},
	}
}

func writeSyncTestFile(
	t *testing.T,
	root string,
	relative string,
	contents string,
	mode os.FileMode,
) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

func assertSyncFile(
	t *testing.T,
	path string,
	contents string,
	mode os.FileMode,
) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	if string(data) != contents {
		t.Fatalf("contents of %q = %q, want %q", path, data, contents)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode {
		t.Fatalf(
			"mode of %q = %#o, want %#o",
			path,
			info.Mode().Perm(),
			mode,
		)
	}
}

func assertSyncMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %q exists or cannot be inspected: %v", path, err)
	}
}
