package pkgstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPlanBuildIncludesBuildDependenciesOnlyWhenNeeded(t *testing.T) {
	store := openTestStore(t)

	runtimeDefinition := PackageDefinition{Name: "runtime"}
	runtimeID := testPackageID(t, runtimeDefinition)
	buildDefinition := PackageDefinition{Name: "build-tool"}
	buildID := testPackageID(t, buildDefinition)
	rootDefinition := PackageDefinition{
		Name:      "root",
		RunDeps:   []PackageID{runtimeID},
		BuildDeps: []PackageID{buildID},
	}
	rootID := testPackageID(t, rootDefinition)
	graph := &PackageGraph{Nodes: map[PackageID]PackageDefinition{
		runtimeID: runtimeDefinition,
		buildID:   buildDefinition,
		rootID:    rootDefinition,
	}}

	plan, err := store.PlanBuild(context.Background(), graph, rootID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plannedIDs(plan), []PackageID{runtimeID, buildID, rootID}; !slices.Equal(got, want) {
		t.Fatalf("planned package order = %v, want %v", got, want)
	}
	for _, planned := range plan.Packages {
		if planned.Disposition != BuildDispositionLocal {
			t.Fatalf("package %s disposition = %q, want %q", planned.ID, planned.Disposition, BuildDispositionLocal)
		}
	}

	writeTestArtifact(t, store, rootID, rootDefinition.Name)
	plan, err = store.PlanBuild(context.Background(), graph, rootID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plannedIDs(plan), []PackageID{runtimeID, rootID}; !slices.Equal(got, want) {
		t.Fatalf("available-root package order = %v, want %v", got, want)
	}
	if plan.Packages[len(plan.Packages)-1].Disposition != BuildDispositionAvailable {
		t.Fatalf("root disposition = %q, want %q", plan.Packages[len(plan.Packages)-1].Disposition, BuildDispositionAvailable)
	}
}

func TestPlanBuildRejectsDefinitionWithWrongID(t *testing.T) {
	store := openTestStore(t)
	definition := PackageDefinition{Name: "package"}
	wrongID := PackageID(strings.Repeat("a", packageIDLength))
	graph := &PackageGraph{Nodes: map[PackageID]PackageDefinition{
		wrongID: definition,
	}}
	_, err := store.PlanBuild(context.Background(), graph, wrongID)
	if err == nil {
		t.Fatal("PlanBuild accepted an invalid package ID")
	}
}

func TestPackageIDUsesLowercaseUnpaddedBase32(t *testing.T) {
	packageID := packageIDFromCanonical([]byte("canonical package definition"))
	const want PackageID = "al6yavugrwtiaphzx4bjvb4fns4fwmsevxdzonrd6btbdozfelea"
	if packageID != want {
		t.Fatalf("package ID = %q, want %q", packageID, want)
	}
	if err := validatePackageID(packageID); err != nil {
		t.Fatalf("validate generated package ID: %v", err)
	}

	for _, invalid := range []PackageID{
		PackageID(strings.ToUpper(string(packageID))),
		PackageID(string(packageID[:packageIDLength-1]) + "1"),
		packageID + "=",
	} {
		if err := validatePackageID(invalid); err == nil {
			t.Fatalf("accepted invalid package ID %q", invalid)
		}
	}
}

func TestFetchBuildInputsRunsFetchesInParallel(t *testing.T) {
	for _, test := range []struct {
		name       string
		jobs       int
		wantActive int64
	}{
		{name: "explicit limit", jobs: 2, wantActive: 2},
		{name: "default limit", jobs: 0, wantActive: DefaultFetchJobs},
	} {
		t.Run(test.name, func(t *testing.T) {
			var active atomic.Int64
			var maximumActive atomic.Int64
			contents := make(map[string][]byte)
			server := httptest.NewServer(http.HandlerFunc(
				func(writer http.ResponseWriter, request *http.Request) {
					current := active.Add(1)
					defer active.Add(-1)
					for {
						maximum := maximumActive.Load()
						if current <= maximum ||
							maximumActive.CompareAndSwap(maximum, current) {
							break
						}
					}
					time.Sleep(75 * time.Millisecond)
					_, _ = writer.Write(contents[request.URL.Path])
				},
			))
			defer server.Close()

			definition := PackageDefinition{Name: "parallel-fetches"}
			for index := range 6 {
				filename := "source-" + strconv.Itoa(index)
				path := "/" + filename
				content := []byte("contents for " + filename)
				contents[path] = content
				digest := sha256.Sum256(content)
				definition.Fetch = append(definition.Fetch, Fetch{
					Filename: filename,
					SHA256:   hex.EncodeToString(digest[:]),
					URLs:     []string{server.URL + path},
				})
			}
			packageID := testPackageID(t, definition)
			plan := &BuildPlan{
				Roots: []PackageID{packageID},
				Packages: []PlannedPackage{{
					ID:          packageID,
					Definition:  definition,
					Disposition: BuildDispositionLocal,
				}},
			}

			store := openTestStore(t)
			if err := store.FetchBuildInputsWithOptions(
				context.Background(),
				plan,
				FetchInputsOptions{Jobs: test.jobs},
			); err != nil {
				t.Fatal(err)
			}
			if got := maximumActive.Load(); got != test.wantActive {
				t.Fatalf(
					"maximum concurrent fetches = %d, want %d",
					got,
					test.wantActive,
				)
			}
		})
	}
}

func TestFetchBuildInputsRejectsNegativeJobs(t *testing.T) {
	store := openTestStore(t)
	definition := PackageDefinition{Name: "negative-fetch-jobs"}
	packageID := testPackageID(t, definition)
	err := store.FetchBuildInputsWithOptions(
		context.Background(),
		&BuildPlan{
			Roots: []PackageID{packageID},
			Packages: []PlannedPackage{{
				ID:          packageID,
				Definition:  definition,
				Disposition: BuildDispositionLocal,
			}},
		},
		FetchInputsOptions{Jobs: -1},
	)
	if err == nil {
		t.Fatal("FetchBuildInputsWithOptions accepted negative jobs")
	}
	if got, want := err.Error(), "pkgstore: fetch jobs must not be negative"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func testPackageID(t *testing.T, definition PackageDefinition) PackageID {
	t.Helper()
	packageID, err := packageIDForDefinition(definition)
	if err != nil {
		t.Fatal(err)
	}
	return packageID
}

func plannedIDs(plan *BuildPlan) []PackageID {
	result := make([]PackageID, len(plan.Packages))
	for index, planned := range plan.Packages {
		result[index] = planned.ID
	}
	return result
}

func writeTestArtifact(
	t *testing.T,
	store *Store,
	packageID PackageID,
	packageName string,
) {
	t.Helper()
	if err := publishTestArtifact(store, packageID, packageName); err != nil {
		t.Fatal(err)
	}
}

func publishTestArtifact(
	store *Store,
	packageID PackageID,
	packageName string,
) error {
	artifactPath := store.packageArtifactPath(packageID, packageName)
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(artifactPath, []byte("artifact"), 0o644); err != nil {
		return err
	}
	return nil
}
