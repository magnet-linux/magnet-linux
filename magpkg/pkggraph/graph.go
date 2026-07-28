package pkggraph

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"slices"
)

const (
	idAlphabet = "abcdefghijklmnopqrstuvwxyz234567"

	// IDLength is the encoded length of a package identity.
	IDLength = 52
)

var idEncoding = base32.NewEncoding(idAlphabet).
	WithPadding(base32.NoPadding)

// ID is the lowercase, unpadded Base32 encoding of the SHA-256 identity of a
// versioned, canonical package definition.
type ID string

// BuildKind selects how a package output is produced.
type BuildKind string

const (
	// BuildNone produces an empty package without running a build.
	BuildNone BuildKind = "none"
	// BuildScript runs BuildSpec.Script in the package build sandbox.
	BuildScript BuildKind = "script"
	// BuildUnpack extracts each fetched archive into the package output.
	BuildUnpack BuildKind = "unpack"
)

// BuildSpec describes how a package output is produced.
type BuildSpec struct {
	Kind   BuildKind `json:"kind"`
	Script string    `json:"script"`
}

// Definition describes a package in a materialized package graph. BuildEnv
// contains overrides to the deterministic default build environment.
type Definition struct {
	Name      string            `json:"name"`
	Build     BuildSpec         `json:"build"`
	BuildEnv  map[string]string `json:"buildEnv"`
	RunDeps   []ID              `json:"runDeps"`
	BuildDeps []ID              `json:"buildDeps"`
	Fetch     []Fetch           `json:"fetch"`
}

// Fetch describes a file required while building a package.
type Fetch struct {
	Filename string   `json:"filename"`
	SHA256   string   `json:"sha256"`
	URLs     []string `json:"urls"`
}

// Graph is a sealed directed graph of package definitions produced by a
// Builder.
//
// Every reachable Graph is valid by construction: definitions are normalized,
// keyed by their content-addressed identity, and never reference a dependency
// outside the graph. Dependency cycles are unrepresentable because a cycle
// would require a package identity to be an input to its own hash. The zero
// Graph is empty.
type Graph struct {
	nodes map[ID]Definition
	roots map[string]ID
}

// Definition returns packageID's definition. The definition's maps and
// slices are shared with the graph and must not be modified; use
// CloneDefinition before mutating.
func (graph *Graph) Definition(packageID ID) (Definition, bool) {
	if graph == nil {
		return Definition{}, false
	}
	definition, ok := graph.nodes[packageID]
	return definition, ok
}

// HasNode reports whether packageID is in the graph.
func (graph *Graph) HasNode(packageID ID) bool {
	if graph == nil {
		return false
	}
	_, ok := graph.nodes[packageID]
	return ok
}

// NodeIDs returns the sorted identity of every definition in the graph.
func (graph *Graph) NodeIDs() []ID {
	if graph == nil {
		return nil
	}
	ids := make([]ID, 0, len(graph.nodes))
	for id := range graph.nodes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Root returns the package ID exported under name.
func (graph *Graph) Root(name string) (ID, bool) {
	if graph == nil {
		return "", false
	}
	id, ok := graph.roots[name]
	return id, ok
}

// RootNames returns the sorted exported root names.
func (graph *Graph) RootNames() []string {
	if graph == nil {
		return nil
	}
	names := make([]string, 0, len(graph.roots))
	for name := range graph.roots {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// IDForDefinition validates definition and returns its canonical identity.
func IDForDefinition(definition Definition) (ID, error) {
	normalized, err := NormalizeDefinition(definition)
	if err != nil {
		return "", err
	}
	return idForNormalized(normalized)
}

func idForNormalized(normalized Definition) (ID, error) {
	canonical := normalized
	canonical.BuildEnv = ResolveBuildEnvironment(canonical.BuildEnv)
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	return idFromCanonical(encoded), nil
}

func idFromCanonical(encoded []byte) ID {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("magpkg-package-v1\x00"))
	_, _ = hasher.Write(encoded)
	return ID(idEncoding.EncodeToString(hasher.Sum(nil)))
}
