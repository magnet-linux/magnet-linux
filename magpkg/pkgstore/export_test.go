package pkgstore

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

func TestExportArchiveComposesRuntimeClosure(t *testing.T) {
	store := openTestStore(t)
	dependency := pkggraph.Definition{
		Name:  "dependency",
		Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone},
	}
	dependencyID := testID(t, dependency)
	root := pkggraph.Definition{
		Name:    "root",
		Build:   pkggraph.BuildSpec{Kind: pkggraph.BuildNone},
		RunDeps: []pkggraph.ID{dependencyID},
	}
	rootID := testID(t, root)
	writeExportTestArtifact(t, store, dependencyID, dependency.Name, func(path string) {
		writeSyncTestFile(t, path, "bin/tool", "dependency\n", 0o755)
		writeSyncTestFile(t, path, "lib/dependency", "library\n", 0o644)
	})
	writeExportTestArtifact(t, store, rootID, root.Name, func(path string) {
		writeSyncTestFile(t, path, "bin/tool", "root\n", 0o755)
		writeSyncTestFile(t, path, "run.sh", "#!/bin/sh\n", 0o755)
	})
	graph := &pkggraph.Graph{
		Nodes: map[pkggraph.ID]pkggraph.Definition{
			dependencyID: dependency,
			rootID:       root,
		},
		Roots: map[string]pkggraph.ID{"root": rootID},
	}

	var output bytes.Buffer
	closure, err := store.ExportArchive(
		context.Background(),
		graph,
		[]pkggraph.ID{rootID},
		&output,
		ExportOptions{Format: ExportFormatTar},
	)
	if err != nil {
		t.Fatal(err)
	}
	if want := []pkggraph.ID{dependencyID, rootID}; !slices.Equal(closure, want) {
		t.Fatalf("export closure = %v, want %v", closure, want)
	}

	archivePath := filepath.Join(t.TempDir(), "environment.tar")
	if err := os.WriteFile(archivePath, output.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	extracted := t.TempDir()
	if err := extractArchive(context.Background(), archivePath, extracted); err != nil {
		t.Fatal(err)
	}
	for relative, want := range map[string]string{
		"bin/tool":       "root\n",
		"lib/dependency": "library\n",
		"run.sh":         "#!/bin/sh\n",
	} {
		contents, err := os.ReadFile(filepath.Join(extracted, relative))
		if err != nil {
			t.Fatal(err)
		}
		if string(contents) != want {
			t.Fatalf("%s contents = %q, want %q", relative, contents, want)
		}
	}
}

func TestExportArchiveRequiresExistingArtifacts(t *testing.T) {
	store := openTestStore(t)
	definition := pkggraph.Definition{
		Name:  "missing",
		Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone},
	}
	packageID := testID(t, definition)
	graph := &pkggraph.Graph{
		Nodes: map[pkggraph.ID]pkggraph.Definition{packageID: definition},
		Roots: map[string]pkggraph.ID{"missing": packageID},
	}

	var output bytes.Buffer
	_, err := store.ExportArchive(
		context.Background(),
		graph,
		[]pkggraph.ID{packageID},
		&output,
		ExportOptions{Format: ExportFormatTar},
	)
	if err == nil || !strings.Contains(err.Error(), "extract package") {
		t.Fatalf("ExportArchive() error = %v, want missing artifact error", err)
	}
	if output.Len() != 0 {
		t.Fatalf("ExportArchive wrote %d bytes for missing closure", output.Len())
	}
}

func TestExportArchiveRejectsInvalidOptionsBeforeUsingArtifacts(t *testing.T) {
	store := openTestStore(t)
	var output bytes.Buffer
	_, err := store.ExportArchive(
		context.Background(),
		&pkggraph.Graph{},
		nil,
		&output,
		ExportOptions{Format: ExportFormat("zip")},
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported export format") {
		t.Fatalf("ExportArchive() error = %v, want format error", err)
	}
	if output.Len() != 0 {
		t.Fatalf("ExportArchive wrote %d bytes for invalid options", output.Len())
	}
}

func writeExportTestArtifact(
	t *testing.T,
	store *Store,
	packageID pkggraph.ID,
	name string,
	populate func(string),
) {
	t.Helper()
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
}
