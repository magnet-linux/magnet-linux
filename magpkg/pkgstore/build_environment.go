package pkgstore

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const buildParallelismEnvironmentVariable = "BUILD_PARALLELISM"

var defaultBuildEnvironment = map[string]string{
	"PATH":         "/usr/bin:/bin:/usr/sbin:/sbin",
	"HOME":         "/build",
	"SHELL":        "/bin/sh",
	"CONFIG_SHELL": "/bin/sh",
	"LC_ALL":       "C",
	"TZ":           "UTC",
}

func resolveBuildEnvironment(overrides map[string]string) map[string]string {
	environment := make(
		map[string]string,
		len(defaultBuildEnvironment)+len(overrides),
	)
	for name, value := range defaultBuildEnvironment {
		environment[name] = value
	}
	for name, value := range overrides {
		environment[name] = value
	}
	return environment
}

func validateBuildEnvironment(environment map[string]string) error {
	names := make([]string, 0, len(environment))
	for name := range environment {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := environment[name]
		if err := validateBuildEnvironmentName(name); err != nil {
			return err
		}
		if name == buildParallelismEnvironmentVariable {
			return fmt.Errorf(
				"%s is controlled by the build executor",
				buildParallelismEnvironmentVariable,
			)
		}
		if strings.IndexByte(value, 0) >= 0 {
			return fmt.Errorf("build environment variable %q contains NUL", name)
		}
	}
	return nil
}

func validateBuildEnvironmentName(name string) error {
	if name == "" {
		return errors.New("build environment variable name is empty")
	}
	for index := range len(name) {
		character := name[index]
		if character == '_' ||
			character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			index > 0 && character >= '0' && character <= '9' {
			continue
		}
		return fmt.Errorf(
			"build environment variable %q is not a valid shell variable name",
			name,
		)
	}
	return nil
}
