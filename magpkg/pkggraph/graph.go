package pkggraph

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
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

// Graph is a directed graph of package definitions. Roots maps exported names
// to package IDs.
type Graph struct {
	Nodes map[ID]Definition `json:"nodes"`
	Roots map[string]ID     `json:"roots"`
}

// IDForDefinition validates definition and returns its canonical identity.
func IDForDefinition(definition Definition) (ID, error) {
	normalized, err := NormalizeDefinition(definition)
	if err != nil {
		return "", err
	}
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
	_, _ = hasher.Write([]byte("magpkg-package-v2\x00"))
	_, _ = hasher.Write(encoded)
	return ID(idEncoding.EncodeToString(hasher.Sum(nil)))
}
