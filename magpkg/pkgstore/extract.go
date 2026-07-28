package pkgstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

type packageReference struct {
	id   pkggraph.ID
	name string
}

// ExtractPackages overlays packageIDs into destination in the supplied order.
// Each artifact is touched and protected by a shared package lock while it is
// extracted.
func (s *Store) ExtractPackages(
	ctx context.Context,
	graph *pkggraph.Graph,
	packageIDs []pkggraph.ID,
	destination string,
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
	if destination == "" {
		return errors.New("pkgstore: package extraction destination is empty")
	}

	references := make([]packageReference, 0, len(packageIDs))
	for _, packageID := range packageIDs {
		definition, ok := graph.Nodes[packageID]
		if !ok {
			return fmt.Errorf(
				"pkgstore: package %q is not in the graph",
				packageID,
			)
		}
		if err := pkggraph.ValidateIdentity(
			packageID,
			definition,
		); err != nil {
			return err
		}
		references = append(references, packageReference{
			id:   packageID,
			name: definition.Name,
		})
	}
	return s.extractPackageReferences(ctx, references, destination)
}

// ExtractRuntimeClosure calculates and extracts roots' runtime closure.
func (s *Store) ExtractRuntimeClosure(
	ctx context.Context,
	graph *pkggraph.Graph,
	roots []pkggraph.ID,
	destination string,
) ([]pkggraph.ID, error) {
	closure, err := graph.RuntimeClosure(roots...)
	if err != nil {
		return nil, err
	}
	if err := s.ExtractPackages(ctx, graph, closure, destination); err != nil {
		return nil, err
	}
	return closure, nil
}

func (s *Store) extractPackageReferences(
	ctx context.Context,
	packages []packageReference,
	destination string,
) error {
	for _, pkg := range packages {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.usePackageArtifact(
			ctx,
			pkg.id,
			pkg.name,
			func(artifactPath string) error {
				return extractArchive(ctx, artifactPath, destination)
			},
		); err != nil {
			return fmt.Errorf("extract package %s: %w", pkg.id, err)
		}
	}
	return nil
}
