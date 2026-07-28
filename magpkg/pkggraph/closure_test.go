package pkggraph

import (
	"slices"
	"testing"
)

func TestRuntimeClosureIsDependencyFirstAndExcludesBuildDeps(t *testing.T) {
	sharedDefinition := Definition{
		Name:  "shared",
		Build: BuildSpec{Kind: BuildNone},
	}
	sharedID := testGraphID(t, sharedDefinition)
	firstDefinition := Definition{
		Name:    "first",
		Build:   BuildSpec{Kind: BuildNone},
		RunDeps: []ID{sharedID},
	}
	firstID := testGraphID(t, firstDefinition)
	secondDefinition := Definition{
		Name:  "second",
		Build: BuildSpec{Kind: BuildNone},
	}
	secondID := testGraphID(t, secondDefinition)
	buildOnlyDefinition := Definition{
		Name:  "build-only",
		Build: BuildSpec{Kind: BuildNone},
	}
	buildOnlyID := testGraphID(t, buildOnlyDefinition)
	rootDefinition := Definition{
		Name:      "root",
		Build:     BuildSpec{Kind: BuildNone},
		RunDeps:   []ID{firstID, secondID},
		BuildDeps: []ID{buildOnlyID},
	}
	rootID := testGraphID(t, rootDefinition)
	otherRootDefinition := Definition{
		Name:    "other-root",
		Build:   BuildSpec{Kind: BuildNone},
		RunDeps: []ID{secondID},
	}
	otherRootID := testGraphID(t, otherRootDefinition)

	graph := &Graph{
		Nodes: map[ID]Definition{
			sharedID:    sharedDefinition,
			firstID:     firstDefinition,
			secondID:    secondDefinition,
			buildOnlyID: buildOnlyDefinition,
			rootID:      rootDefinition,
			otherRootID: otherRootDefinition,
		},
	}
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

func testGraphID(t *testing.T, definition Definition) ID {
	t.Helper()
	id, err := IDForDefinition(definition)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
