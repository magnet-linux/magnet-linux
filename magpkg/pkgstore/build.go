package pkgstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

// Artifact describes a package artifact available in the store.
type Artifact struct {
	ID        pkggraph.ID
	Name      string
	StorePath string
}

// BuildOptions controls complete package realization through local artifacts,
// binary-cache substitution, source fetching, and local builds.
type BuildOptions struct {
	// CacheOnly permits existing local artifacts and binary-cache
	// substitutions but prohibits source fetching and local package builds.
	CacheOnly bool

	// Jobs is the maximum number of package builds performed concurrently by
	// this process. Zero selects one job.
	Jobs int

	// FetchJobs is the maximum number of source fetches performed concurrently.
	// Zero selects DefaultFetchJobs.
	FetchJobs int

	// BuildParallelism is exposed to package scripts as BUILD_PARALLELISM.
	// Zero selects one.
	BuildParallelism int

	// Stdout and Stderr receive line-prefixed package build output. Nil uses
	// the process standard output or standard error.
	Stdout io.Writer
	Stderr io.Writer

	// FetchProgress receives source-fetch progress updates.
	FetchProgress FetchProgressFunc
}

// ExecuteOptions controls execution of an already-fetched BuildPlan.
type ExecuteOptions struct {
	// Jobs is the maximum number of package builds performed concurrently.
	// Zero selects one job.
	Jobs int

	// BuildParallelism is exposed to package scripts as BUILD_PARALLELISM.
	// Zero selects one.
	BuildParallelism int

	// Stdout and Stderr receive line-prefixed package build output. Nil uses
	// the process standard output or standard error.
	Stdout io.Writer
	Stderr io.Writer
}

type normalizedExecuteOptions struct {
	jobs             int
	buildParallelism int
	stdout           io.Writer
	stderr           io.Writer
	outputMutex      *sync.Mutex
}

// Build realizes packageID and its dependencies using binary-cache
// substitution when configured and one local package-build job when needed.
func (s *Store) Build(
	ctx context.Context,
	graph *pkggraph.Graph,
	packageID pkggraph.ID,
) (*Artifact, error) {
	packages, err := s.BuildPackages(ctx, graph, packageID)
	if err != nil {
		return nil, err
	}
	return packages[0], nil
}

// BuildWithOptions realizes packageID and its dependencies with options.
func (s *Store) BuildWithOptions(
	ctx context.Context,
	graph *pkggraph.Graph,
	packageID pkggraph.ID,
	options BuildOptions,
) (*Artifact, error) {
	packages, err := s.BuildPackagesWithOptions(
		ctx,
		graph,
		[]pkggraph.ID{packageID},
		options,
	)
	if err != nil {
		return nil, err
	}
	return packages[0], nil
}

// BuildPackages realizes packageIDs and their dependencies. Returned packages
// are ordered like the deduplicated input roots.
func (s *Store) BuildPackages(
	ctx context.Context,
	graph *pkggraph.Graph,
	packageIDs ...pkggraph.ID,
) ([]*Artifact, error) {
	return s.BuildPackagesWithOptions(
		ctx,
		graph,
		packageIDs,
		BuildOptions{},
	)
}

// BuildPackagesWithOptions is BuildPackages with realization controls.
func (s *Store) BuildPackagesWithOptions(
	ctx context.Context,
	graph *pkggraph.Graph,
	packageIDs []pkggraph.ID,
	options BuildOptions,
) ([]*Artifact, error) {
	if err := validateBuildOptions(options); err != nil {
		return nil, err
	}
	if err := s.substituteBuildClosure(
		ctx,
		graph,
		packageIDs,
		options.CacheOnly,
	); err != nil {
		return nil, err
	}
	plan, err := s.PlanBuild(ctx, graph, packageIDs...)
	if err != nil {
		return nil, err
	}
	if options.CacheOnly {
		for _, planned := range plan.packages {
			if planned.Disposition == BuildDispositionNeedsBuild {
				return nil, fmt.Errorf(
					"pkgstore: package %s still requires a local build "+
						"after cache-only substitution",
					planned.ID,
				)
			}
		}
	}
	if err := s.FetchBuildInputsWithOptions(ctx, plan, FetchInputsOptions{
		Jobs:     options.FetchJobs,
		Progress: options.FetchProgress,
	}); err != nil {
		return nil, err
	}
	packages, err := s.ExecuteBuildPlan(
		ctx,
		plan,
		executeOptionsFromBuildOptions(options),
	)
	if err != nil {
		return nil, err
	}
	roots := make([]*Artifact, 0, len(plan.roots))
	for _, packageID := range plan.roots {
		pkg, ok := packages[packageID]
		if !ok {
			return nil, fmt.Errorf(
				"build result for package %s is missing",
				packageID,
			)
		}
		roots = append(roots, pkg)
	}
	return roots, nil
}

