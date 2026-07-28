package pkggraph

import (
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{
		"",
		"a",
		"gcc-15.1.0",
		"libstdc++-rt",
		"package_name",
		strings.Repeat("a", maximumPackageNameLength),
	}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q): %v", name, err)
		}
	}

	invalid := []string{
		".hidden",
		"-option",
		"has space",
		"has/slash",
		"naïve",
		strings.Repeat("a", maximumPackageNameLength+1),
	}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) succeeded", name)
		}
	}
}

func TestValidateBuildSpec(t *testing.T) {
	valid := []BuildSpec{
		{Kind: BuildNone},
		{Kind: BuildUnpack},
		{Kind: BuildScript, Script: "true"},
	}
	for _, build := range valid {
		if err := ValidateBuildSpec(build); err != nil {
			t.Errorf("ValidateBuildSpec(%+v): %v", build, err)
		}
	}
	invalid := []BuildSpec{
		{},
		{Kind: BuildNone, Script: "true"},
		{Kind: BuildUnpack, Script: "true"},
		{Kind: BuildScript},
	}
	for _, build := range invalid {
		if err := ValidateBuildSpec(build); err == nil {
			t.Errorf("ValidateBuildSpec(%+v) succeeded", build)
		}
	}
}

func TestGraphValidateRejectsMissingDependency(t *testing.T) {
	missing := ID(strings.Repeat("a", IDLength))
	definition := Definition{
		Name:    "root",
		Build:   BuildSpec{Kind: BuildNone},
		RunDeps: []ID{missing},
	}
	id, err := IDForDefinition(definition)
	if err != nil {
		t.Fatal(err)
	}
	graph := &Graph{
		Nodes: map[ID]Definition{id: definition},
		Roots: map[string]ID{"root": id},
	}
	err = graph.Validate()
	if err == nil || !strings.Contains(err.Error(), "is not in the graph") {
		t.Fatalf("Validate() error = %v, want missing dependency error", err)
	}
}
