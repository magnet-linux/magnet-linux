package pkggraph

import (
	"fmt"
	"reflect"
	"slices"
)

// Builder incrementally constructs a Graph. The zero value is ready to use.
//
// Definitions are added dependency-first: every run and build dependency of a
// definition must already be in the builder. Identities are computed, never
// supplied, so a sealed Graph cannot pair a definition with a forged ID.
type Builder struct {
	nodes map[ID]Definition
	roots map[string]ID
}

// AddDefinition validates and normalizes definition and returns its canonical
// identity. Every dependency must already be in the builder. Adding a
// definition that is already present returns its existing identity.
func (builder *Builder) AddDefinition(definition Definition) (ID, error) {
	normalized, err := NormalizeDefinition(definition)
	if err != nil {
		return "", err
	}
	for _, dependency := range append(
		slices.Clone(normalized.RunDeps),
		normalized.BuildDeps...,
	) {
		if _, ok := builder.nodes[dependency]; !ok {
			return "", fmt.Errorf(
				"pkggraph: dependency %s is not in the graph",
				dependency,
			)
		}
	}
	id, err := idForNormalized(normalized)
	if err != nil {
		return "", err
	}
	if previous, ok := builder.nodes[id]; ok {
		if !reflect.DeepEqual(previous, normalized) {
			return "", fmt.Errorf("pkggraph: package ID collision for %s", id)
		}
		return id, nil
	}
	if builder.nodes == nil {
		builder.nodes = make(map[ID]Definition)
	}
	builder.nodes[id] = normalized
	return id, nil
}

// SetRoot exports packageID under name. The package must already be in the
// builder.
func (builder *Builder) SetRoot(name string, packageID ID) error {
	if _, ok := builder.nodes[packageID]; !ok {
		return fmt.Errorf(
			"pkggraph: root %q package %s is not in the graph",
			name,
			packageID,
		)
	}
	if builder.roots == nil {
		builder.roots = make(map[string]ID)
	}
	builder.roots[name] = packageID
	return nil
}

// Graph seals the builder's packages and roots into a Graph and resets the
// builder.
func (builder *Builder) Graph() *Graph {
	graph := &Graph{nodes: builder.nodes, roots: builder.roots}
	builder.nodes = nil
	builder.roots = nil
	return graph
}