// ExecuteBuildPlan realizes plan. It scans the dependency-first package list
// for ready work and uses a non-blocking package lock. If a ready package is
// being built by another process, execution continues with another ready
// branch instead of waiting on that lock.
func (s *Store) ExecuteBuildPlan(
	ctx context.Context,
	plan *BuildPlan,
	options ExecuteOptions,
) (map[pkggraph.ID]*Artifact, error) {
	normalized, err := normalizeExecuteOptions(options)
	if err != nil {
		return nil, err
	}
	return s.executeBuildPlan(ctx, plan, normalized, s.buildPlannedPackage)
}

type plannedPackageBuilder func(
	context.Context,
	*BuildPlan,
	PlannedPackage,
	normalizedExecuteOptions,
) error

type buildResult struct {
	packageID pkggraph.ID
	err       error
}

func (s *Store) executeBuildPlan(
	ctx context.Context,
	plan *BuildPlan,
	options normalizedExecuteOptions,
	builder plannedPackageBuilder,
) (map[pkggraph.ID]*Artifact, error) {
	if err := validateStore(s); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateBuildPlanForStore(s, plan); err != nil {
		return nil, err
	}
	for _, directory := range []string{
		filepath.Join(s.path, packageDirectoryName),
		filepath.Join(s.path, buildLockDirectory),
		filepath.Join(s.path, buildWorkDirectory),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, fmt.Errorf("create build store directory: %w", err)
		}
	}

	executionContext, cancel := context.WithCancel(ctx)
	defer cancel()

	pending := make(map[pkggraph.ID]struct{}, len(plan.packages))
	plannedByID := make(map[pkggraph.ID]PlannedPackage, len(plan.packages))
	for _, planned := range plan.packages {
		pending[planned.ID] = struct{}{}
		plannedByID[planned.ID] = planned
	}
	active := make(map[pkggraph.ID]struct{})
	results := make(chan buildResult, options.jobs)
	var firstErr error

	receiveResult := func() {
		result := <-results
		delete(active, result.packageID)
		if result.err != nil && firstErr == nil {
			firstErr = fmt.Errorf("build package %s: %w", result.packageID, result.err)
			cancel()
			return
		}
		if result.err == nil {
			delete(pending, result.packageID)
		}
	}

	for len(pending) > 0 || len(active) > 0 {
		if firstErr != nil {
			for len(active) > 0 {
				receiveResult()
			}
			return nil, firstErr
		}
		if err := executionContext.Err(); err != nil {
			firstErr = err
			continue
		}

		progress := false
		for _, planned := range plan.packages {
			if len(active) >= options.jobs {
				break
			}
			if _, ok := pending[planned.ID]; !ok {
				continue
			}
			if _, ok := active[planned.ID]; ok {
				continue
			}

			available, err := s.packageAvailableForUse(
				executionContext,
				planned.ID,
				planned.Definition.Name,
			)
			if err != nil {
				firstErr = fmt.Errorf("inspect package %s: %w", planned.ID, err)
				cancel()
				break
			}
			if available {
				delete(pending, planned.ID)
				progress = true
				continue
			}
			if planned.Disposition == BuildDispositionAvailable {
				firstErr = fmt.Errorf(
					"package %s disappeared after planning; create a new build plan",
					planned.ID,
				)
				cancel()
				break
			}

			ready, err := s.prerequisitesAvailable(planned, plannedByID)
			if err != nil {
				firstErr = err
				cancel()
				break
			}
			if !ready {
				continue
			}

			lock, busy, err := tryLock(executionContext, s.packageLockPath(planned.ID))
			if err != nil {
				firstErr = fmt.Errorf("lock package %s: %w", planned.ID, err)
				cancel()
				break
			}
			if busy {
				continue
			}

			// Another executor may have published the artifact just before
			// this lock was acquired.
			available, err = s.packageAvailable(
				planned.ID,
				planned.Definition.Name,
			)
			if err != nil {
				_ = lock.Close()
				firstErr = fmt.Errorf("reinspect package %s: %w", planned.ID, err)
				cancel()
				break
			}
			if available {
				if err := lock.Close(); err != nil {
					firstErr = fmt.Errorf("unlock package %s: %w", planned.ID, err)
					cancel()
					break
				}
				delete(pending, planned.ID)
				progress = true
				continue
			}
			ready, err = s.prerequisitesAvailable(planned, plannedByID)
			if err != nil || !ready {
				closeErr := lock.Close()
				if err := errors.Join(err, closeErr); err != nil {
					firstErr = fmt.Errorf("recheck package %s: %w", planned.ID, err)
					cancel()
					break
				}
				continue
			}

			active[planned.ID] = struct{}{}
			progress = true
			go func(planned PlannedPackage, lock *fileLock) {
				err := builder(executionContext, plan, planned, options)
				if err == nil {
					available, inspectErr := s.packageAvailable(
						planned.ID,
						planned.Definition.Name,
					)
					if inspectErr != nil {
						err = inspectErr
					} else if !available {
						err = errors.New("builder returned without publishing an artifact")
					}
				}
				err = errors.Join(err, lock.Close())
				results <- buildResult{packageID: planned.ID, err: err}
			}(planned, lock)
		}

		if firstErr != nil {
			continue
		}
		if len(active) > 0 {
			receiveResult()
			continue
		}
		if len(pending) == 0 {
			break
		}
		if progress {
			continue
		}

		// All currently-ready packages are locked by other executors. Poll
		// artifacts and locks without coordinating through a central queue.
		timer := time.NewTimer(lockPollInterval)
		select {
		case <-executionContext.Done():
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
		}
	}

	packages := make(map[pkggraph.ID]*Artifact, len(plan.packages))
	for _, planned := range plan.packages {
		available, err := s.packageAvailable(
			planned.ID,
			planned.Definition.Name,
		)
		if err != nil {
			return nil, fmt.Errorf("inspect package %s: %w", planned.ID, err)
		}
		if !available {
			return nil, fmt.Errorf(
				"package %s has no artifact after execution",
				planned.ID,
			)
		}
		packages[planned.ID] = &Artifact{
			ID:   planned.ID,
			Name: planned.Definition.Name,
			StorePath: s.packageArtifactPath(
				planned.ID,
				planned.Definition.Name,
			),
		}
	}
	return packages, nil
}

