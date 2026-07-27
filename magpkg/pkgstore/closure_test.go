package pkgstore

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRuntimeClosureIsDependencyFirstAndExcludesBuildDeps(t *testing.T) {
	sharedDefinition := PackageDefinition{Name: "shared"}
	sharedID := testPackageID(t, sharedDefinition)
	firstDefinition := PackageDefinition{
		Name:    "first",
		RunDeps: []PackageID{sharedID},
	}
	firstID := testPackageID(t, firstDefinition)
	secondDefinition := PackageDefinition{Name: "second"}
	secondID := testPackageID(t, secondDefinition)
	buildOnlyDefinition := PackageDefinition{Name: "build-only"}
	buildOnlyID := testPackageID(t, buildOnlyDefinition)
	rootDefinition := PackageDefinition{
		Name:      "root",
		RunDeps:   []PackageID{firstID, secondID},
		BuildDeps: []PackageID{buildOnlyID},
	}
	rootID := testPackageID(t, rootDefinition)
	otherRootDefinition := PackageDefinition{
		Name:    "other-root",
		RunDeps: []PackageID{secondID},
	}
	otherRootID := testPackageID(t, otherRootDefinition)

	graph := &PackageGraph{Nodes: map[PackageID]PackageDefinition{
		sharedID:    sharedDefinition,
		firstID:     firstDefinition,
		secondID:    secondDefinition,
		buildOnlyID: buildOnlyDefinition,
		rootID:      rootDefinition,
		otherRootID: otherRootDefinition,
	}}
	got, err := graph.RuntimeClosure(rootID, otherRootID, rootID)
	if err != nil {
		t.Fatal(err)
	}
	want := []PackageID{
		sharedID,
		firstID,
		secondID,
		rootID,
		otherRootID,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RuntimeClosure() = %v, want %v", got, want)
	}
}

func TestExtractPackagesUsesOrderedOverlay(t *testing.T) {
	store := openTestStore(t)
	firstDefinition := PackageDefinition{Name: "first"}
	firstID := testPackageID(t, firstDefinition)
	secondDefinition := PackageDefinition{Name: "second"}
	secondID := testPackageID(t, secondDefinition)
	graph := &PackageGraph{Nodes: map[PackageID]PackageDefinition{
		firstID:  firstDefinition,
		secondID: secondDefinition,
	}}
	writeArchive := func(
		id PackageID,
		name string,
		contents string,
	) {
		t.Helper()
		source := t.TempDir()
		if err := os.MkdirAll(filepath.Join(source, "usr", "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(source, "usr", "bin", "tool"),
			[]byte(contents),
			0o755,
		); err != nil {
			t.Fatal(err)
		}
		artifact := store.packageArtifactPath(id, name)
		if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := createPackageArchive(
			context.Background(),
			source,
			artifact,
		); err != nil {
			t.Fatal(err)
		}
	}
	writeArchive(firstID, firstDefinition.Name, "first")
	writeArchive(secondID, secondDefinition.Name, "second")

	destination := t.TempDir()
	if err := store.ExtractPackages(
		context.Background(),
		graph,
		[]PackageID{firstID, secondID},
		destination,
	); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "usr", "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("overlaid file = %q, want second", got)
	}
}
