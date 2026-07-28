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

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

func TestPlanBuildIncludesBuildDependenciesOnlyWhenNeeded(t *testing.T) {
	store := openTestStore(t)

	runtimeDefinition := pkggraph.Definition{Name: "runtime", Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone}}
	runtimeID := testID(t, runtimeDefinition)
	buildDefinition := pkggraph.Definition{Name: "build-tool", Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone}}
	buildID := testID(t, buildDefinition)
	rootDefinition := pkggraph.Definition{
		Name:      "root",
		Build:     pkggraph.BuildSpec{Kind: pkggraph.BuildNone},
		RunDeps:   []pkggraph.ID{runtimeID},
		BuildDeps: []pkggraph.ID{buildID},
	}
	rootID := testID(t, rootDefinition)
	graph := testGraph(t, nil, runtimeDefinition, buildDefinition, rootDefinition)

	plan, err := store.PlanBuild(context.Background(), graph, rootID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plannedIDs(plan), []pkggraph.ID{runtimeID, buildID, rootID}; !slices.Equal(got, want) {
		t.Fatalf("planned package order = %v, want %v", got, want)
	}
	for _, planned := range plan.packages {
		if planned.Disposition != BuildDispositionNeedsBuild {
			t.Fatalf("package %s disposition = %q, want %q", planned.ID, planned.Disposition, BuildDispositionNeedsBuild)
		}
	}

	writeTestArtifact(t, store, rootID, rootDefinition.Name)
	plan, err = store.PlanBuild(context.Background(), graph, rootID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plannedIDs(plan), []pkggraph.ID{runtimeID, rootID}; !slices.Equal(got, want) {
		t.Fatalf("available-root package order = %v, want %v", got, want)
	}
	if plan.packages[len(plan.packages)-1].Disposition != BuildDispositionAvailable {
		t.Fatalf("root disposition = %q, want %q", plan.packages[len(plan.packages)-1].Disposition, BuildDispositionAvailable)
	}
}

func TestBuildPlanInspectionDoesNotMutatePlan(t *testing.T) {
	store := openTestStore(t)
	definition := pkggraph.Definition{
		Name:  "package",
		Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone},
	}
	packageID := testID(t, definition)
	graph := testGraph(t, nil, definition)
	plan, err := store.PlanBuild(context.Background(), graph, packageID)
	if err != nil {
		t.Fatal(err)
	}
	roots := plan.Roots()
	packages := plan.Packages()
	roots[0] = ""
	packages[0].Definition.Name = "modified"
	if got := plan.Roots()[0]; got != packageID {
		t.Fatalf("plan root = %s, want %s", got, packageID)
	}
	if got := plan.Packages()[0].Definition.Name; got != definition.Name {
		t.Fatalf("plan definition name = %q, want %q", got, definition.Name)
	}
}

func TestBuildPlanIsBoundToItsStore(t *testing.T) {
	firstStore := openTestStore(t)
	secondStore := openTestStore(t)
	definition := pkggraph.Definition{
		Name:  "package",
		Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone},
	}
	packageID := testID(t, definition)
	graph := testGraph(t, nil, definition)
	plan, err := firstStore.PlanBuild(context.Background(), graph, packageID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = secondStore.ExecuteBuildPlan(
		context.Background(),
		plan,
		ExecuteOptions{},
	)
	if err == nil || !strings.Contains(err.Error(), "build plan belongs to store") {
		t.Fatalf("ExecuteBuildPlan() error = %v, want store mismatch", err)
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

			definition := pkggraph.Definition{Name: "parallel-fetches", Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone}}
			for index := range 6 {
				filename := "source-" + strconv.Itoa(index)
				path := "/" + filename
				content := []byte("contents for " + filename)
				contents[path] = content
				digest := sha256.Sum256(content)
				definition.Fetch = append(definition.Fetch, pkggraph.Fetch{
					Filename: filename,
					SHA256:   hex.EncodeToString(digest[:]),
					URLs:     []string{server.URL + path},
				})
			}
			packageID := testID(t, definition)
			plan := &BuildPlan{
				roots: []pkggraph.ID{packageID},
				packages: []PlannedPackage{{
					ID:          packageID,
					Definition:  definition,
					Disposition: BuildDispositionNeedsBuild,
				}},
			}

			store := openTestStore(t)
			plan.storePath = store.path
			if err := store.FetchBuildInputs(
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
	definition := pkggraph.Definition{Name: "negative-fetch-jobs", Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone}}
	packageID := testID(t, definition)
	err := store.FetchBuildInputs(
		context.Background(),
		&BuildPlan{
			storePath: store.path,
			roots:     []pkggraph.ID{packageID},
			packages: []PlannedPackage{{
				ID:          packageID,
				Definition:  definition,
				Disposition: BuildDispositionNeedsBuild,
			}},
		},
		FetchInputsOptions{Jobs: -1},
	)
	if err == nil {
		t.Fatal("FetchBuildInputs accepted negative jobs")
	}
	if got, want := err.Error(), "pkgstore: fetch jobs must not be negative"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestBuildPackagesValidatesOptionsBeforeFetching(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		requests.Add(1)
		_, _ = writer.Write([]byte("source"))
	}))
	defer server.Close()

	digest := sha256.Sum256([]byte("source"))
	definition := pkggraph.Definition{
		Name:  "invalid-options",
		Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone},
		Fetch: []pkggraph.Fetch{{
			Filename: "source",
			SHA256:   hex.EncodeToString(digest[:]),
			URLs:     []string{server.URL},
		}},
	}
	packageID := testID(t, definition)
	graph := testGraph(t, map[string]pkggraph.ID{"package": packageID}, definition)
	store := openTestStore(t)
	_, err := store.BuildPackages(
		context.Background(),
		graph,
		[]pkggraph.ID{packageID},
		BuildOptions{Jobs: -1},
	)
	if err == nil || !strings.Contains(err.Error(), "build jobs must not be negative") {
		t.Fatalf("BuildPackages() error = %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("source requests = %d, want 0", got)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	store, err := Open(root, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func testID(t *testing.T, definition pkggraph.Definition) pkggraph.ID {
	t.Helper()
	packageID, err := pkggraph.IDForDefinition(definition)
	if err != nil {
		t.Fatal(err)
	}
	return packageID
}

// testGraph seals definitions, given dependency-first, and roots into a
// package graph.
func testGraph(
	t *testing.T,
	roots map[string]pkggraph.ID,
	definitions ...pkggraph.Definition,
) *pkggraph.Graph {
	t.Helper()
	var builder pkggraph.Builder
	for _, definition := range definitions {
		if _, err := builder.AddDefinition(definition); err != nil {
			t.Fatal(err)
		}
	}
	for name, packageID := range roots {
		if err := builder.SetRoot(name, packageID); err != nil {
			t.Fatal(err)
		}
	}
	return builder.Graph()
}

func plannedIDs(plan *BuildPlan) []pkggraph.ID {
	result := make([]pkggraph.ID, len(plan.packages))
	for index, planned := range plan.packages {
		result[index] = planned.ID
	}
	return result
}

func writeTestArtifact(
	t *testing.T,
	store *Store,
	packageID pkggraph.ID,
	packageName string,
) {
	t.Helper()
	if err := publishTestArtifact(store, packageID, packageName); err != nil {
		t.Fatal(err)
	}
}

func publishTestArtifact(
	store *Store,
	packageID pkggraph.ID,
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
