package pkgstore

import (
	"context"
	"errors"
	"fmt"
)

// Package describes a package produced in the store.
type Package struct {
	StorePath string
}

// Build builds packageID and its dependencies from graph and returns the
// resulting package.
func (s *Store) Build(
	ctx context.Context,
	graph *PackageGraph,
	packageID PackageID,
) (*Package, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.path == "" {
		return nil, errors.New("pkgstore: store is not initialized")
	}
	if graph == nil {
		return nil, errors.New("pkgstore: package graph is nil")
	}
	if packageID == "" {
		return nil, errors.New("pkgstore: package ID is empty")
	}
	if _, ok := graph.Nodes[packageID]; !ok {
		return nil, fmt.Errorf("pkgstore: package %q is not in the graph", packageID)
	}

	return nil, fmt.Errorf("build package %q: %w", packageID, ErrNotImplemented)
}
