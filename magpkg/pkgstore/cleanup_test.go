package pkgstore_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andrewchambers/magpkg/pkgstore"
	"golang.org/x/sys/unix"
)

func TestCleanupFetchesRemovesExpiredAndRetainsTouched(t *testing.T) {
	store := newTestStore(t)
	expiredPath := fetchTestSource(t, store, "expired", []byte("expired source"))
	retainedPath := fetchTestSource(t, store, "retained", []byte("retained source"))

	old := time.Now().Add(-2 * time.Hour)
	for _, path := range []string{expiredPath, retainedPath} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}

	// A cache hit refreshes the source's age without requiring its URLs.
	retainedDigest := filepath.Base(retainedPath)
	gotPath, err := store.Fetch(context.Background(), pkgstore.Fetch{
		Filename: "retained",
		SHA256:   retainedDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != retainedPath {
		t.Fatalf("Fetch() path = %q, want %q", gotPath, retainedPath)
	}

	if err := store.CleanupFetches(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertPathMissing(t, expiredPath)
	assertPathMissing(t, expiredPath+".lock")
	assertRegularFile(t, retainedPath)
	assertPathMissing(t, retainedPath+".lock")
}

func TestCleanupFetchesSkipsActiveLock(t *testing.T) {
	store := newTestStore(t)
	cachedPath := fetchTestSource(t, store, "source", []byte("locked source"))
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(cachedPath, old, old); err != nil {
		t.Fatal(err)
	}

	lockPath := cachedPath + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX); err != nil {
		_ = lockFile.Close()
		t.Fatal(err)
	}

	if err := store.CleanupFetches(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertRegularFile(t, cachedPath)
	assertRegularFile(t, lockPath)

	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := lockFile.Close(); err != nil {
		t.Fatal(err)
	}

	if err := store.CleanupFetches(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertPathMissing(t, cachedPath)
	assertPathMissing(t, lockPath)
}

func TestCleanupFetchesPartialsAndUnknownEntries(t *testing.T) {
	store := newTestStore(t)
	fetchRoot := filepath.Join(store.Path(), "fetch")
	if err := os.MkdirAll(fetchRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	expiredDigest := strings.Repeat("1", 64)
	recentDigest := strings.Repeat("2", 64)
	expiredPartial := filepath.Join(fetchRoot, expiredDigest+".tmp")
	recentPartial := filepath.Join(fetchRoot, recentDigest+".tmp")
	unknown := filepath.Join(fetchRoot, "unrecognized.tmp")
	for _, path := range []string{expiredPartial, recentPartial, unknown} {
		if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(expiredPartial, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(unknown, old, old); err != nil {
		t.Fatal(err)
	}

	if err := store.CleanupFetches(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertPathMissing(t, expiredPartial)
	assertPathMissing(t, filepath.Join(fetchRoot, expiredDigest+".lock"))
	assertRegularFile(t, recentPartial)
	assertPathMissing(t, filepath.Join(fetchRoot, recentDigest+".lock"))
	assertRegularFile(t, unknown)
}

func TestCleanupFetchesValidationAndMissingCache(t *testing.T) {
	store := newTestStore(t)
	if err := store.CleanupFetches(context.Background(), time.Hour); err != nil {
		t.Fatalf("CleanupFetches() on missing cache: %v", err)
	}

	if err := store.CleanupFetches(context.Background(), -time.Second); err == nil ||
		!strings.Contains(err.Error(), "must not be negative") {
		t.Fatalf("negative maxAge error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.CleanupFetches(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled CleanupFetches() error = %v, want context.Canceled", err)
	}

	var nilStore *pkgstore.Store
	if err := nilStore.CleanupFetches(context.Background(), time.Hour); err == nil ||
		!strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("nil store error = %v", err)
	}
}

func fetchTestSource(
	t *testing.T,
	store *pkgstore.Store,
	filename string,
	content []byte,
) string {
	t.Helper()
	sourcePath := filepath.Join(t.TempDir(), filename)
	if err := os.WriteFile(sourcePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	sourceURL := (&url.URL{Scheme: "file", Path: sourcePath}).String()
	path, err := store.Fetch(context.Background(), pkgstore.Fetch{
		Filename: filename,
		SHA256:   sha256Hex(content),
		URLs:     []string{sourceURL},
	})
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func assertRegularFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%q is not a regular file", path)
	}
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %q exists or cannot be inspected: %v", path, err)
	}
}
