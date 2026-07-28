package pkgstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

func TestAcquireTempDirIsLockedUntilClosed(t *testing.T) {
	store := openTestStore(t)
	lease, err := acquireTempDir(context.Background(), store.path, "sync-")
	if err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(store.path, tempDirectoryName)
	if !strings.HasPrefix(lease.path, root+string(os.PathSeparator)) {
		t.Fatalf("temporary path %q is not under %q", lease.path, root)
	}
	assertDirectory(t, lease.path)
	assertTempRegularFile(t, lease.path+tempLockSuffix)

	// A live lease holds its lock, so cleanup must skip it.
	if err := store.CleanTempFiles(
		context.Background(),
		CleanOptions{OnRemove: func(event CleanEvent) {
			t.Fatalf("cleaned %s %q while leased", event.Kind, event.Path)
		}},
	); err != nil {
		t.Fatal(err)
	}
	assertDirectory(t, lease.path)

	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	assertTempPathMissing(t, lease.path)
	assertTempPathMissing(t, lease.path+tempLockSuffix)
}

func TestCleanTempFilesRemovesAbandonedPathsRegardlessOfAge(t *testing.T) {
	store := openTestStore(t)
	root := filepath.Join(store.path, tempDirectoryName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	// A crashed owner leaves its directory and unheld lock behind.
	abandoned := filepath.Join(root, "sync-abandoned")
	if err := os.MkdirAll(filepath.Join(abandoned, "usr", "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		abandoned+tempLockSuffix,
		nil,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	// A crash between reserving a lock and creating its directory leaves an
	// orphan lock file.
	orphanLock := filepath.Join(root, "export-orphan") + tempLockSuffix
	if err := os.WriteFile(orphanLock, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	var events []CleanEvent
	if err := store.CleanTempFiles(
		context.Background(),
		CleanOptions{OnRemove: func(event CleanEvent) {
			events = append(events, event)
		}},
	); err != nil {
		t.Fatal(err)
	}

	assertTempPathMissing(t, abandoned)
	assertTempPathMissing(t, abandoned+tempLockSuffix)
	assertTempPathMissing(t, orphanLock)
	if len(events) != 1 ||
		events[0].Kind != CleanKindTemp ||
		events[0].Path != abandoned {
		t.Fatalf("clean events = %#v, want one temp removal of %q", events, abandoned)
	}
}

func TestSyncLeavesNoTemporaryFiles(t *testing.T) {
	store := openTestStore(t)
	definition, packageID := writeSyncTestPackage(
		t,
		store,
		"hello",
		func(root string) {
			writeSyncTestFile(t, root, "bin/hello", "hello\n", 0o755)
		},
	)

	if _, err := store.Sync(
		context.Background(),
		syncTestGraph(packageID, definition),
		[]pkggraph.ID{packageID},
		t.TempDir(),
		SyncOptions{},
	); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(filepath.Join(store.path, tempDirectoryName))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("temporary root has leftover entries %v", names)
	}
}

func assertDirectory(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("%q is not a directory", path)
	}
}

func assertTempRegularFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%q is not a regular file", path)
	}
}

func assertTempPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %q still exists (err = %v)", path, err)
	}
}
