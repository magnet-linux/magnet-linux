package jsonnet

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"

	gojsonnet "github.com/andrewchambers/go-jsonnet"
	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

// Options controls evaluation of a Jsonnet package graph.
type Options struct {
	// ExtVars binds string values available through std.extVar.
	ExtVars map[string]string
}

// EvaluateFile evaluates filename and materializes its package graph without
// manifesting the Jsonnet value to JSON.
func EvaluateFile(
	ctx context.Context,
	filename string,
	options Options,
) (*pkggraph.Graph, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if filename == "" {
		return nil, errors.New("pkggraph: Jsonnet filename is empty")
	}

	vm := gojsonnet.MakeVM()
	for name, value := range options.ExtVars {
		if name == "" {
			return nil, errors.New("pkggraph: Jsonnet external variable name is empty")
		}
		vm.ExtVar(name, value)
	}
	value, err := vm.EvaluateFileValue(filename)
	if err != nil {
		return nil, fmt.Errorf("evaluate Jsonnet file %q: %w", filename, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	root, ok := value.(gojsonnet.ObjectValue)
	if !ok {
		return nil, fmt.Errorf(
			"Jsonnet file %q: expected top-level object, got %s",
			filename,
			jsonnetValueType(value),
		)
	}

	materializer := newGraphMaterializer(ctx)

	rootNames, err := root.Fields()
	if err != nil {
		return nil, fmt.Errorf("read roots from %q: %w", filename, err)
	}
	roots := make(map[string]pkggraph.ID, len(rootNames))
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

	graph := &pkggraph.Graph{
		Nodes: materializer.nodes,
		Roots: roots,
	}
	if err := graph.Validate(); err != nil {
		return nil, err
	}
	return graph, nil
}

// MaterializeValues materializes package object values and their transitive
// dependencies. Returned IDs correspond to values in input order.
func MaterializeValues(
	ctx context.Context,
	values []gojsonnet.Value,
) (*pkggraph.Graph, []pkggraph.ID, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if len(values) == 0 {
		return nil, nil, errors.New("pkggraph: package value list is empty")
	}

	materializer := newGraphMaterializer(ctx)
	packageIDs := make([]pkggraph.ID, len(values))
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
	graph := &pkggraph.Graph{
		Nodes: materializer.nodes,
		Roots: make(map[string]pkggraph.ID),
	}
	if err := graph.Validate(); err != nil {
		return nil, nil, err
	}
	return graph, packageIDs, nil
}

func newGraphMaterializer(ctx context.Context) graphMaterializer {
	return graphMaterializer{
		ctx:          ctx,
		nodes:        make(map[pkggraph.ID]pkggraph.Definition),
		materialized: make(map[gojsonnet.ObjectToken]materializedObject),
		packageFieldSet: stringSet(
			"name",
			"build",
			"buildEnv",
			"runDeps",
			"buildDeps",
			"fetch",
		),
		buildFieldSet: stringSet("kind", "script"),
		fetchFieldSet: stringSet("filename", "sha256", "urls"),
	}
}

type graphMaterializer struct {
	ctx             context.Context
	nodes           map[pkggraph.ID]pkggraph.Definition
	materialized    map[gojsonnet.ObjectToken]materializedObject
	packageFieldSet map[string]struct{}
	buildFieldSet   map[string]struct{}
	fetchFieldSet   map[string]struct{}
}

type materializedObject struct {
	path     string
	id       pkggraph.ID
	visiting bool
}

func (m *graphMaterializer) packageID(
	value gojsonnet.Value,
	path string,
) (pkggraph.ID, error) {
	if err := m.ctx.Err(); err != nil {
		return "", err
	}

	object, ok := value.(gojsonnet.ObjectValue)
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
	definition, err = pkggraph.NormalizeDefinition(definition)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}

	id, err := pkggraph.IDForDefinition(definition)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	if previous, ok := m.nodes[id]; ok && !reflect.DeepEqual(previous, definition) {
		return "", fmt.Errorf("%s: package ID collision for %s", path, id)
	}
	m.nodes[id] = definition
	m.materialized[token] = materializedObject{
		path: path,
		id:   id,
	}
	return id, nil
}

func (m *graphMaterializer) packageDefinition(
	object gojsonnet.ObjectValue,
	path string,
) (pkggraph.Definition, error) {
	if err := validateObjectFields(
		object,
		path,
		m.packageFieldSet,
		"build",
		"runDeps",
		"buildDeps",
		"fetch",
	); err != nil {
		return pkggraph.Definition{}, err
	}

	name, err := optionalStringObjectField(object, "name", path)
	if err != nil {
		return pkggraph.Definition{}, err
	}
	if err := pkggraph.ValidateName(name); err != nil {
		return pkggraph.Definition{}, fmt.Errorf("%s.name: %w", path, err)
	}

	buildValue, err := objectField(object, "build", path)
	if err != nil {
		return pkggraph.Definition{}, err
	}
	build, err := m.buildSpec(buildValue, path+".build")
	if err != nil {
		return pkggraph.Definition{}, err
	}

	buildEnv, err := buildEnvironmentObjectField(object, "buildEnv", path)
	if err != nil {
		return pkggraph.Definition{}, err
	}

	runDepsValue, err := objectField(object, "runDeps", path)
	if err != nil {
		return pkggraph.Definition{}, err
	}
	runDeps, err := m.dependencies(runDepsValue, path+".runDeps")
	if err != nil {
		return pkggraph.Definition{}, err
	}

	buildDepsValue, err := objectField(object, "buildDeps", path)
	if err != nil {
		return pkggraph.Definition{}, err
	}
	buildDeps, err := m.dependencies(buildDepsValue, path+".buildDeps")
	if err != nil {
		return pkggraph.Definition{}, err
	}

	fetchValue, err := objectField(object, "fetch", path)
	if err != nil {
		return pkggraph.Definition{}, err
	}
	fetch, err := m.fetches(fetchValue, path+".fetch")
	if err != nil {
		return pkggraph.Definition{}, err
	}

	return pkggraph.Definition{
		Name:      name,
		Build:     build,
		BuildEnv:  buildEnv,
		RunDeps:   runDeps,
		BuildDeps: buildDeps,
		Fetch:     fetch,
	}, nil
}

func (m *graphMaterializer) buildSpec(
	value gojsonnet.Value,
	path string,
) (pkggraph.BuildSpec, error) {
	object, ok := value.(gojsonnet.ObjectValue)
	if !ok {
		return pkggraph.BuildSpec{}, fmt.Errorf(
			"%s: expected object, got %s",
			path,
			jsonnetValueType(value),
		)
	}
	if err := validateObjectFields(
		object,
		path,
		m.buildFieldSet,
		"kind",
	); err != nil {
		return pkggraph.BuildSpec{}, err
	}
	kind, err := stringObjectField(object, "kind", path)
	if err != nil {
		return pkggraph.BuildSpec{}, err
	}
	script, err := optionalStringObjectField(object, "script", path)
	if err != nil {
		return pkggraph.BuildSpec{}, err
	}
	build := pkggraph.BuildSpec{
		Kind:   pkggraph.BuildKind(kind),
		Script: script,
	}
	if err := pkggraph.ValidateBuildSpec(build); err != nil {
		return pkggraph.BuildSpec{}, fmt.Errorf("%s: %w", path, err)
	}
	return build, nil
}

func buildEnvironmentObjectField(
	object gojsonnet.ObjectValue,
	field string,
	path string,
) (map[string]string, error) {
	value, found, err := object.Field(field)
	if err != nil {
		return nil, fmt.Errorf("%s.%s: %w", path, field, err)
	}
	if !found {
		return nil, nil
	}
	environmentObject, ok := value.(gojsonnet.ObjectValue)
	if !ok {
		return nil, fmt.Errorf(
			"%s.%s: expected object, got %s",
			path,
			field,
			jsonnetValueType(value),
		)
	}

	environment := make(map[string]string)
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
		stringValue, ok := value.(gojsonnet.StringValue)
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
	return environment, nil
}

func (m *graphMaterializer) dependencies(
	value gojsonnet.Value,
	path string,
) ([]pkggraph.ID, error) {
	array, ok := value.(gojsonnet.ArrayValue)
	if !ok {
		return nil, fmt.Errorf(
			"%s: expected array, got %s",
			path,
			jsonnetValueType(value),
		)
	}

	dependencies := make([]pkggraph.ID, array.Len())
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
	value gojsonnet.Value,
	path string,
) ([]pkggraph.Fetch, error) {
	array, ok := value.(gojsonnet.ArrayValue)
	if !ok {
		return nil, fmt.Errorf(
			"%s: expected array, got %s",
			path,
			jsonnetValueType(value),
		)
	}

	fetches := make([]pkggraph.Fetch, array.Len())
	for index := range fetches {
		if err := m.ctx.Err(); err != nil {
			return nil, err
		}

		value, err := array.Index(index)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", path, index, err)
		}
		object, ok := value.(gojsonnet.ObjectValue)
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
	object gojsonnet.ObjectValue,
	path string,
) (pkggraph.Fetch, error) {
	if err := validateObjectFields(
		object,
		path,
		m.fetchFieldSet,
		"filename",
		"sha256",
		"urls",
	); err != nil {
		return pkggraph.Fetch{}, err
	}

	filename, err := stringObjectField(object, "filename", path)
	if err != nil {
		return pkggraph.Fetch{}, err
	}
	if filename == "" {
		return pkggraph.Fetch{}, fmt.Errorf("%s.filename: must not be empty", path)
	}

	sha256Sum, err := stringObjectField(object, "sha256", path)
	if err != nil {
		return pkggraph.Fetch{}, err
	}
	decodedSHA256, err := hex.DecodeString(sha256Sum)
	if err != nil || len(decodedSHA256) != sha256.Size {
		return pkggraph.Fetch{}, fmt.Errorf(
			"%s.sha256: expected a %d-character hexadecimal SHA-256 digest",
			path,
			sha256.Size*2,
		)
	}

	urlsValue, err := objectField(object, "urls", path)
	if err != nil {
		return pkggraph.Fetch{}, err
	}
	urls, err := m.stringArray(urlsValue, path+".urls")
	if err != nil {
		return pkggraph.Fetch{}, err
	}

	return pkggraph.Fetch{
		Filename: filename,
		SHA256:   sha256Sum,
		URLs:     urls,
	}, nil
}

func validateObjectFields(
	object gojsonnet.ObjectValue,
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
	object gojsonnet.ObjectValue,
	field string,
	path string,
) (gojsonnet.Value, error) {
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
	object gojsonnet.ObjectValue,
	field string,
	path string,
) (string, error) {
	value, err := objectField(object, field, path)
	if err != nil {
		return "", err
	}
	stringValue, ok := value.(gojsonnet.StringValue)
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
	object gojsonnet.ObjectValue,
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
	stringValue, ok := value.(gojsonnet.StringValue)
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
	value gojsonnet.Value,
	path string,
) ([]string, error) {
	array, ok := value.(gojsonnet.ArrayValue)
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
		stringValue, ok := value.(gojsonnet.StringValue)
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

func jsonnetValueType(value gojsonnet.Value) string {
	switch value.(type) {
	case gojsonnet.NullValue:
		return "null"
	case gojsonnet.BoolValue:
		return "boolean"
	case gojsonnet.NumberValue:
		return "number"
	case gojsonnet.StringValue:
		return "string"
	case gojsonnet.ArrayValue:
		return "array"
	case gojsonnet.ObjectValue:
		return "object"
	case gojsonnet.FunctionValue:
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
