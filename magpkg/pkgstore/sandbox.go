package pkgstore

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

func runBuildSandbox(
	ctx context.Context,
	rootfs string,
	buildDirectory string,
	outputDirectory string,
	fetchDirectory string,
	script string,
	buildEnvironment map[string]string,
	options normalizedBuildOptions,
) error {
	bubblewrap, err := exec.LookPath("bwrap")
	if err != nil {
		return fmt.Errorf("find bubblewrap: %w", err)
	}
	scriptPath := filepath.Join(buildDirectory, ".magpkg-build")
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return fmt.Errorf("write package build script: %w", err)
	}

	arguments := []string{
		"--die-with-parent",
		"--new-session",
		"--unshare-all",
		"--hostname", "magpkg",
		// The assembled root is a disposable per-package copy. Keep it
		// writable because some package builds install temporary files into
		// / while producing their real result under /out.
		"--bind", rootfs, "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
		"--bind", buildDirectory, "/build",
		"--bind", outputDirectory, "/out",
		"--ro-bind", fetchDirectory, "/fetch",
		"--clearenv",
	}
	buildEnvironment = resolveBuildEnvironment(buildEnvironment)
	names := make([]string, 0, len(buildEnvironment))
	for name := range buildEnvironment {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		arguments = append(
			arguments,
			"--setenv",
			name,
			buildEnvironment[name],
		)
	}
	arguments = append(
		arguments,
		"--setenv",
		buildParallelismEnvironmentVariable,
		buildParallelismValue(options),
		"--chdir", "/build",
		"/bin/sh", "-e", "/build/.magpkg-build",
	)
	command := exec.CommandContext(ctx, bubblewrap, arguments...)
	command.Stdout = options.stdout
	command.Stderr = options.stderr
	if err := command.Run(); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return fmt.Errorf("run package build sandbox: %w", err)
	}
	return nil
}
