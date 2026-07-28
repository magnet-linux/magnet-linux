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

func TestNormalizeDefinitionCanonicalizesEmptyLists(t *testing.T) {
	normalized, err := NormalizeDefinition(Definition{
		Build: BuildSpec{Kind: BuildNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if normalized.RunDeps == nil ||
		normalized.BuildDeps == nil ||
		normalized.Fetch == nil {
		t.Fatalf(
			"normalized lists = %#v, want empty non-nil lists",
			normalized,
		)
	}

	nilID, err := IDForDefinition(Definition{Build: BuildSpec{Kind: BuildNone}})
	if err != nil {
		t.Fatal(err)
	}
	emptyID, err := IDForDefinition(Definition{
		Build:     BuildSpec{Kind: BuildNone},
		RunDeps:   []ID{},
		BuildDeps: []ID{},
		Fetch:     []Fetch{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if nilID != emptyID {
		t.Fatalf(
			"nil-list ID %s differs from empty-list ID %s",
			nilID,
			emptyID,
		)
	}
}
