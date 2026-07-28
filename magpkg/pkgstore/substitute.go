package pkgstore

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

// substituteBuildClosure materializes binary-cache hits before ordinary build
// planning. A hit follows only runtime dependencies; a miss which may be built
// locally follows both runtime and build dependencies.
func (s *Store) substituteBuildClosure(
	ctx context.Context,
	graph *pkggraph.Graph,
	roots []pkggraph.ID,
	noBuild bool,
) error {
	if err := validateStore(s); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if graph == nil {
		return errors.New("pkgstore: package graph is nil")
	}
	if err := graph.Validate(); err != nil {
		return err
	}
	if s.binaryCache == nil && !noBuild {
		return nil
	}

	type visitState uint8
	const (
		visitUnseen visitState = iota
		visitActive
		visitDone
	)
	state := make(map[pkggraph.ID]visitState)
	var visit func(pkggraph.ID) error
	visit = func(packageID pkggraph.ID) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch state[packageID] {
		case visitActive:
			return fmt.Errorf(
				"pkgstore: dependency cycle through package %s",
				packageID,
			)
		case visitDone:
			return nil
		}
		definition, ok := graph.Nodes[packageID]
		if !ok {
			return fmt.Errorf(
				"pkgstore: package %q is not in the graph",
				packageID,
			)
		}

		state[packageID] = visitActive
		available, err := s.packageAvailableForUse(
			ctx,
			packageID,
			definition.Name,
		)
		if err != nil {
			return fmt.Errorf("inspect package %s: %w", packageID, err)
		}
		if !available && s.binaryCache != nil {
			available, err = s.fetchPackageFromBinaryCache(
				ctx,
				packageID,
				definition.Name,
			)
			if err != nil {
				return err
			}
		}
		if !available && noBuild {
			if s.binaryCache == nil {
				return fmt.Errorf(
					"pkgstore: package %s is missing, local builds are "+
						"disabled, and no binary cache is configured",
					packageID,
				)
			}
			return fmt.Errorf(
				"pkgstore: package %s is not available from the binary cache "+
					"and local builds are disabled",
				packageID,
			)
		}

		dependencies := slices.Clone(definition.RunDeps)
		if !available {
			dependencies = append(dependencies, definition.BuildDeps...)
		}
		for _, dependency := range deduplicateIDs(dependencies) {
			if err := visit(dependency); err != nil {
				return fmt.Errorf(
					"package %s dependency %s: %w",
					packageID,
					dependency,
					err,
				)
			}
		}
		state[packageID] = visitDone
		return nil
	}

	for _, root := range deduplicateIDs(roots) {
		if err := visit(root); err != nil {
			return err
		}
	}
	return nil
}
