package pkgstore

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	jsonnet "github.com/andrewchambers/go-jsonnet"
)

const (
	packageIDAlphabet = "abcdefghijklmnopqrstuvwxyz234567"
	packageIDLength   = 52
)

var packageIDEncoding = base32.NewEncoding(packageIDAlphabet).
	WithPadding(base32.NoPadding)

// PackageID is the lowercase, unpadded Base32 encoding of the SHA-256
// identity of a versioned, canonical package definition.
type PackageID string

// PackageDefinition describes a package in a materialized package graph.
type PackageDefinition struct {
	Name  string `json:"name"`
	Build string `json:"build"`
	// BuildEnv overrides the deterministic default environment used by build
	// scripts. Jsonnet materialization stores the fully resolved environment.
	BuildEnv  map[string]string `json:"buildEnv"`
	RunDeps   []PackageID       `json:"runDeps"`
	BuildDeps []PackageID       `json:"buildDeps"`
	Fetch     []Fetch           `json:"fetch"`
}

// Fetch describes a file required while building a package.
type Fetch struct {
	Filename string   `json:"filename"`
	SHA256   string   `json:"sha256"`
	URLs     []string `json:"urls"`
}

// PackageGraph is a directed graph of package definitions. Roots maps the
// fields exported by the Jsonnet program to their package IDs.
type PackageGraph struct {
	Nodes map[PackageID]PackageDefinition `json:"nodes"`
	Roots map[string]PackageID            `json:"roots"`
}

// JsonnetOptions controls evaluation of a Jsonnet package graph.
type JsonnetOptions struct {
	// ExtVars binds string values available through std.extVar.
	ExtVars map[string]string
}

// PackageGraphFromJsonnet evaluates jsonnetFile and normalizes its package
// definitions into a graph without manifesting the Jsonnet value to JSON.
func PackageGraphFromJsonnet(
	ctx context.Context,
	jsonnetFile string,
) (*PackageGraph, error) {
	return PackageGraphFromJsonnetWithOptions(
		ctx,
		jsonnetFile,
		JsonnetOptions{},
	)
}

