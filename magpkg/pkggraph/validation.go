package pkggraph

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

const maximumPackageNameLength = 128

// ValidateDefinition validates a package definition independently of a graph.
func ValidateDefinition(definition Definition) error {
	if err := ValidateName(definition.Name); err != nil {
		return fmt.Errorf("pkggraph: invalid package name: %w", err)
	}
	if err := ValidateBuildSpec(definition.Build); err != nil {
		return fmt.Errorf("pkggraph: invalid build specification: %w", err)
	}
	if err := validateBuildEnvironment(definition.BuildEnv); err != nil {
		return fmt.Errorf("pkggraph: invalid build environment: %w", err)
	}
	filenames := make(map[string]struct{}, len(definition.Fetch))
	for _, source := range definition.Fetch {
		if err := ValidateFetchFilename(source.Filename); err != nil {
			return fmt.Errorf("pkggraph: %w", err)
		}
		if _, ok := filenames[source.Filename]; ok {
			return fmt.Errorf(
				"pkggraph: duplicate fetch filename %q",
				source.Filename,
			)
		}
		filenames[source.Filename] = struct{}{}
		decoded, err := hex.DecodeString(source.SHA256)
		if err != nil || len(decoded) != sha256.Size {
			return fmt.Errorf(
				"pkggraph: fetch %q SHA-256 must be a %d-character "+
					"hexadecimal digest",
				source.Filename,
				sha256.Size*2,
			)
		}
	}
	for _, dependency := range append(
		slices.Clone(definition.RunDeps),
		definition.BuildDeps...,
	) {
		if err := ValidateID(dependency); err != nil {
			return fmt.Errorf("pkggraph: dependency: %w", err)
		}
	}
	return nil
}

// ValidateBuildSpec validates a package build specification.
func ValidateBuildSpec(build BuildSpec) error {
	switch build.Kind {
	case BuildNone, BuildUnpack:
		if build.Script != "" {
			return fmt.Errorf("build kind %q must not contain a script", build.Kind)
		}
	case BuildScript:
		if build.Script == "" {
			return errors.New("script build has an empty script")
		}
	default:
		return fmt.Errorf("unknown build kind %q", build.Kind)
	}
	return nil
}

// ValidateName checks the optional human-readable artifact prefix.
func ValidateName(name string) error {
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

// ValidateID checks the canonical package identity encoding.
func ValidateID(id ID) error {
	value := string(id)
	if len(value) != IDLength {
		return fmt.Errorf("pkggraph: invalid package ID %q", id)
	}
	decoded, err := idEncoding.DecodeString(value)
	if err != nil ||
		len(decoded) != sha256.Size ||
		idEncoding.EncodeToString(decoded) != value {
		return fmt.Errorf("pkggraph: invalid package ID %q", id)
	}
	return nil
}

// ValidateFetchFilename checks that filename is one path component.
func ValidateFetchFilename(filename string) error {
	if filename == "" {
		return errors.New("fetch filename is empty")
	}
	if filename == "." ||
		filepath.Base(filename) != filename ||
		strings.ContainsAny(filename, `/\`) {
		return fmt.Errorf(
			"fetch filename %q must be a single path component",
			filename,
		)
	}
	return nil
}

// CloneDefinition returns a deep copy of definition.
func CloneDefinition(definition Definition) Definition {
	definition.BuildEnv = cloneStringMap(definition.BuildEnv)
	definition.RunDeps = slices.Clone(definition.RunDeps)
	definition.BuildDeps = slices.Clone(definition.BuildDeps)
	definition.Fetch = slices.Clone(definition.Fetch)
	for index := range definition.Fetch {
		definition.Fetch[index].URLs = slices.Clone(definition.Fetch[index].URLs)
	}
	return definition
}

// NormalizeDefinition validates definition and returns its canonical graph
// representation. Environment overrides equal to defaults are removed and
// absent dependency, fetch, and URL lists become empty lists.
func NormalizeDefinition(definition Definition) (Definition, error) {
	if err := ValidateDefinition(definition); err != nil {
		return Definition{}, err
	}
	normalized := CloneDefinition(definition)
	for name, value := range normalized.BuildEnv {
		if defaultValue, ok := defaultBuildEnvironment[name]; ok &&
			value == defaultValue {
			delete(normalized.BuildEnv, name)
		}
	}
	if len(normalized.BuildEnv) == 0 {
		normalized.BuildEnv = nil
	}
	if normalized.RunDeps == nil {
		normalized.RunDeps = []ID{}
	}
	if normalized.BuildDeps == nil {
		normalized.BuildDeps = []ID{}
	}
	if normalized.Fetch == nil {
		normalized.Fetch = []Fetch{}
	}
	for index := range normalized.Fetch {
		if normalized.Fetch[index].URLs == nil {
			normalized.Fetch[index].URLs = []string{}
		}
	}
	return normalized, nil
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