func (s *Store) prerequisitesAvailable(
	planned PlannedPackage,
	plannedByID map[pkggraph.ID]PlannedPackage,
) (bool, error) {
	for _, dependency := range planned.Prerequisites {
		dependencyPlan, ok := plannedByID[dependency]
		if !ok {
			return false, fmt.Errorf(
				"package %s prerequisite %s is not in the build plan",
				planned.ID,
				dependency,
			)
		}
		available, err := s.packageAvailable(
			dependency,
			dependencyPlan.Definition.Name,
		)
		if err != nil {
			return false, fmt.Errorf("inspect prerequisite %s: %w", dependency, err)
		}
		if !available {
			return false, nil
		}
	}
	return true, nil
}

func normalizeExecuteOptions(options ExecuteOptions) (normalizedExecuteOptions, error) {
	if options.Jobs < 0 {
		return normalizedExecuteOptions{}, errors.New("pkgstore: build jobs must not be negative")
	}
	if options.BuildParallelism < 0 {
		return normalizedExecuteOptions{}, errors.New(
			"pkgstore: build parallelism must not be negative",
		)
	}
	if options.Jobs == 0 {
		options.Jobs = 1
	}
	if options.BuildParallelism == 0 {
		options.BuildParallelism = 1
	}
	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}
	return normalizedExecuteOptions{
		jobs:             options.Jobs,
		buildParallelism: options.BuildParallelism,
		stdout:           options.Stdout,
		stderr:           options.Stderr,
		outputMutex:      &sync.Mutex{},
	}, nil
}

