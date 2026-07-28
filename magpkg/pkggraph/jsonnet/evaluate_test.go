package jsonnet_test

import (
	"context"
	"encoding/base32"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
	graphjsonnet "github.com/andrewchambers/magnet-linux/magpkg/pkggraph/jsonnet"
)

func TestEvaluateFile(t *testing.T) {
	jsonnetFile := writeJsonnet(t, "packages.jsonnet", `
		local dependency = {
			name: "dependency",
			build: { kind: "script", script: "build dependency" },
			runDeps: [],
			buildDeps: [],
			fetch: [],
		};

		local equivalentDependency = {
			name: "dependency",
			build: { kind: "script", script: "build dependency" },
			runDeps: [],
			buildDeps: [],
			fetch: [],
		};

		local root = {
			name: "root",
			build: { kind: "script", script: "build root" },
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

	graph, err := graphjsonnet.EvaluateFile(
		context.Background(),
		jsonnetFile,
		graphjsonnet.Options{},
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
	assertID(t, dependencyID)

	rootID := graph.Roots["root"]
	assertID(t, rootID)
	root := graph.Nodes[rootID]
	if root.Name != "root" {
		t.Fatalf("root.Name = %q, want %q", root.Name, "root")
	}
	wantBuild := pkggraph.BuildSpec{
		Kind:   pkggraph.BuildScript,
		Script: "build root",
	}
	if root.Build != wantBuild {
		t.Fatalf("root.Build = %#v, want %#v", root.Build, wantBuild)
	}
	if root.BuildEnv != nil {
		t.Fatalf("root.BuildEnv = %#v, want nil overrides", root.BuildEnv)
	}
	if !reflect.DeepEqual(root.RunDeps, []pkggraph.ID{dependencyID}) {
		t.Fatalf("root.RunDeps = %#v, want [%q]", root.RunDeps, dependencyID)
	}
	if !reflect.DeepEqual(root.BuildDeps, []pkggraph.ID{dependencyID}) {
		t.Fatalf("root.BuildDeps = %#v, want [%q]", root.BuildDeps, dependencyID)
	}
	wantFetch := []pkggraph.Fetch{{
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

func TestEvaluateFileAllowsOmittedName(t *testing.T) {
	jsonnetFile := writeJsonnet(t, "unnamed.jsonnet", `{
		package: {
			build: { kind: "none" },
			runDeps: [],
			buildDeps: [],
			fetch: [],
		},
	}`)

	graph, err := graphjsonnet.EvaluateFile(
		context.Background(),
		jsonnetFile,
		graphjsonnet.Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	definition := graph.Nodes[graph.Roots["package"]]
	if definition.Name != "" {
		t.Fatalf("package name = %q, want empty", definition.Name)
	}
}

func TestEvaluateFileNormalizesBuildEnvironmentOverrides(t *testing.T) {
	jsonnetFile := writeJsonnet(t, "environment.jsonnet", `
		local package = {
			name: "package",
			build: { kind: "script", script: "build" },
			runDeps: [],
			buildDeps: [],
			fetch: [],
		};

		{
			default: package,
			explicitDefault: package + {
				buildEnv: {
					PATH: "/usr/bin:/bin:/usr/sbin:/sbin",
					HOME: "/build",
					SHELL: "/bin/sh",
					CONFIG_SHELL: "/bin/sh",
					LC_ALL: "C",
					TZ: "UTC",
				},
			},
			custom: package + {
				buildEnv: {
					PATH: "/bin",
					CUSTOM: "custom value",
				},
			},
		}
	`)

	graph, err := graphjsonnet.EvaluateFile(
		context.Background(),
		jsonnetFile,
		graphjsonnet.Options{},
	)
	if err != nil {
		t.Fatal(err)
	}

	defaultID := graph.Roots["default"]
	if graph.Roots["explicitDefault"] != defaultID {
		t.Fatal("omitted and explicitly default build environments have different IDs")
	}
	customID := graph.Roots["custom"]
	if customID == defaultID {
		t.Fatal("build environment override did not change the package ID")
	}

	wantCustom := map[string]string{
		"PATH":   "/bin",
		"CUSTOM": "custom value",
	}
	if got := graph.Nodes[customID].BuildEnv; !reflect.DeepEqual(got, wantCustom) {
		t.Fatalf("custom BuildEnv = %#v, want %#v", got, wantCustom)
	}
}

func TestEvaluateFileImports(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "bootstrap.jsonnet"),
		[]byte(`{
			bootstrap: {
				name: "bootstrap",
				build: { kind: "script", script: "bootstrap" },
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
				build: { kind: "script", script: "root" },
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

	graph, err := graphjsonnet.EvaluateFile(
		context.Background(),
		jsonnetFile,
		graphjsonnet.Options{},
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

func TestEvaluateFileIsDeterministic(t *testing.T) {
	source := `{
		package: {
			name: "package",
			build: { kind: "script", script: "build" },
			runDeps: [],
			buildDeps: [],
			fetch: [],
		},
	}`
	firstFile := writeJsonnet(t, "first.jsonnet", source)
	secondFile := writeJsonnet(t, "second.jsonnet", source)

	first, err := graphjsonnet.EvaluateFile(
		context.Background(),
		firstFile,
		graphjsonnet.Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := graphjsonnet.EvaluateFile(
		context.Background(),
		secondFile,
		graphjsonnet.Options{},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("materializing identical definitions produced different graphs")
	}
}

func TestEvaluateFileRejectsDependencyCycle(t *testing.T) {
	jsonnetFile := writeJsonnet(t, "cycle.jsonnet", `
		local package = {
			name: "cyclic",
			build: { kind: "script", script: "build" },
			runDeps: [package],
			buildDeps: [],
			fetch: [],
		};
		{ package: package }
	`)

	_, err := graphjsonnet.EvaluateFile(
		context.Background(),
		jsonnetFile,
		graphjsonnet.Options{},
	)
	if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("error = %v, want dependency cycle error", err)
	}
}

func TestEvaluateFileValidatesDefinitions(t *testing.T) {
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
					build: { kind: "script", script: "build" },
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
					build: { kind: "script", script: "build" },
					runDeps: "not an array",
					buildDeps: [],
					fetch: [],
				},
			}`,
			wantError: "root[\"package\"].runDeps: expected array",
		},
		{
			name: "package name",
			definition: `{
				package: {
					name: "../package",
					build: { kind: "script", script: "build" },
					runDeps: [],
					buildDeps: [],
					fetch: [],
				},
			}`,
			wantError: "must start with an ASCII letter or digit",
		},
		{
			name: "fetch checksum",
			definition: `{
				package: {
					name: "package",
					build: { kind: "script", script: "build" },
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
		{
			name: "fetch filename path",
			definition: `{
				package: {
					name: "package",
					build: { kind: "script", script: "build" },
					runDeps: [],
					buildDeps: [],
					fetch: [{
						filename: "sources/source.tar",
						sha256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
						urls: [],
					}],
				},
			}`,
			wantError: "must be a single path component",
		},
		{
			name: "duplicate fetch filename",
			definition: `{
				package: {
					name: "package",
					build: { kind: "script", script: "build" },
					runDeps: [],
					buildDeps: [],
					fetch: [
						{
							filename: "source.tar",
							sha256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
							urls: [],
						},
						{
							filename: "source.tar",
							sha256: "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
							urls: [],
						},
					],
				},
			}`,
			wantError: "duplicate fetch filename",
		},
		{
			name: "build environment type",
			definition: `{
				package: {
					name: "package",
					build: { kind: "script", script: "build" },
					buildEnv: [],
					runDeps: [],
					buildDeps: [],
					fetch: [],
				},
			}`,
			wantError: "buildEnv: expected object",
		},
		{
			name: "build environment value",
			definition: `{
				package: {
					name: "package",
					build: { kind: "script", script: "build" },
					buildEnv: { PATH: 42 },
					runDeps: [],
					buildDeps: [],
					fetch: [],
				},
			}`,
			wantError: `buildEnv["PATH"]: expected string`,
		},
		{
			name: "build environment name",
			definition: `{
				package: {
					name: "package",
					build: { kind: "script", script: "build" },
					buildEnv: { "INVALID-NAME": "value" },
					runDeps: [],
					buildDeps: [],
					fetch: [],
				},
			}`,
			wantError: "is not a valid shell variable name",
		},
		{
			name: "executor environment",
			definition: `{
				package: {
					name: "package",
					build: { kind: "script", script: "build" },
					buildEnv: { BUILD_PARALLELISM: "99" },
					runDeps: [],
					buildDeps: [],
					fetch: [],
				},
			}`,
			wantError: "BUILD_PARALLELISM is controlled by the build executor",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			jsonnetFile := writeJsonnet(t, "invalid.jsonnet", test.definition)
			_, err := graphjsonnet.EvaluateFile(
				context.Background(),
				jsonnetFile,
				graphjsonnet.Options{},
			)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want error containing %q", err, test.wantError)
			}
		})
	}
}

func TestEvaluateFileHonorsCanceledContext(t *testing.T) {
	jsonnetFile := writeJsonnet(t, "package.jsonnet", `{}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := graphjsonnet.EvaluateFile(
		ctx,
		jsonnetFile,
		graphjsonnet.Options{},
	)
	if err != context.Canceled {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestRepositoryPackageGraphs(t *testing.T) {
	for _, name := range []string{
		"bootstrap.jsonnet",
		"bubblewrap.jsonnet",
		"core.jsonnet",
		"hello.jsonnet",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", "..", "packages", name)
			if _, err := graphjsonnet.EvaluateFile(
				context.Background(),
				path,
				graphjsonnet.Options{},
			); err != nil {
				t.Fatal(err)
			}
		})
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

func assertID(t *testing.T, id pkggraph.ID) {
	t.Helper()
	encoding := base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").
		WithPadding(base32.NoPadding)
	decoded, err := encoding.DecodeString(string(id))
	if err != nil {
		t.Fatalf("ID %q is not lowercase Base32: %v", id, err)
	}
	if len(decoded) != 32 {
		t.Fatalf("ID %q decodes to %d bytes, want 32", id, len(decoded))
	}
	if got, want := len(id), 52; got != want {
		t.Fatalf("ID %q has length %d, want %d", id, got, want)
	}
}
