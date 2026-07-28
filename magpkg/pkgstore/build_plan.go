package pkgstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

// BuildDisposition describes how a package was expected to be realized when
// its build plan was created.
type BuildDisposition string

const (
	// BuildDispositionAvailable means the package artifact was already in the
	// local store when the plan was created.
	BuildDispositionAvailable BuildDisposition = "available"

	// BuildDispositionNeedsBuild means the package must be built from source.
	BuildDispositionNeedsBuild BuildDisposition = "build"
)

// PlannedPackage describes one step returned when inspecting a BuildPlan.
// Mutating the returned value does not modify the plan.
type PlannedPackage struct {
	ID            pkggraph.ID
	Definition    pkggraph.Definition
	Prerequisites []pkggraph.ID
	Disposition   BuildDisposition
}

// BuildPlan is an immutable, store-bound, dependency-first snapshot of the
// work needed to realize its roots.
//
// A plan may be safely executed by more than one process. Package locks and
// atomic artifact publication decide which process performs each build.
type BuildPlan struct {
	storePath string
	roots     []pkggraph.ID
	packages  []PlannedPackage
}

// Roots returns the deduplicated requested package IDs.
func (plan *BuildPlan) Roots() []pkggraph.ID {
	if plan == nil {
		return nil
	}
	return slices.Clone(plan.roots)
}

// Packages returns the plan steps in dependency-first order.
func (plan *BuildPlan) Packages() []PlannedPackage {
	if plan == nil {
		return nil
	}
	packages := make([]PlannedPackage, len(plan.packages))
	for index, planned := range plan.packages {
		packages[index] = clonePlannedPackage(planned)
	}
	return packages
}

// DefaultFetchJobs is the default maximum number of source fetches performed
// concurrently by FetchBuildInputs.
const DefaultFetchJobs = 4

// PlanBuild creates a static plan for packageIDs. Existing packages contribute
// their runtime dependencies to the plan; packages that need building
// contribute both runtime and build dependencies.
func (s *Store) PlanBuild(
	ctx context.Context,
	graph *pkggraph.Graph,
	packageIDs ...pkggraph.ID,
) (*BuildPlan, error) {
	if err := validateStore(s); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if graph == nil {
		return nil, errors.New("pkgstore: package graph is nil")
	}
	if len(packageIDs) == 0 {
		return nil, errors.New("pkgstore: build plan has no roots")
	}

	planner := buildPlanner{
		ctx:   ctx,
		store: s,
		graph: graph,
		seen:  make(map[pkggraph.ID]struct{}),
	}
	roots := make([]pkggraph.ID, 0, len(packageIDs))
	rootSet := make(map[pkggraph.ID]struct{}, len(packageIDs))
	for _, packageID := range packageIDs {
		if _, exists := rootSet[packageID]; exists {
			continue
		}
		if err := planner.visit(packageID); err != nil {
			return nil, err
		}
		rootSet[packageID] = struct{}{}
		roots = append(roots, packageID)
	}

	plan := &BuildPlan{
		storePath: s.path,
		roots:     roots,
		packages:  planner.packages,
	}
	if err := validateBuildPlanForStore(s, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// FetchInputsOptions controls fetching all inputs for a build plan.
type FetchInputsOptions struct {
	// Jobs is the maximum number of source fetches performed concurrently.
	// Zero selects DefaultFetchJobs.
	Jobs int

	// Progress receives concurrent source-fetch progress updates.
	Progress FetchProgressFunc
}

// FetchBuildInputs fetches and verifies every source needed by the local-build
// actions in plan. Sources shared by multiple packages are fetched once.
func (s *Store) FetchBuildInputs(
	ctx context.Context,
	plan *BuildPlan,
	options FetchInputsOptions,
) error {
	if err := validateStore(s); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateBuildPlanForStore(s, plan); err != nil {
		return err
	}
	if options.Jobs < 0 {
		return errors.New("pkgstore: fetch jobs must not be negative")
	}
	if options.Jobs == 0 {
		options.Jobs = DefaultFetchJobs
	}

	type fetchTask struct {
		packageID   pkggraph.ID
		packageName string
		source      pkggraph.Fetch
	}
	tasks := make([]fetchTask, 0)
	fetched := make(map[string]struct{})
	for _, planned := range plan.packages {
		if planned.Disposition != BuildDispositionNeedsBuild {
			continue
		}
		available, err := s.packageAvailableForUse(
			ctx,
			planned.ID,
			planned.Definition.Name,
		)
		if err != nil {
			return fmt.Errorf("inspect package %s before fetching inputs: %w", planned.ID, err)
		}
		if available {
			continue
		}
		for _, source := range planned.Definition.Fetch {
			if err := ctx.Err(); err != nil {
				return err
			}
			_, digest, err := parseFetchSHA256(source.SHA256)
			if err != nil {
				return fmt.Errorf("package %s fetch %q: %w", planned.ID, source.Filename, err)
			}
			if _, ok := fetched[digest]; ok {
				continue
			}
			fetched[digest] = struct{}{}
			tasks = append(tasks, fetchTask{
				packageID:   planned.ID,
				packageName: planned.Definition.Name,
				source:      source,
			})
		}
	}
	if len(tasks) == 0 {
		return nil
	}

	workerCount := min(options.Jobs, len(tasks))
	taskQueue := make(chan fetchTask, len(tasks))
	for _, task := range tasks {
		taskQueue <- task
	}
	close(taskQueue)

	fetchContext, cancel := context.WithCancel(ctx)
	defer cancel()

	var workers sync.WaitGroup
	var firstError error
	var firstErrorOnce sync.Once
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for task := range taskQueue {
				if fetchContext.Err() != nil {
					return
				}
				if _, err := s.Fetch(
					fetchContext,
					task.source,
					FetchOptions{Progress: options.Progress},
				); err != nil {
					firstErrorOnce.Do(func() {
						firstError = fmt.Errorf(
							"fetch inputs for package %s (%s): %w",
							task.packageID,
							task.packageName,
							err,
						)
						cancel()
					})
					return
				}
			}
		}()
	}
	workers.Wait()
	if firstError != nil {
		return firstError
	}
	return ctx.Err()
}

