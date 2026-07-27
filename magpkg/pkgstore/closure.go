package pkgstore

import (
	"errors"
	"fmt"
)

// RuntimeClosure returns roots and their transitive runtime dependencies in
// deterministic dependency-first installation order. Roots and dependencies
// retain declaration order and duplicate packages appear only once.
func (graph *PackageGraph) RuntimeClosure(
	roots ...PackageID,
) ([]PackageID, error) {
	if graph == nil {
		return nil, errors.New("pkgstore: package graph is nil")
	}
	if len(roots) == 0 {
		return nil, errors.New("pkgstore: runtime closure has no roots")
	}

	state := make(map[PackageID]visitState)
	order := make([]PackageID, 0)
	var visit func(PackageID) error
	visit = func(packageID PackageID) error {
		switch state[packageID] {
		case visitActive:
			return fmt.Errorf(
				"pkgstore: runtime dependency cycle through package %s",
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
		if err := validatePackageDefinition(packageID, definition); err != nil {
			return err
		}
		state[packageID] = visitActive
		for _, dependency := range definition.RunDeps {
			if err := visit(dependency); err != nil {
				return fmt.Errorf(
					"package %s runtime dependency %s: %w",
					packageID,
					dependency,
					err,
				)
			}
		}
		state[packageID] = visitDone
		order = append(order, packageID)
		return nil
	}

	for _, root := range roots {
		if err := visit(root); err != nil {
			return nil, err
		}
	}
	return order, nil
}
