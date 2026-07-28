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

	// A sealed graph's dependencies are always present and acyclic, so only
	// the requested roots need membership checks.
	seen := make(map[ID]struct{})
	order := make([]ID, 0)
	var visit func(ID)
	visit = func(packageID ID) {
		if _, ok := seen[packageID]; ok {
			return
		}
		seen[packageID] = struct{}{}
		for _, dependency := range graph.nodes[packageID].RunDeps {
			visit(dependency)
		}
		order = append(order, packageID)
	}

	for _, root := range roots {
		if _, ok := graph.nodes[root]; !ok {
			return nil, fmt.Errorf(
				"pkggraph: package %q is not in the graph",
				root,
			)
		}
		visit(root)
	}
	return order, nil
}
