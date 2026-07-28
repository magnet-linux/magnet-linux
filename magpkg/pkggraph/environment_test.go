package pkggraph

import (
	"reflect"
	"testing"
)

func TestResolveBuildEnvironmentAppliesOverridesWithoutMutatingDefaults(
	t *testing.T,
) {
	environment := ResolveBuildEnvironment(map[string]string{
		"PATH":   "/bin",
		"CUSTOM": "value",
	})

	want := map[string]string{
		"PATH":         "/bin",
		"HOME":         "/build",
		"SHELL":        "/bin/sh",
		"CONFIG_SHELL": "/bin/sh",
		"LC_ALL":       "C",
		"TZ":           "UTC",
		"CUSTOM":       "value",
	}
	if !reflect.DeepEqual(environment, want) {
		t.Fatalf("resolveBuildEnvironment() = %#v, want %#v", environment, want)
	}
	if got := defaultBuildEnvironment["PATH"]; got != "/usr/bin:/bin:/usr/sbin:/sbin" {
		t.Fatalf("default PATH was mutated to %q", got)
	}
}

func TestValidateBuildEnvironmentRejectsExecutorVariable(t *testing.T) {
	err := validateBuildEnvironment(map[string]string{
		buildParallelismEnvironmentVariable: "8",
	})
	if err == nil {
		t.Fatal("validateBuildEnvironment() accepted BUILD_PARALLELISM")
	}
}

func TestNormalizeDefinitionStoresOnlyEffectiveOverrides(t *testing.T) {
	definition := Definition{
		Build: BuildSpec{Kind: BuildNone},
		BuildEnv: map[string]string{
			"HOME":   "/build",
			"PATH":   "/bin",
			"CUSTOM": "value",
		},
	}
	normalized, err := NormalizeDefinition(definition)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"PATH":   "/bin",
		"CUSTOM": "value",
	}
	if !reflect.DeepEqual(normalized.BuildEnv, want) {
		t.Fatalf("normalized BuildEnv = %#v, want %#v", normalized.BuildEnv, want)
	}
	if len(definition.BuildEnv) != 3 {
		t.Fatal("NormalizeDefinition mutated its input")
	}
}