// PackageGraphFromJsonnetWithOptions evaluates jsonnetFile with options and
// normalizes its package definitions into a graph without manifesting the
// Jsonnet value to JSON.
func PackageGraphFromJsonnetWithOptions(
	ctx context.Context,
	jsonnetFile string,
	options JsonnetOptions,
) (*PackageGraph, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if jsonnetFile == "" {
		return nil, errors.New("pkgstore: Jsonnet filename is empty")
	}

	vm := jsonnet.MakeVM()
	for name, value := range options.ExtVars {
		if name == "" {
			return nil, errors.New("pkgstore: Jsonnet external variable name is empty")
		}
		vm.ExtVar(name, value)
	}
	value, err := vm.EvaluateFileValue(jsonnetFile)
	if err != nil {
		return nil, fmt.Errorf("evaluate Jsonnet file %q: %w", jsonnetFile, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	root, ok := value.(jsonnet.ObjectValue)
	if !ok {
		return nil, fmt.Errorf(
			"Jsonnet file %q: expected top-level object, got %s",
			jsonnetFile,
			jsonnetValueType(value),
		)
	}

	materializer := newGraphMaterializer(ctx)

	rootNames, err := root.Fields()
	if err != nil {
		return nil, fmt.Errorf("read roots from %q: %w", jsonnetFile, err)
	}
	roots := make(map[string]PackageID, len(rootNames))
	for _, rootName := range rootNames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		value, found, err := root.Field(rootName)
		if err != nil {
			return nil, fmt.Errorf("root %q: %w", rootName, err)
		}
		if !found {
			return nil, fmt.Errorf("root %q disappeared during evaluation", rootName)
		}

		id, err := materializer.packageID(
			value,
			fmt.Sprintf("root[%q]", rootName),
		)
		if err != nil {
			return nil, err
		}
		roots[rootName] = id
	}

	return &PackageGraph{
		Nodes: materializer.nodes,
		Roots: roots,
	}, nil
}

// PackageGraphFromValues materializes package object values and their
// transitive dependencies into one graph. Returned IDs correspond to values
// in input order.
func PackageGraphFromValues(
	ctx context.Context,
	values []jsonnet.Value,
) (*PackageGraph, []PackageID, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if len(values) == 0 {
		return nil, nil, errors.New("pkgstore: package value list is empty")
	}

	materializer := newGraphMaterializer(ctx)
	packageIDs := make([]PackageID, len(values))
	for index, value := range values {
		id, err := materializer.packageID(
			value,
			fmt.Sprintf("packages[%d]", index),
		)
		if err != nil {
			return nil, nil, err
		}
		packageIDs[index] = id
	}
	return &PackageGraph{
		Nodes: materializer.nodes,
		Roots: make(map[string]PackageID),
	}, packageIDs, nil
}

func newGraphMaterializer(ctx context.Context) graphMaterializer {
	return graphMaterializer{
		ctx:           ctx,
		nodes:         make(map[PackageID]PackageDefinition),
		canonicalByID: make(map[PackageID]string),
		materialized:  make(map[jsonnet.ObjectToken]materializedObject),
		packageFieldSet: stringSet(
			"name",
			"build",
			"buildEnv",
			"runDeps",
			"buildDeps",
			"fetch",
		),
		fetchFieldSet: stringSet("filename", "sha256", "urls"),
	}
}

type graphMaterializer struct {
	ctx             context.Context
	nodes           map[PackageID]PackageDefinition
	canonicalByID   map[PackageID]string
	materialized    map[jsonnet.ObjectToken]materializedObject
	packageFieldSet map[string]struct{}
	fetchFieldSet   map[string]struct{}
}

type materializedObject struct {
	path     string
	id       PackageID
	visiting bool
}

func (m *graphMaterializer) packageID(
	value jsonnet.Value,
	path string,
) (PackageID, error) {
	if err := m.ctx.Err(); err != nil {
		return "", err
	}

	object, ok := value.(jsonnet.ObjectValue)
	if !ok {
		return "", fmt.Errorf(
			"%s: expected package object, got %s",
			path,
			jsonnetValueType(value),
		)
	}

	token := object.Token()
	if materialized, ok := m.materialized[token]; ok {
		if materialized.visiting {
			return "", fmt.Errorf(
				"%s: package dependency cycle through %s",
				path,
				materialized.path,
			)
		}
		return materialized.id, nil
	}
	m.materialized[token] = materializedObject{
		path:     path,
		visiting: true,
	}

	definition, err := m.packageDefinition(object, path)
	if err != nil {
		return "", err
	}

	canonical, err := json.Marshal(definition)
	if err != nil {
		return "", fmt.Errorf("%s: encode package definition: %w", path, err)
	}
	id := packageIDFromCanonical(canonical)

	if previous, ok := m.canonicalByID[id]; ok && previous != string(canonical) {
		return "", fmt.Errorf("%s: package ID collision for %s", path, id)
	}
	m.canonicalByID[id] = string(canonical)
	m.nodes[id] = definition
	m.materialized[token] = materializedObject{
		path: path,
		id:   id,
	}
	return id, nil
}

func packageIDForDefinition(definition PackageDefinition) (PackageID, error) {
	definition.BuildEnv = resolveBuildEnvironment(definition.BuildEnv)
	canonical, err := json.Marshal(definition)
	if err != nil {
		return "", err
	}
	return packageIDFromCanonical(canonical), nil
}

func packageIDFromCanonical(canonical []byte) PackageID {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("magpkg-package-v1\x00"))
	_, _ = hasher.Write(canonical)
	return PackageID(packageIDEncoding.EncodeToString(hasher.Sum(nil)))
}

