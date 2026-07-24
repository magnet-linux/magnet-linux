package pkgstore_test

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/andrewchambers/magpkg/pkgstore"
)

func TestPackageGraphFromJsonnet(t *testing.T) {
	jsonnetFile := writeJsonnet(t, "packages.jsonnet", `
		local dependency = {
			name: "dependency",
			build: "build dependency",
			runDeps: [],
			buildDeps: [],
			fetch: [],
		};

		local equivalentDependency = {
			name: "dependency",
			build: "build dependency",
			runDeps: [],
			buildDeps: [],
			fetch: [],
		};

		local root = {
			name: "root",
			build: "build root",
			runDeps: [dependency],
			buildDeps: [dependency],
			fetch: [{
				filename: "source.tar.gz",
				sha256: "0000000000000000000000000000000000000000000000000000000000000000",
				urls: [
					"https://example.com/source.tar.gz",
					"https://mirror.example.com/source.tar.gz",
				],
			}],
		};

		{
			dependency: dependency,
			dependencyAlias: dependency,
			equivalentDependency: equivalentDependency,
			root: root,
		}
	`)

	graph, err := pkgstore.PackageGraphFromJsonnet(
		context.Background(),
		jsonnetFile,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(graph.Nodes) != 2 {
		t.Fatalf("len(Nodes) = %d, want 2", len(graph.Nodes))
	}
	if len(graph.Roots) != 4 {
		t.Fatalf("len(Roots) = %d, want 4", len(graph.Roots))
	}

	dependencyID := graph.Roots["dependency"]
	if dependencyID == "" {
		t.Fatal("dependency root has an empty package ID")
	}
	if graph.Roots["dependencyAlias"] != dependencyID {
		t.Fatal("references to the same object have different package IDs")
	}
	if graph.Roots["equivalentDependency"] != dependencyID {
		t.Fatal("equivalent package definitions were not content-deduplicated")
	}
	assertPackageID(t, dependencyID)

	rootID := graph.Roots["root"]
	assertPackageID(t, rootID)
	root := graph.Nodes[rootID]
	if root.Name != "root" {
		t.Fatalf("root.Name = %q, want %q", root.Name, "root")
	}
	if root.Build != "build root" {
		t.Fatalf("root.Build = %q, want %q", root.Build, "build root")
	}
	if !reflect.DeepEqual(root.RunDeps, []pkgstore.PackageID{dependencyID}) {
		t.Fatalf("root.RunDeps = %#v, want [%q]", root.RunDeps, dependencyID)
	}
	if !reflect.DeepEqual(root.BuildDeps, []pkgstore.PackageID{dependencyID}) {
		t.Fatalf("root.BuildDeps = %#v, want [%q]", root.BuildDeps, dependencyID)
	}
	wantFetch := []pkgstore.Fetch{{
		Filename: "source.tar.gz",
		SHA256:   strings.Repeat("0", 64),
		URLs: []string{
			"https://example.com/source.tar.gz",
			"https://mirror.example.com/source.tar.gz",
		},
	}}
	if !reflect.DeepEqual(root.Fetch, wantFetch) {
		t.Fatalf("root.Fetch = %#v, want %#v", root.Fetch, wantFetch)
	}
}

func TestPackageGraphFromJsonnetImports(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "bootstrap.jsonnet"),
		[]byte(`{
			bootstrap: {
				name: "bootstrap",
				build: "bootstrap",
				runDeps: [],
				buildDeps: [],
				fetch: [],
			},
		}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	jsonnetFile := filepath.Join(dir, "packages.jsonnet")
	if err := os.WriteFile(
		jsonnetFile,
		[]byte(`
			local imported = import "./bootstrap.jsonnet";
			local root = {
				name: "root",
				build: "root",
				runDeps: [],
				buildDeps: [imported.bootstrap],
				fetch: [],
			};
			{ root: root }
		`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	graph, err := pkgstore.PackageGraphFromJsonnet(
		context.Background(),
		jsonnetFile,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(graph.Nodes) != 2 {
		t.Fatalf("len(Nodes) = %d, want 2", len(graph.Nodes))
	}
	root := graph.Nodes[graph.Roots["root"]]
	if len(root.BuildDeps) != 1 {
		t.Fatalf("root.BuildDeps = %#v, want one dependency", root.BuildDeps)
	}
	bootstrap := graph.Nodes[root.BuildDeps[0]]
	if bootstrap.Name != "bootstrap" {
		t.Fatalf("bootstrap.Name = %q, want %q", bootstrap.Name, "bootstrap")
	}
}

func TestPackageGraphFromJsonnetIsDeterministic(t *testing.T) {
	source := `{
		package: {
			name: "package",
			build: "build",
			runDeps: [],
			buildDeps: [],
			fetch: [],
		},
	}`
	firstFile := writeJsonnet(t, "first.jsonnet", source)
	secondFile := writeJsonnet(t, "second.jsonnet", source)

	first, err := pkgstore.PackageGraphFromJsonnet(context.Background(), firstFile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := pkgstore.PackageGraphFromJsonnet(context.Background(), secondFile)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("materializing identical definitions produced different graphs")
	}
}

func TestPackageGraphFromJsonnetRejectsDependencyCycle(t *testing.T) {
	jsonnetFile := writeJsonnet(t, "cycle.jsonnet", `
		local package = {
			name: "cyclic",
			build: "build",
			runDeps: [package],
			buildDeps: [],
			fetch: [],
		};
		{ package: package }
	`)

	_, err := pkgstore.PackageGraphFromJsonnet(
		context.Background(),
		jsonnetFile,
	)
	if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("error = %v, want dependency cycle error", err)
	}
}

func TestPackageGraphFromJsonnetValidatesDefinitions(t *testing.T) {
	tests := []struct {
		name       string
		definition string
		wantError  string
	}{
		{
			name: "top level",
			definition: `
				[]
			`,
			wantError: "expected top-level object",
		},
		{
			name: "missing field",
			definition: `{
				package: {
					name: "package",
					build: "build",
					runDeps: [],
					buildDeps: [],
				},
			}`,
			wantError: `missing field "fetch"`,
		},
		{
			name: "dependency type",
			definition: `{
				package: {
					name: "package",
					build: "build",
					runDeps: "not an array",
					buildDeps: [],
					fetch: [],
				},
			}`,
			wantError: "root[\"package\"].runDeps: expected array",
		},
		{
			name: "fetch checksum",
			definition: `{
				package: {
					name: "package",
					build: "build",
					runDeps: [],
					buildDeps: [],
					fetch: [{
						filename: "source",
						sha256: "invalid",
						urls: [],
					}],
				},
			}`,
			wantError: "expected a 64-character hexadecimal SHA-256 digest",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			jsonnetFile := writeJsonnet(t, "invalid.jsonnet", test.definition)
			_, err := pkgstore.PackageGraphFromJsonnet(
				context.Background(),
				jsonnetFile,
			)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want error containing %q", err, test.wantError)
			}
		})
	}
}

func TestPackageGraphFromJsonnetHonorsCanceledContext(t *testing.T) {
	jsonnetFile := writeJsonnet(t, "package.jsonnet", `{}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := pkgstore.PackageGraphFromJsonnet(ctx, jsonnetFile)
	if err != context.Canceled {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func writeJsonnet(t *testing.T, name string, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertPackageID(t *testing.T, id pkgstore.PackageID) {
	t.Helper()
	decoded, err := hex.DecodeString(string(id))
	if err != nil {
		t.Fatalf("PackageID %q is not hexadecimal: %v", id, err)
	}
	if len(decoded) != 32 {
		t.Fatalf("PackageID %q decodes to %d bytes, want 32", id, len(decoded))
	}
}
