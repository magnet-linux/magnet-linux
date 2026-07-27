package pkgstore

import (
	"reflect"
	"testing"
)

func TestResolveBuildEnvironmentAppliesOverridesWithoutMutatingDefaults(
	t *testing.T,
) {
	environment := resolveBuildEnvironment(map[string]string{
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
