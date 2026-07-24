package pkgstore_test

import (
	"path/filepath"
	"testing"

	"github.com/andrewchambers/magpkg/pkgstore"
)

func TestInit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store")

	if err := pkgstore.Init(path); err != nil {
		t.Fatal(err)
	}

	store, err := pkgstore.Open(path)
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
	_, err := pkgstore.Open(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("Open() succeeded for a missing store")
	}
}
