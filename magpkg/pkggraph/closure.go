package pkggraph

import (
	"errors"
	"fmt"
)

// RuntimeClosure returns roots and their transitive runtime dependencies in
// deterministic dependency-first installation order. Roots and dependencies
// retain declaration order and duplicate packages appear only once.
func (graph *Graph) RuntimeClosure(
	roots ...ID,
) ([]ID, error) {
	if graph == nil {
		return nil, errors.New("pkggraph: package graph is nil")
	}
	if len(roots) == 0 {
		return nil, errors.New("pkggraph: runtime closure has no roots")
	}

	type visitState uint8
	const (
		visitUnseen visitState = iota
		visitActive
		visitDone
	)
	state := make(map[ID]visitState)
	order := make([]ID, 0)
	var visit func(ID) error
	visit = func(packageID ID) error {
		switch state[packageID] {
		case visitActive:
			return fmt.Errorf(
				"pkggraph: runtime dependency cycle through package %s",
				packageID,
			)
		case visitDone:
			return nil
		}
		definition, ok := graph.Nodes[packageID]
		if !ok {
			return fmt.Errorf(
				"pkggraph: package %q is not in the graph",
				packageID,
			)
		}
		if err := ValidateIdentity(packageID, definition); err != nil {
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