func (m *graphMaterializer) packageDefinition(
	object jsonnet.ObjectValue,
	path string,
) (PackageDefinition, error) {
	if err := validateObjectFields(
		object,
		path,
		m.packageFieldSet,
		"build",
		"runDeps",
		"buildDeps",
		"fetch",
	); err != nil {
		return PackageDefinition{}, err
	}

	name, err := optionalStringObjectField(object, "name", path)
	if err != nil {
		return PackageDefinition{}, err
	}
	if err := validatePackageName(name); err != nil {
		return PackageDefinition{}, fmt.Errorf("%s.name: %w", path, err)
	}

	build, err := stringObjectField(object, "build", path)
	if err != nil {
		return PackageDefinition{}, err
	}

	buildEnv, err := buildEnvironmentObjectField(object, "buildEnv", path)
	if err != nil {
		return PackageDefinition{}, err
	}

	runDepsValue, err := objectField(object, "runDeps", path)
	if err != nil {
		return PackageDefinition{}, err
	}
	runDeps, err := m.dependencies(runDepsValue, path+".runDeps")
	if err != nil {
		return PackageDefinition{}, err
	}

	buildDepsValue, err := objectField(object, "buildDeps", path)
	if err != nil {
		return PackageDefinition{}, err
	}
	buildDeps, err := m.dependencies(buildDepsValue, path+".buildDeps")
	if err != nil {
		return PackageDefinition{}, err
	}

	fetchValue, err := objectField(object, "fetch", path)
	if err != nil {
		return PackageDefinition{}, err
	}
	fetch, err := m.fetches(fetchValue, path+".fetch")
	if err != nil {
		return PackageDefinition{}, err
	}

	return PackageDefinition{
		Name:      name,
		Build:     build,
		BuildEnv:  buildEnv,
		RunDeps:   runDeps,
		BuildDeps: buildDeps,
		Fetch:     fetch,
	}, nil
}

func buildEnvironmentObjectField(
	object jsonnet.ObjectValue,
	field string,
	path string,
) (map[string]string, error) {
	value, found, err := object.Field(field)
	if err != nil {
		return nil, fmt.Errorf("%s.%s: %w", path, field, err)
	}
	if !found {
		return resolveBuildEnvironment(nil), nil
	}
	environmentObject, ok := value.(jsonnet.ObjectValue)
	if !ok {
		return nil, fmt.Errorf(
			"%s.%s: expected object, got %s",
			path,
			field,
			jsonnetValueType(value),
		)
	}

	environment := resolveBuildEnvironment(nil)
	names, err := environmentObject.Fields()
	if err != nil {
		return nil, fmt.Errorf("%s.%s: %w", path, field, err)
	}
	for _, name := range names {
		value, found, err := environmentObject.Field(name)
		if err != nil {
			return nil, fmt.Errorf("%s.%s[%q]: %w", path, field, name, err)
		}
		if !found {
			return nil, fmt.Errorf("%s.%s[%q]: field disappeared", path, field, name)
		}
		stringValue, ok := value.(jsonnet.StringValue)
		if !ok {
			return nil, fmt.Errorf(
				"%s.%s[%q]: expected string, got %s",
				path,
				field,
				name,
				jsonnetValueType(value),
			)
		}
		environment[name] = string(stringValue)
	}
	if err := validateBuildEnvironment(environment); err != nil {
		return nil, fmt.Errorf("%s.%s: %w", path, field, err)
	}
	return environment, nil
}

func (m *graphMaterializer) dependencies(
	value jsonnet.Value,
	path string,
) ([]PackageID, error) {
	array, ok := value.(jsonnet.ArrayValue)
	if !ok {
		return nil, fmt.Errorf(
			"%s: expected array, got %s",
			path,
			jsonnetValueType(value),
		)
	}

	dependencies := make([]PackageID, array.Len())
	for index := range dependencies {
		if err := m.ctx.Err(); err != nil {
			return nil, err
		}

		value, err := array.Index(index)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", path, index, err)
		}
		dependencies[index], err = m.packageID(
			value,
			fmt.Sprintf("%s[%d]", path, index),
		)
		if err != nil {
			return nil, err
		}
	}
	return dependencies, nil
}

func (m *graphMaterializer) fetches(
	value jsonnet.Value,
	path string,
) ([]Fetch, error) {
	array, ok := value.(jsonnet.ArrayValue)
	if !ok {
		return nil, fmt.Errorf(
			"%s: expected array, got %s",
			path,
			jsonnetValueType(value),
		)
	}

	fetches := make([]Fetch, array.Len())
	for index := range fetches {
		if err := m.ctx.Err(); err != nil {
			return nil, err
		}

		value, err := array.Index(index)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", path, index, err)
		}
		object, ok := value.(jsonnet.ObjectValue)
		if !ok {
			return nil, fmt.Errorf(
				"%s[%d]: expected fetch object, got %s",
				path,
				index,
				jsonnetValueType(value),
			)
		}

		fetches[index], err = m.fetch(object, fmt.Sprintf("%s[%d]", path, index))
		if err != nil {
			return nil, err
		}
	}
	return fetches, nil
}

