package pkgstore

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// BuildDisposition describes how a package was expected to be realized when
// its build plan was created.
type BuildDisposition string

const (
	// BuildDispositionAvailable means the package artifact was already in the
	// local store when the plan was created.
	BuildDispositionAvailable BuildDisposition = "available"

	// BuildDispositionLocal means the package must be built from source.
	BuildDispositionLocal BuildDisposition = "build"
)

// PlannedPackage is one immutable step in a BuildPlan. Prerequisites are
// ordered before the package in BuildPlan.Packages.
type PlannedPackage struct {
	ID            PackageID
	Definition    PackageDefinition
	Prerequisites []PackageID
	Disposition   BuildDisposition
}

// BuildPlan is a deterministic, dependency-first snapshot of the work needed
// to realize Roots.
//
// A plan may be safely executed by more than one process. Package locks and
// atomic artifact publication decide which process performs each build.
type BuildPlan struct {
	Roots    []PackageID
	Packages []PlannedPackage
}

const maximumPackageNameLength = 128

// DefaultFetchJobs is the default maximum number of source fetches performed
// concurrently by FetchBuildInputs.
const DefaultFetchJobs = 4

// PlanBuild creates a static plan for packageIDs. Existing packages contribute
// their runtime dependencies to the plan; packages that need building
// contribute both runtime and build dependencies.
func (s *Store) PlanBuild(
	ctx context.Context,
	graph *PackageGraph,
	packageIDs ...PackageID,
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
		state: make(map[PackageID]visitState),
	}
	roots := make([]PackageID, 0, len(packageIDs))
	rootSet := make(map[PackageID]struct{}, len(packageIDs))
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
		Roots:    roots,
		Packages: planner.packages,
	}
	if err := validateBuildPlan(plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// FetchInputsOptions controls fetching all inputs for a build plan.
type FetchInputsOptions struct {
	// Jobs is the maximum number of source fetches performed concurrently.
	// Zero selects DefaultFetchJobs.
	Jobs int

	Progress FetchProgressFunc
}

// FetchBuildInputs fetches and verifies every source needed by the local-build
// actions in plan. Sources shared by multiple packages are fetched once.
func (s *Store) FetchBuildInputs(ctx context.Context, plan *BuildPlan) error {
	return s.FetchBuildInputsWithOptions(ctx, plan, FetchInputsOptions{})
}

// FetchBuildInputsWithOptions is FetchBuildInputs with progress reporting.
func (s *Store) FetchBuildInputsWithOptions(
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
	if err := validateBuildPlan(plan); err != nil {
		return err
	}
	if options.Jobs < 0 {
		return errors.New("pkgstore: fetch jobs must not be negative")
	}
	if options.Jobs == 0 {
		options.Jobs = DefaultFetchJobs
	}

	type fetchTask struct {
		packageID   PackageID
		packageName string
		source      Fetch
	}
	tasks := make([]fetchTask, 0)
	fetched := make(map[string]struct{})
	for _, planned := range plan.Packages {
		if planned.Disposition != BuildDispositionLocal {
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
				if _, err := s.FetchWithOptions(
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

type visitState uint8

const (
	visitUnseen visitState = iota
	visitActive
	visitDone
)

type buildPlanner struct {
	ctx      context.Context
	store    *Store
	graph    *PackageGraph
	state    map[PackageID]visitState
	packages []PlannedPackage
}

func (p *buildPlanner) visit(packageID PackageID) error {
	if err := p.ctx.Err(); err != nil {
		return err
	}
	switch p.state[packageID] {
	case visitActive:
		return fmt.Errorf("pkgstore: dependency cycle through package %s", packageID)
	case visitDone:
		return nil
	}

	definition, ok := p.graph.Nodes[packageID]
	if !ok {
		return fmt.Errorf("pkgstore: package %q is not in the graph", packageID)
	}
	if err := validatePackageDefinition(packageID, definition); err != nil {
		return err
	}

	available, err := p.store.packageAvailableForUse(
		p.ctx,
		packageID,
		definition.Name,
	)
	if err != nil {
		return fmt.Errorf("inspect package %s: %w", packageID, err)
	}
	disposition := BuildDispositionLocal
	prerequisites := append([]PackageID(nil), definition.RunDeps...)
	if available {
		disposition = BuildDispositionAvailable
	} else {
		prerequisites = append(prerequisites, definition.BuildDeps...)
	}
	prerequisites = deduplicatePackageIDs(prerequisites)

	p.state[packageID] = visitActive
	for _, dependency := range prerequisites {
		if err := p.visit(dependency); err != nil {
			return fmt.Errorf("package %s dependency %s: %w", packageID, dependency, err)
		}
	}
	p.state[packageID] = visitDone
	p.packages = append(p.packages, PlannedPackage{
		ID:            packageID,
		Definition:    clonePackageDefinition(definition),
		Prerequisites: slices.Clone(prerequisites),
		Disposition:   disposition,
	})
	return nil
}

func validateBuildPlan(plan *BuildPlan) error {
	if plan == nil {
		return errors.New("pkgstore: build plan is nil")
	}
	if len(plan.Roots) == 0 {
		return errors.New("pkgstore: build plan has no roots")
	}
	seen := make(map[PackageID]struct{}, len(plan.Packages))
	for index, planned := range plan.Packages {
		if _, ok := seen[planned.ID]; ok {
			return fmt.Errorf("pkgstore: package %s occurs more than once in build plan", planned.ID)
		}
		if err := validatePackageDefinition(planned.ID, planned.Definition); err != nil {
			return fmt.Errorf("pkgstore: build plan package %d: %w", index, err)
		}
		switch planned.Disposition {
		case BuildDispositionAvailable, BuildDispositionLocal:
		default:
			return fmt.Errorf(
				"pkgstore: package %s has invalid build disposition %q",
				planned.ID,
				planned.Disposition,
			)
		}

		expected := planned.Definition.RunDeps
		if planned.Disposition == BuildDispositionLocal {
			expected = append(
				slices.Clone(planned.Definition.RunDeps),
				planned.Definition.BuildDeps...,
			)
		}
		expected = deduplicatePackageIDs(expected)
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
	for _, root := range plan.Roots {
		if _, ok := seen[root]; !ok {
			return fmt.Errorf("pkgstore: build plan root %s is missing", root)
		}
	}
	return nil
}

func validatePackageDefinition(packageID PackageID, definition PackageDefinition) error {
	if err := validatePackageID(packageID); err != nil {
		return err
	}
	expectedID, err := packageIDForDefinition(definition)
	if err != nil {
		return fmt.Errorf("pkgstore: encode package %s: %w", packageID, err)
	}
	if packageID != expectedID {
		return fmt.Errorf(
			"pkgstore: package ID %s does not match its definition (expected %s)",
			packageID,
			expectedID,
		)
	}
	if err := validatePackageName(definition.Name); err != nil {
		return fmt.Errorf(
			"pkgstore: package %s has invalid name: %w",
			packageID,
			err,
		)
	}
	if err := validateBuildEnvironment(definition.BuildEnv); err != nil {
		return fmt.Errorf(
			"pkgstore: package %s has invalid build environment: %w",
			packageID,
			err,
		)
	}
	filenames := make(map[string]struct{}, len(definition.Fetch))
	for _, source := range definition.Fetch {
		if err := validateFetchFilename(source.Filename); err != nil {
			return fmt.Errorf("pkgstore: package %s: %w", packageID, err)
		}
		if _, ok := filenames[source.Filename]; ok {
			return fmt.Errorf(
				"pkgstore: package %s has duplicate fetch filename %q",
				packageID,
				source.Filename,
			)
		}
		filenames[source.Filename] = struct{}{}
		if _, _, err := parseFetchSHA256(source.SHA256); err != nil {
			return fmt.Errorf(
				"pkgstore: package %s fetch %q: %w",
				packageID,
				source.Filename,
				err,
			)
		}
	}
	for _, dependency := range append(
		slices.Clone(definition.RunDeps),
		definition.BuildDeps...,
	) {
		if err := validatePackageID(dependency); err != nil {
			return fmt.Errorf("pkgstore: package %s dependency: %w", packageID, err)
		}
	}
	return nil
}

func validatePackageName(name string) error {
	if name == "" {
		return nil
	}
	if len(name) > maximumPackageNameLength {
		return fmt.Errorf("must not exceed %d bytes", maximumPackageNameLength)
	}
	for index, character := range name {
		allowed := character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			index > 0 && strings.ContainsRune("._+-", character)
		if !allowed {
			return errors.New(
				"must start with an ASCII letter or digit and contain only " +
					"ASCII letters, digits, '.', '_', '+', or '-'",
			)
		}
	}
	return nil
}

func validatePackageID(packageID PackageID) error {
	value := string(packageID)
	if len(value) != packageIDLength {
		return fmt.Errorf("pkgstore: invalid package ID %q", packageID)
	}
	decoded, err := packageIDEncoding.DecodeString(value)
	if err != nil ||
		len(decoded) != sha256.Size ||
		packageIDEncoding.EncodeToString(decoded) != value {
		return fmt.Errorf("pkgstore: invalid package ID %q", packageID)
	}
	return nil
}

func validateFetchFilename(filename string) error {
	if filename == "" {
		return errors.New("fetch filename is empty")
	}
	if filename == "." ||
		filepath.Base(filename) != filename ||
		strings.ContainsAny(filename, `/\`) {
		return fmt.Errorf("fetch filename %q must be a single path component", filename)
	}
	return nil
}

func deduplicatePackageIDs(packageIDs []PackageID) []PackageID {
	result := make([]PackageID, 0, len(packageIDs))
	seen := make(map[PackageID]struct{}, len(packageIDs))
	for _, packageID := range packageIDs {
		if _, ok := seen[packageID]; ok {
			continue
		}
		seen[packageID] = struct{}{}
		result = append(result, packageID)
	}
	return result
}

func clonePackageDefinition(definition PackageDefinition) PackageDefinition {
	definition.BuildEnv = resolveBuildEnvironment(definition.BuildEnv)
	definition.RunDeps = slices.Clone(definition.RunDeps)
	definition.BuildDeps = slices.Clone(definition.BuildDeps)
	definition.Fetch = slices.Clone(definition.Fetch)
	for index := range definition.Fetch {
		definition.Fetch[index].URLs = slices.Clone(definition.Fetch[index].URLs)
	}
	return definition
}