func validateBuildOptions(options BuildOptions) error {
	if options.FetchJobs < 0 {
		return errors.New("pkgstore: fetch jobs must not be negative")
	}
	_, err := normalizeExecuteOptions(executeOptionsFromBuildOptions(options))
	return err
}

func executeOptionsFromBuildOptions(options BuildOptions) ExecuteOptions {
	return ExecuteOptions{
		Jobs:             options.Jobs,
		BuildParallelism: options.BuildParallelism,
		Stdout:           options.Stdout,
		Stderr:           options.Stderr,
	}
}

func (s *Store) buildPlannedPackage(
	ctx context.Context,
	plan *BuildPlan,
	planned PlannedPackage,
	options normalizedExecuteOptions,
) error {
	workRoot := s.packageWorkPath(planned.ID)
	if err := os.RemoveAll(workRoot); err != nil {
		return fmt.Errorf("remove previous build workspace: %w", err)
	}
	rootfs := filepath.Join(workRoot, "root")
	buildDirectory := filepath.Join(workRoot, "build")
	outputDirectory := filepath.Join(workRoot, "output")
	fetchDirectory := filepath.Join(workRoot, "fetch")
	for _, directory := range []string{
		rootfs,
		buildDirectory,
		outputDirectory,
		fetchDirectory,
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create build workspace: %w", err)
		}
	}

	sources := make([]string, len(planned.Definition.Fetch))
	for index, source := range planned.Definition.Fetch {
		fetchedPath, err := s.Fetch(ctx, source)
		if err != nil {
			return fmt.Errorf("fetch %q: %w", source.Filename, err)
		}
		sources[index] = fetchedPath
	}

	if planned.Definition.Build.Kind == pkggraph.BuildUnpack {
		for index, sourcePath := range sources {
			if err := extractArchive(ctx, sourcePath, outputDirectory); err != nil {
				return fmt.Errorf(
					"unpack %q: %w",
					planned.Definition.Fetch[index].Filename,
					err,
				)
			}
		}
	} else {
		if err := s.materializeBuildRoot(ctx, plan, planned, rootfs); err != nil {
			return err
		}
		for index, sourcePath := range sources {
			destination := filepath.Join(
				fetchDirectory,
				planned.Definition.Fetch[index].Filename,
			)
			if err := copyRegularFile(ctx, sourcePath, destination, 0o444); err != nil {
				return fmt.Errorf(
					"stage fetch %q: %w",
					planned.Definition.Fetch[index].Filename,
					err,
				)
			}
		}
		if planned.Definition.Build.Kind == pkggraph.BuildScript {
			outputMutex := options.outputMutex
			if outputMutex == nil {
				outputMutex = &sync.Mutex{}
			}
			prefix := buildOutputPrefix(planned)
			stdout := newBuildOutputWriter(options.stdout, outputMutex, prefix)
			stderr := newBuildOutputWriter(options.stderr, outputMutex, prefix)
			buildOptions := options
			buildOptions.stdout = stdout
			buildOptions.stderr = stderr
			buildErr := runBuildSandbox(
				ctx,
				rootfs,
				buildDirectory,
				outputDirectory,
				fetchDirectory,
				planned.Definition.Build.Script,
				planned.Definition.BuildEnv,
				buildOptions,
			)
			if err := errors.Join(buildErr, stdout.Flush(), stderr.Flush()); err != nil {
				return err
			}
		}
	}

	temporaryArtifact := filepath.Join(workRoot, "artifact"+packageArtifactSuffix)
	if err := createPackageArchive(ctx, outputDirectory, temporaryArtifact); err != nil {
		return fmt.Errorf("archive package output: %w", err)
	}
	finalArtifact := s.packageArtifactPath(
		planned.ID,
		planned.Definition.Name,
	)
	available, err := s.packageAvailable(
		planned.ID,
		planned.Definition.Name,
	)
	if err != nil {
		return fmt.Errorf("inspect package before publication: %w", err)
	}
	if !available {
		if err := os.Rename(temporaryArtifact, finalArtifact); err != nil {
			return fmt.Errorf("publish package artifact: %w", err)
		}
		if err := syncDirectory(filepath.Dir(finalArtifact)); err != nil {
			return fmt.Errorf("sync package store: %w", err)
		}
	}
	if err := os.RemoveAll(workRoot); err != nil {
		return fmt.Errorf("remove successful build workspace: %w", err)
	}
	return nil
}

