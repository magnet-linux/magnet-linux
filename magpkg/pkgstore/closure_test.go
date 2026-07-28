package pkgstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

func TestExtractPackagesUsesOrderedOverlay(t *testing.T) {
	store := openTestStore(t)
	firstDefinition := pkggraph.Definition{Name: "first", Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone}}
	firstID := testID(t, firstDefinition)
	secondDefinition := pkggraph.Definition{Name: "second", Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone}}
	secondID := testID(t, secondDefinition)
	graph := &pkggraph.Graph{Nodes: map[pkggraph.ID]pkggraph.Definition{
		firstID:  firstDefinition,
		secondID: secondDefinition,
	}}
	writeArchive := func(
		id pkggraph.ID,
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
		[]pkggraph.ID{firstID, secondID},
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
