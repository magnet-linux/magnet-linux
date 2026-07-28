package pkgstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

func TestCleanPackagesRemovesExpiredAndRetainsTouched(t *testing.T) {
	store := openTestStore(t)
	expiredDefinition := pkggraph.Definition{Name: "expired", Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone}}
	expiredID := testID(t, expiredDefinition)
	retainedDefinition := pkggraph.Definition{Name: "retained", Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone}}
	retainedID := testID(t, retainedDefinition)
	writeTestArtifact(t, store, expiredID, expiredDefinition.Name)
	writeTestArtifact(t, store, retainedID, retainedDefinition.Name)

	workRoot := filepath.Join(store.Path(), buildWorkDirectory)
	for _, packageID := range []pkggraph.ID{expiredID, retainedID} {
		workPath := filepath.Join(workRoot, string(packageID))
		if err := os.MkdirAll(workPath, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	old := time.Now().Add(-2 * time.Hour)
	for _, path := range []string{
		store.packageArtifactPath(expiredID, expiredDefinition.Name),
		store.packageArtifactPath(retainedID, retainedDefinition.Name),
		filepath.Join(workRoot, string(expiredID)),
		filepath.Join(workRoot, string(retainedID)),
	} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}

	graph := &pkggraph.Graph{
		Nodes: map[pkggraph.ID]pkggraph.Definition{
			retainedID: retainedDefinition,
		},
		Roots: map[string]pkggraph.ID{"retained": retainedID},
	}
	if _, err := store.PlanBuild(context.Background(), graph, retainedID); err != nil {
		t.Fatal(err)
	}

	var events []CleanEvent
	if err := store.CleanPackagesWithOptions(
		context.Background(),
		time.Hour,
		CleanOptions{
			OnRemove: func(event CleanEvent) {
				events = append(events, event)
			},
		},
	); err != nil {
		t.Fatal(err)
	}
	assertMissingPackagePath(
		t,
		store.packageArtifactPath(expiredID, expiredDefinition.Name),
	)
	assertRegularPackagePath(
		t,
		store.packageArtifactPath(retainedID, retainedDefinition.Name),
	)
	assertMissingPackagePath(t, filepath.Join(workRoot, string(expiredID)))
	assertMissingPackagePath(t, filepath.Join(workRoot, string(retainedID)))
	assertMissingPackagePath(t, store.packageLockPath(expiredID))
	assertMissingPackagePath(t, store.packageLockPath(retainedID))

	wantEvents := map[CleanEvent]bool{
		{
			Kind: CleanKindPackage,
			Path: store.packageArtifactPath(
				expiredID,
				expiredDefinition.Name,
			),
		}: true,
		{
			Kind: CleanKindWorkspace,
			Path: filepath.Join(workRoot, string(expiredID)),
		}: true,
		{
			Kind: CleanKindWorkspace,
			Path: filepath.Join(workRoot, string(retainedID)),
		}: true,
	}
	if len(events) != len(wantEvents) {
		t.Fatalf("cleanup events = %#v, want %#v", events, wantEvents)
	}
	for _, event := range events {
		if !wantEvents[event] {
			t.Fatalf("unexpected cleanup event %#v", event)
		}
	}
}

func TestCleanPackagesSkipsPackageHeldForUse(t *testing.T) {
	store := openTestStore(t)
	definition := pkggraph.Definition{Name: "in-use", Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone}}
	packageID := testID(t, definition)
	writeTestArtifact(t, store, packageID, definition.Name)
	artifactPath := store.packageArtifactPath(packageID, definition.Name)
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(artifactPath, old, old); err != nil {
		t.Fatal(err)
	}

	useStarted := make(chan struct{})
	releaseUse := make(chan struct{})
	useDone := make(chan error, 1)
	go func() {
		useDone <- store.usePackageArtifact(
			context.Background(),
			packageID,
			definition.Name,
			func(string) error {
				close(useStarted)
				<-releaseUse
				return nil
			},
		)
	}()
	<-useStarted

	if err := store.CleanPackages(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertRegularPackagePath(t, artifactPath)
	assertRegularPackagePath(t, store.packageLockPath(packageID))

	close(releaseUse)
	if err := <-useDone; err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(artifactPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := store.CleanPackages(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	assertMissingPackagePath(t, artifactPath)
	assertMissingPackagePath(t, store.packageLockPath(packageID))
}

func TestCleanPackagesAllIgnoresFutureModificationTime(t *testing.T) {
	store := openTestStore(t)
	definition := pkggraph.Definition{Name: "future", Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone}}
	packageID := testID(t, definition)
	writeTestArtifact(t, store, packageID, definition.Name)
	artifactPath := store.packageArtifactPath(packageID, definition.Name)
	future := time.Now().Add(24 * time.Hour)
	if err := os.Chtimes(artifactPath, future, future); err != nil {
		t.Fatal(err)
	}

	if err := store.CleanPackages(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	assertMissingPackagePath(t, artifactPath)
}

func TestCleanPackagesIgnoresUnknownEntries(t *testing.T) {
	store := openTestStore(t)
	for _, relative := range []string{
		filepath.Join(packageDirectoryName, "unknown.tar.zst"),
		filepath.Join(buildLockDirectory, "unknown.lock"),
	} {
		path := filepath.Join(store.Path(), relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("unknown"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.CleanPackages(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	assertRegularPackagePath(
		t,
		filepath.Join(store.Path(), packageDirectoryName, "unknown.tar.zst"),
	)
	assertRegularPackagePath(
		t,
		filepath.Join(store.Path(), buildLockDirectory, "unknown.lock"),
	)
}

func TestCleanPackagesValidationAndMissingDirectories(t *testing.T) {
	store := openTestStore(t)
	if err := store.CleanPackages(context.Background(), time.Hour); err != nil {
		t.Fatalf("CleanPackages() on empty store: %v", err)
	}
	if err := store.CleanPackages(context.Background(), -time.Second); err == nil ||
		!strings.Contains(err.Error(), "must not be negative") {
		t.Fatalf("negative maxAge error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.CleanPackages(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled CleanPackages() error = %v, want context.Canceled", err)
	}

	var nilStore *Store
	if err := nilStore.CleanPackages(context.Background(), time.Hour); err == nil ||
		!strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("nil store error = %v", err)
	}
}

func TestPackageArtifactEntryID(t *testing.T) {
	packageID := pkggraph.ID(strings.Repeat("a", pkggraph.IDLength))
	for _, name := range []string{
		packageArtifactFilename(packageID, ""),
		packageArtifactFilename(packageID, "gcc-15.1"),
	} {
		if got, ok := packageArtifactEntryID(name); !ok || got != packageID {
			t.Fatalf("packageArtifactEntryID(%q) = %q, %v", name, got, ok)
		}
	}
	for _, name := range []string{
		"unknown.tar.zst",
		"gcc" + string(packageID) + packageArtifactSuffix,
		"gcc-" + strings.ToUpper(string(packageID)) + packageArtifactSuffix,
	} {
		if got, ok := packageArtifactEntryID(name); ok {
			t.Fatalf("packageArtifactEntryID(%q) = %q, true", name, got)
		}
	}
}

func assertRegularPackagePath(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%q is not a regular file", path)
	}
}

func assertMissingPackagePath(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %q exists or cannot be inspected: %v", path, err)
	}
}