func (s *Store) materializeBuildRoot(
	ctx context.Context,
	plan *BuildPlan,
	planned PlannedPackage,
	rootfs string,
) error {
	order, err := runtimeInstallOrder(plan, planned)
	if err != nil {
		return err
	}
	byID := make(map[pkggraph.ID]PlannedPackage, len(plan.packages))
	for _, item := range plan.packages {
		byID[item.ID] = item
	}
	for _, dependency := range order {
		dependencyPlan, ok := byID[dependency]
		if !ok {
			return fmt.Errorf("dependency %s is not in the build plan", dependency)
		}
		if err := s.extractPackageReferences(
			ctx,
			[]packageReference{{
				id:   dependency,
				name: dependencyPlan.Definition.Name,
			}},
			rootfs,
		); err != nil {
			return fmt.Errorf("install dependency %s: %w", dependency, err)
		}
	}
	for _, directory := range []string{"build", "out", "fetch", "tmp", "proc", "dev"} {
		mountPoint := filepath.Join(rootfs, directory)
		if err := replaceNonDirectory(mountPoint); err != nil {
			return fmt.Errorf("prepare sandbox mount point /%s: %w", directory, err)
		}
		if err := os.Mkdir(mountPoint, 0o755); err != nil &&
			!errors.Is(err, os.ErrExist) {
			return fmt.Errorf("prepare sandbox mount point /%s: %w", directory, err)
		}
	}
	return nil
}

func runtimeInstallOrder(plan *BuildPlan, planned PlannedPackage) ([]pkggraph.ID, error) {
	byID := make(map[pkggraph.ID]PlannedPackage, len(plan.packages))
	for _, item := range plan.packages {
		byID[item.ID] = item
	}
	state := make(map[pkggraph.ID]visitState)
	var order []pkggraph.ID
	var visit func(pkggraph.ID) error
	visit = func(packageID pkggraph.ID) error {
		switch state[packageID] {
		case visitActive:
			return fmt.Errorf("runtime dependency cycle through package %s", packageID)
		case visitDone:
			return nil
		}
		dependency, ok := byID[packageID]
		if !ok {
			return fmt.Errorf("runtime dependency %s is not in the build plan", packageID)
		}
		state[packageID] = visitActive
		for _, runtimeDependency := range dependency.Definition.RunDeps {
			if err := visit(runtimeDependency); err != nil {
				return err
			}
		}
		state[packageID] = visitDone
		order = append(order, packageID)
		return nil
	}
	// Apply explicitly ordered build dependencies first. Runtime dependencies
	// then fill out the target closure without allowing a bootstrap build root
	// to overwrite a rebuilt toolchain that appears later in BuildDeps.
	for _, packageID := range append(
		append([]pkggraph.ID(nil), planned.Definition.BuildDeps...),
		planned.Definition.RunDeps...,
	) {
		if err := visit(packageID); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func copyRegularFile(
	ctx context.Context,
	source string,
	destination string,
	mode os.FileMode,
) (retErr error) {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	outputOpen := true
	defer func() {
		if outputOpen {
			retErr = errors.Join(retErr, output.Close())
		}
	}()
	if _, err := copyContext(ctx, output, input); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	outputOpen = false
	return nil
}

func buildParallelismValue(options normalizedExecuteOptions) string {
	return strconv.Itoa(options.buildParallelism)
}
