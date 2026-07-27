package pkgstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageArtifactFilename(t *testing.T) {
	packageID := PackageID(strings.Repeat("a", packageIDLength))
	if got, want := packageArtifactFilename(packageID, "gcc"), "gcc-"+string(packageID)+".tar.zst"; got != want {
		t.Fatalf("packageArtifactFilename() = %q, want %q", got, want)
	}
	if got, want := packageArtifactFilename(packageID, ""), string(packageID)+".tar.zst"; got != want {
		t.Fatalf("packageArtifactFilename() without name = %q, want %q", got, want)
	}
}

func TestPackageAvailableDoesNotSearchOtherFilenames(t *testing.T) {
	store := openTestStore(t)
	packageID := PackageID(strings.Repeat("b", packageIDLength))
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
	definition := PackageDefinition{}
	packageID := testPackageID(t, definition)
	graph := &PackageGraph{
		Nodes: map[PackageID]PackageDefinition{packageID: definition},
		Roots: map[string]PackageID{"unnamed": packageID},
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

func TestValidatePackageName(t *testing.T) {
	valid := []string{
		"",
		"a",
		"gcc-15.1.0",
		"libstdc++-rt",
		"package_name",
		strings.Repeat("a", maximumPackageNameLength),
	}
	for _, name := range valid {
		if err := validatePackageName(name); err != nil {
			t.Errorf("validatePackageName(%q): %v", name, err)
		}
	}

	invalid := []string{
		".hidden",
		"-option",
		"has space",
		"has/slash",
		"naïve",
		strings.Repeat("a", maximumPackageNameLength+1),
	}
	for _, name := range invalid {
		if err := validatePackageName(name); err == nil {
			t.Errorf("validatePackageName(%q) succeeded", name)
		}
	}
}
