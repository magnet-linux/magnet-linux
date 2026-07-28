package pkgstore_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
)

func TestInit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store")

	if err := pkgstore.Init(path); err != nil {
		t.Fatal(err)
	}
	if err := pkgstore.Init(path); err != nil {
		t.Fatalf("second Init() failed: %v", err)
	}

	store, err := pkgstore.Open(path, pkgstore.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.Path() != absolutePath {
		t.Fatalf("Path() = %q, want %q", store.Path(), absolutePath)
	}
}

func TestOpenMissingStore(t *testing.T) {
	_, err := pkgstore.Open(filepath.Join(t.TempDir(), "missing"), pkgstore.OpenOptions{})
	if err == nil {
		t.Fatal("Open() succeeded for a missing store")
	}
}

func TestOpenRejectsDirectoryWithoutStoreMetadata(t *testing.T) {
	_, err := pkgstore.Open(t.TempDir(), pkgstore.OpenOptions{})
	if err == nil || !strings.Contains(err.Error(), "store metadata") {
		t.Fatalf("Open() error = %v, want missing metadata error", err)
	}
}

func TestOpenRejectsUnsupportedStoreMetadata(t *testing.T) {
	path := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(path, ".magpkg-store.json"),
		[]byte(`{"format":"magpkg-store","version":999}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	_, err := pkgstore.Open(path, pkgstore.OpenOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported store format version") {
		t.Fatalf("Open() error = %v, want unsupported version error", err)
	}
}

func TestOpenValidatesBinaryCacheURLWithoutContactingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store")
	if err := pkgstore.Init(path); err != nil {
		t.Fatal(err)
	}
	if _, err := pkgstore.Open(path, pkgstore.OpenOptions{
		BinaryCacheURL: "https://cache.invalid/base",
	}); err != nil {
		t.Fatalf("Open() contacted or rejected a valid URL: %v", err)
	}
	for _, cacheURL := range []string{
		"file:///tmp/cache",
		"http:///missing-host",
		"https://cache.invalid/?query=value",
	} {
		if _, err := pkgstore.Open(path, pkgstore.OpenOptions{
			BinaryCacheURL: cacheURL,
		}); err == nil {
			t.Fatalf("Open() accepted cache URL %q", cacheURL)
		}
	}
}
