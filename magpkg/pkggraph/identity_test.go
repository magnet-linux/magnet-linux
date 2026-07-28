package pkggraph

import (
	"strings"
	"testing"
)

func TestIDUsesLowercaseUnpaddedBase32(t *testing.T) {
	packageID := idFromCanonical([]byte("canonical package definition"))
	const want ID = "al6yavugrwtiaphzx4bjvb4fns4fwmsevxdzonrd6btbdozfelea"
	if packageID != want {
		t.Fatalf("package ID = %q, want %q", packageID, want)
	}
	if err := ValidateID(packageID); err != nil {
		t.Fatalf("validate generated package ID: %v", err)
	}

	for _, invalid := range []ID{
		ID(strings.ToUpper(string(packageID))),
		ID(string(packageID[:IDLength-1]) + "1"),
		packageID + "=",
	} {
		if err := ValidateID(invalid); err == nil {
			t.Fatalf("accepted invalid package ID %q", invalid)
		}
	}
}

func TestDocumentedDefinitionID(t *testing.T) {
	id, err := IDForDefinition(Definition{
		Name: "hello",
		Build: BuildSpec{
			Kind:   BuildScript,
			Script: "true",
		},
		RunDeps:   []ID{},
		BuildDeps: []ID{},
		Fetch:     []Fetch{},
	})
	if err != nil {
		t.Fatal(err)
	}
	const want ID = "a5wcuzmc5xqg4aj4rbdxodaxv7ycwedcwqt525xmk7kqflkix44q"
	if id != want {
		t.Fatalf("IDForDefinition() = %s, want %s", id, want)
	}
}