func (m *graphMaterializer) fetch(
	object jsonnet.ObjectValue,
	path string,
) (Fetch, error) {
	if err := validateObjectFields(
		object,
		path,
		m.fetchFieldSet,
		"filename",
		"sha256",
		"urls",
	); err != nil {
		return Fetch{}, err
	}

	filename, err := stringObjectField(object, "filename", path)
	if err != nil {
		return Fetch{}, err
	}
	if filename == "" {
		return Fetch{}, fmt.Errorf("%s.filename: must not be empty", path)
	}

	sha256Sum, err := stringObjectField(object, "sha256", path)
	if err != nil {
		return Fetch{}, err
	}
	decodedSHA256, err := hex.DecodeString(sha256Sum)
	if err != nil || len(decodedSHA256) != sha256.Size {
		return Fetch{}, fmt.Errorf(
			"%s.sha256: expected a %d-character hexadecimal SHA-256 digest",
			path,
			sha256.Size*2,
		)
	}

	urlsValue, err := objectField(object, "urls", path)
	if err != nil {
		return Fetch{}, err
	}
	urls, err := m.stringArray(urlsValue, path+".urls")
	if err != nil {
		return Fetch{}, err
	}

	return Fetch{
		Filename: filename,
		SHA256:   sha256Sum,
		URLs:     urls,
	}, nil
}

func validateObjectFields(
	object jsonnet.ObjectValue,
	path string,
	allowed map[string]struct{},
	required ...string,
) error {
	fields, err := object.Fields()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	present := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if _, ok := allowed[field]; !ok {
			return fmt.Errorf("%s: unknown field %q", path, field)
		}
		present[field] = struct{}{}
	}
	for _, field := range required {
		if _, ok := present[field]; !ok {
			return fmt.Errorf("%s: missing field %q", path, field)
		}
	}
	return nil
}

func objectField(
	object jsonnet.ObjectValue,
	field string,
	path string,
) (jsonnet.Value, error) {
	value, found, err := object.Field(field)
	if err != nil {
		return nil, fmt.Errorf("%s.%s: %w", path, field, err)
	}
	if !found {
		return nil, fmt.Errorf("%s: missing field %q", path, field)
	}
	return value, nil
}

func stringObjectField(
	object jsonnet.ObjectValue,
	field string,
	path string,
) (string, error) {
	value, err := objectField(object, field, path)
	if err != nil {
		return "", err
	}
	stringValue, ok := value.(jsonnet.StringValue)
	if !ok {
		return "", fmt.Errorf(
			"%s.%s: expected string, got %s",
			path,
			field,
			jsonnetValueType(value),
		)
	}
	return string(stringValue), nil
}

func optionalStringObjectField(
	object jsonnet.ObjectValue,
	field string,
	path string,
) (string, error) {
	value, found, err := object.Field(field)
	if err != nil {
		return "", fmt.Errorf("%s.%s: %w", path, field, err)
	}
	if !found {
		return "", nil
	}
	stringValue, ok := value.(jsonnet.StringValue)
	if !ok {
		return "", fmt.Errorf(
			"%s.%s: expected string, got %s",
			path,
			field,
			jsonnetValueType(value),
		)
	}
	return string(stringValue), nil
}

func (m *graphMaterializer) stringArray(
	value jsonnet.Value,
	path string,
) ([]string, error) {
	array, ok := value.(jsonnet.ArrayValue)
	if !ok {
		return nil, fmt.Errorf(
			"%s: expected array, got %s",
			path,
			jsonnetValueType(value),
		)
	}

	strings := make([]string, array.Len())
	for index := range strings {
		if err := m.ctx.Err(); err != nil {
			return nil, err
		}

		value, err := array.Index(index)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", path, index, err)
		}
		stringValue, ok := value.(jsonnet.StringValue)
		if !ok {
			return nil, fmt.Errorf(
				"%s[%d]: expected string, got %s",
				path,
				index,
				jsonnetValueType(value),
			)
		}
		strings[index] = string(stringValue)
	}
	return strings, nil
}

func jsonnetValueType(value jsonnet.Value) string {
	switch value.(type) {
	case jsonnet.NullValue:
		return "null"
	case jsonnet.BoolValue:
		return "boolean"
	case jsonnet.NumberValue:
		return "number"
	case jsonnet.StringValue:
		return "string"
	case jsonnet.ArrayValue:
		return "array"
	case jsonnet.ObjectValue:
		return "object"
	case jsonnet.FunctionValue:
		return "function"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func stringSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