type buildPlanner struct {
	ctx      context.Context
	store    *Store
	graph    *pkggraph.Graph
	seen     map[pkggraph.ID]struct{}
	packages []PlannedPackage
}

func (p *buildPlanner) visit(packageID pkggraph.ID) error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	if _, ok := p.seen[packageID]; ok {
		return nil
	}

	definition, ok := p.graph.Definition(packageID)
	if !ok {
		return fmt.Errorf("pkgstore: package %q is not in the graph", packageID)
	}

	available, err := p.store.packageAvailableForUse(
		p.ctx,
		packageID,
		definition.Name,
	)
	if err != nil {
		return fmt.Errorf("inspect package %s: %w", packageID, err)
	}
	disposition := BuildDispositionNeedsBuild
	prerequisites := append([]pkggraph.ID(nil), definition.RunDeps...)
	if available {
		disposition = BuildDispositionAvailable
	} else {
		prerequisites = append(prerequisites, definition.BuildDeps...)
	}
	prerequisites = deduplicateIDs(prerequisites)

	p.seen[packageID] = struct{}{}
	for _, dependency := range prerequisites {
		if err := p.visit(dependency); err != nil {
			return fmt.Errorf("package %s dependency %s: %w", packageID, dependency, err)
		}
	}
	p.packages = append(p.packages, PlannedPackage{
		ID:            packageID,
		Definition:    definition,
		Prerequisites: prerequisites,
		Disposition:   disposition,
	})
	return nil
}

func validateBuildPlan(plan *BuildPlan) error {
	if plan == nil {
		return errors.New("pkgstore: build plan is nil")
	}
	if len(plan.roots) == 0 {
		return errors.New("pkgstore: build plan has no roots")
	}
	seen := make(map[pkggraph.ID]struct{}, len(plan.packages))
	for _, planned := range plan.packages {
		if _, ok := seen[planned.ID]; ok {
			return fmt.Errorf("pkgstore: package %s occurs more than once in build plan", planned.ID)
		}
		switch planned.Disposition {
		case BuildDispositionAvailable, BuildDispositionNeedsBuild:
		default:
			return fmt.Errorf(
				"pkgstore: package %s has invalid build disposition %q",
				planned.ID,
				planned.Disposition,
			)
		}

		expected := planned.Definition.RunDeps
		if planned.Disposition == BuildDispositionNeedsBuild {
			expected = append(
				slices.Clone(planned.Definition.RunDeps),
				planned.Definition.BuildDeps...,
			)
		}
		expected = deduplicateIDs(expected)
		if !slices.Equal(planned.Prerequisites, expected) {
			return fmt.Errorf("pkgstore: package %s has invalid prerequisites", planned.ID)
		}
		for _, dependency := range planned.Prerequisites {
			if _, ok := seen[dependency]; !ok {
				return fmt.Errorf(
					"pkgstore: package %s is ordered before prerequisite %s",
					planned.ID,
					dependency,
				)
			}
		}
		seen[planned.ID] = struct{}{}
	}
	for _, root := range plan.roots {
		if _, ok := seen[root]; !ok {
			return fmt.Errorf("pkgstore: build plan root %s is missing", root)
		}
	}
	return nil
}

func validateBuildPlanForStore(store *Store, plan *BuildPlan) error {
	if err := validateBuildPlan(plan); err != nil {
		return err
	}
	if plan.storePath != store.path {
		return fmt.Errorf(
			"pkgstore: build plan belongs to store %q, not %q",
			plan.storePath,
			store.path,
		)
	}
	return nil
}

func deduplicateIDs(packageIDs []pkggraph.ID) []pkggraph.ID {
	result := make([]pkggraph.ID, 0, len(packageIDs))
	seen := make(map[pkggraph.ID]struct{}, len(packageIDs))
	for _, packageID := range packageIDs {
		if _, ok := seen[packageID]; ok {
			continue
		}
		seen[packageID] = struct{}{}
		result = append(result, packageID)
	}
	return result
}

func clonePlannedPackage(planned PlannedPackage) PlannedPackage {
	planned.Definition = pkggraph.CloneDefinition(planned.Definition)
	planned.Prerequisites = slices.Clone(planned.Prerequisites)
	return planned
}
