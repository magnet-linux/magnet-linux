package pkgstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

func TestPackageArtifactFilename(t *testing.T) {
	packageID := pkggraph.ID(strings.Repeat("a", pkggraph.IDLength))
	if got, want := packageArtifactFilename(packageID, "gcc"), "gcc-"+string(packageID)+".tar.zst"; got != want {
		t.Fatalf("packageArtifactFilename() = %q, want %q", got, want)
	}
	if got, want := packageArtifactFilename(packageID, ""), string(packageID)+".tar.zst"; got != want {
		t.Fatalf("packageArtifactFilename() without name = %q, want %q", got, want)
	}
}

func TestPackageAvailableDoesNotSearchOtherFilenames(t *testing.T) {
	store := openTestStore(t)
	packageID := pkggraph.ID(strings.Repeat("b", pkggraph.IDLength))
	unnamedPath := store.packageArtifactPath(packageID, "")
	if err := os.MkdirAll(filepath.Dir(unnamedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unnamedPath, []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}

	available, err := store.packageAvailable(packageID, "package-name")
	if err != nil {
		t.Fatal(err)
	}
	if available {
		t.Fatal("named package unexpectedly found the unnamed artifact")
	}
	available, err = store.packageAvailable(packageID, "")
	if err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Fatal("unnamed package did not find its exact artifact path")
	}
}

func TestBuildPublishesUnnamedPackageArtifact(t *testing.T) {
	store := openTestStore(t)
	definition := pkggraph.Definition{Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone}}
	packageID := testID(t, definition)
	graph := &pkggraph.Graph{
		Nodes: map[pkggraph.ID]pkggraph.Definition{packageID: definition},
		Roots: map[string]pkggraph.ID{"unnamed": packageID},
	}

	pkg, err := store.Build(context.Background(), graph, packageID)
	if err != nil {
		t.Fatal(err)
	}
	want := store.packageArtifactPath(packageID, "")
	if pkg.StorePath != want {
		t.Fatalf("StorePath = %q, want %q", pkg.StorePath, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}
}
