package pkggraph

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuilderRejectsMissingDependency(t *testing.T) {
	missing := ID(strings.Repeat("a", IDLength))
	var builder Builder
	_, err := builder.AddDefinition(Definition{
		Name:    "root",
		Build:   BuildSpec{Kind: BuildNone},
		RunDeps: []ID{missing},
	})
	if err == nil || !strings.Contains(err.Error(), "is not in the graph") {
		t.Fatalf("AddDefinition() error = %v, want missing dependency error", err)
	}
}

func TestBuilderRejectsRootOutsideGraph(t *testing.T) {
	var builder Builder
	err := builder.SetRoot("root", ID(strings.Repeat("a", IDLength)))
	if err == nil || !strings.Contains(err.Error(), "is not in the graph") {
		t.Fatalf("SetRoot() error = %v, want missing package error", err)
	}
}

func TestBuilderComputesCanonicalIdentities(t *testing.T) {
	definition := Definition{
		Name:  "package",
		Build: BuildSpec{Kind: BuildNone},
		BuildEnv: map[string]string{
			"HOME": "/build",
			"PATH": "/bin",
		},
	}
	want, err := IDForDefinition(definition)
	if err != nil {
		t.Fatal(err)
	}

	var builder Builder
	got, err := builder.AddDefinition(definition)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("AddDefinition() = %s, want %s", got, want)
	}

	// Equivalent spellings of the same definition converge on one identity.
	again, err := builder.AddDefinition(Definition{
		Name:  "package",
		Build: BuildSpec{Kind: BuildNone},
		BuildEnv: map[string]string{
			"PATH": "/bin",
		},
		RunDeps:   []ID{},
		BuildDeps: []ID{},
		Fetch:     []Fetch{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if again != want {
		t.Fatalf("equivalent AddDefinition() = %s, want %s", again, want)
	}

	graph := builder.Graph()
	if err := builder.SetRoot("package", want); err == nil {
		t.Fatal("SetRoot() succeeded on a reset builder")
	}
	stored, ok := graph.Definition(want)
	if !ok {
		t.Fatal("sealed graph is missing its definition")
	}
	if !reflect.DeepEqual(stored.BuildEnv, map[string]string{"PATH": "/bin"}) {
		t.Fatalf("stored BuildEnv = %#v, want effective overrides only", stored.BuildEnv)
	}
}

func TestGraphAccessorsOnZeroGraph(t *testing.T) {
	graph := &Graph{}
	if _, ok := graph.Definition(ID(strings.Repeat("a", IDLength))); ok {
		t.Fatal("zero graph returned a definition")
	}
	if graph.HasNode(ID(strings.Repeat("a", IDLength))) {
		t.Fatal("zero graph reported a node")
	}
	if _, ok := graph.Root("name"); ok {
		t.Fatal("zero graph returned a root")
	}
	if len(graph.NodeIDs()) != 0 || len(graph.RootNames()) != 0 {
		t.Fatal("zero graph enumerated contents")
	}
}
