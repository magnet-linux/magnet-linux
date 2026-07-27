package pkgstore

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestExecutorSkipsBusyReadyBranch(t *testing.T) {
	store := openTestStore(t)
	plan, firstID, secondID := independentBranchPlan(t)

	if err := osMkdirBuildDirectories(store); err != nil {
		t.Fatal(err)
	}
	firstLock, err := acquireLock(context.Background(), store.packageLockPath(firstID))
	if err != nil {
		t.Fatal(err)
	}
	defer firstLock.Close() //nolint:errcheck

	secondStarted := make(chan struct{})
	builder := func(
		_ context.Context,
		_ *BuildPlan,
		planned PlannedPackage,
		_ normalizedBuildOptions,
	) error {
		if planned.ID == secondID {
			close(secondStarted)
		}
		return publishTestArtifact(store, planned.ID, planned.Definition.Name)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := store.executeBuildPlan(
			ctx,
			plan,
			normalizedBuildOptions{jobs: 1},
			builder,
		)
		done <- err
	}()

	select {
	case <-secondStarted:
		// The executor found the other branch while firstID was locked.
	case <-time.After(2 * time.Second):
		t.Fatal("executor waited on a busy package instead of scanning another branch")
	}
	if err := firstLock.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not finish after the busy lock was released")
	}
}

func TestExecutorsSharePlanThroughPackageLocks(t *testing.T) {
	store := openTestStore(t)
	plan, _, _ := independentBranchPlan(t)

	var mutex sync.Mutex
	buildCounts := make(map[PackageID]int)
	builder := func(
		ctx context.Context,
		_ *BuildPlan,
		planned PlannedPackage,
		_ normalizedBuildOptions,
	) error {
		mutex.Lock()
		buildCounts[planned.ID]++
		mutex.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
		return publishTestArtifact(store, planned.ID, planned.Definition.Name)
	}

	start := make(chan struct{})
	errors := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := store.executeBuildPlan(
				context.Background(),
				plan,
				normalizedBuildOptions{jobs: 1},
				builder,
			)
			errors <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}

	for _, planned := range plan.Packages {
		if buildCounts[planned.ID] != 1 {
			t.Fatalf(
				"package %s was built %d times, want exactly once",
				planned.ID,
				buildCounts[planned.ID],
			)
		}
	}
}

func TestExecutorRunsIndependentBranchesInParallel(t *testing.T) {
	store := openTestStore(t)
	plan, firstID, secondID := independentBranchPlan(t)

	started := make(chan PackageID, 2)
	release := make(chan struct{})
	builder := func(
		ctx context.Context,
		_ *BuildPlan,
		planned PlannedPackage,
		_ normalizedBuildOptions,
	) error {
		if planned.ID == firstID || planned.ID == secondID {
			started <- planned.ID
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
			}
		}
		return publishTestArtifact(store, planned.ID, planned.Definition.Name)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := store.executeBuildPlan(
			ctx,
			plan,
			normalizedBuildOptions{jobs: 2},
			builder,
		)
		done <- err
	}()

	seen := make(map[PackageID]bool)
	for range 2 {
		select {
		case packageID := <-started:
			seen[packageID] = true
		case <-time.After(2 * time.Second):
			t.Fatal("independent package builds did not start concurrently")
		}
	}
	close(release)
	if !seen[firstID] || !seen[secondID] {
		t.Fatalf("started packages = %v, want both independent branches", seen)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeInstallOrderAppliesBuildDependenciesBeforeRuntimeDependencies(
	t *testing.T,
) {
	bootstrap := PlannedPackage{
		ID:         PackageID("bootstrap"),
		Definition: PackageDefinition{Name: "bootstrap"},
	}
	runtime := PlannedPackage{
		ID:         PackageID("runtime"),
		Definition: PackageDefinition{Name: "runtime"},
	}
	toolchain := PlannedPackage{
		ID: PackageID("toolchain"),
		Definition: PackageDefinition{
			Name:    "toolchain",
			RunDeps: []PackageID{runtime.ID},
		},
	}
	root := PlannedPackage{
		ID: PackageID("root"),
		Definition: PackageDefinition{
			Name:      "root",
			BuildDeps: []PackageID{bootstrap.ID, toolchain.ID},
			RunDeps:   []PackageID{runtime.ID},
		},
	}
	plan := &BuildPlan{
		Packages: []PlannedPackage{bootstrap, runtime, toolchain, root},
	}

	order, err := runtimeInstallOrder(plan, root)
	if err != nil {
		t.Fatal(err)
	}
	want := []PackageID{bootstrap.ID, runtime.ID, toolchain.ID}
	if len(order) != len(want) {
		t.Fatalf("runtimeInstallOrder() = %v, want %v", order, want)
	}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("runtimeInstallOrder() = %v, want %v", order, want)
		}
	}
}

func independentBranchPlan(t *testing.T) (*BuildPlan, PackageID, PackageID) {
	t.Helper()
	firstDefinition := PackageDefinition{Name: "first"}
	firstID := testPackageID(t, firstDefinition)
	secondDefinition := PackageDefinition{Name: "second"}
	secondID := testPackageID(t, secondDefinition)
	rootDefinition := PackageDefinition{
		Name:    "root",
		RunDeps: []PackageID{firstID, secondID},
	}
	rootID := testPackageID(t, rootDefinition)
	return &BuildPlan{
		Roots: []PackageID{rootID},
		Packages: []PlannedPackage{
			{
				ID:          firstID,
				Definition:  firstDefinition,
				Disposition: BuildDispositionLocal,
			},
			{
				ID:          secondID,
				Definition:  secondDefinition,
				Disposition: BuildDispositionLocal,
			},
			{
				ID:            rootID,
				Definition:    rootDefinition,
				Prerequisites: []PackageID{firstID, secondID},
				Disposition:   BuildDispositionLocal,
			},
		},
	}, firstID, secondID
}

func osMkdirBuildDirectories(store *Store) error {
	for _, directory := range []string{
		filepath.Join(store.path, packageDirectoryName),
		filepath.Join(store.path, buildLockDirectory),
		filepath.Join(store.path, buildWorkDirectory),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	return nil
}
