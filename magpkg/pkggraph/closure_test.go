package pkggraph

import (
	"slices"
	"testing"
)

func TestRuntimeClosureIsDependencyFirstAndExcludesBuildDeps(t *testing.T) {
	var builder Builder
	add := func(definition Definition) ID {
		t.Helper()
		id, err := builder.AddDefinition(definition)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	sharedID := add(Definition{
		Name:  "shared",
		Build: BuildSpec{Kind: BuildNone},
	})
	firstID := add(Definition{
		Name:    "first",
		Build:   BuildSpec{Kind: BuildNone},
		RunDeps: []ID{sharedID},
	})
	secondID := add(Definition{
		Name:  "second",
		Build: BuildSpec{Kind: BuildNone},
	})
	buildOnlyID := add(Definition{
		Name:  "build-only",
		Build: BuildSpec{Kind: BuildNone},
	})
	rootID := add(Definition{
		Name:      "root",
		Build:     BuildSpec{Kind: BuildNone},
		RunDeps:   []ID{firstID, secondID},
		BuildDeps: []ID{buildOnlyID},
	})
	otherRootID := add(Definition{
		Name:    "other-root",
		Build:   BuildSpec{Kind: BuildNone},
		RunDeps: []ID{secondID},
	})

	graph := builder.Graph()
	got, err := graph.RuntimeClosure(rootID, otherRootID, rootID)
	if err != nil {
		t.Fatal(err)
	}
	want := []ID{
		sharedID,
		firstID,
		secondID,
		rootID,
		otherRootID,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("RuntimeClosure() = %v, want %v", got, want)
	}
}

func TestRuntimeClosureRejectsUnknownRoot(t *testing.T) {
	var builder Builder
	if _, err := builder.AddDefinition(Definition{
		Build: BuildSpec{Kind: BuildNone},
	}); err != nil {
		t.Fatal(err)
	}
	graph := builder.Graph()

	unknown, err := IDForDefinition(Definition{
		Name:  "unknown",
		Build: BuildSpec{Kind: BuildNone},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RuntimeClosure(unknown); err == nil {
		t.Fatal("RuntimeClosure() accepted a root outside the graph")
	}
}
