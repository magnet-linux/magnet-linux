package shellenv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
)

// RunOptions supplies host process state to the package-provided launcher.
// Env is passed exactly; a nil Env passes an empty environment.
type RunOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Env    []string
	Dir    string
}

// Run executes environment's package-provided launcher. The launcher receives
// the absolute environment root as its first argument, followed by command.
// An empty command asks the launcher to start its default interactive shell.
func Run(
	ctx context.Context,
	environment *Environment,
	command []string,
	options RunOptions,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if environment == nil || !environment.isOpen() {
		return errors.New("shellenv: environment is not leased")
	}
	if options.Dir == "" {
		return errors.New("shellenv: working directory is empty")
	}
	absoluteDirectory, err := filepath.Abs(options.Dir)
	if err != nil {
		return fmt.Errorf("shellenv: resolve working directory: %w", err)
	}
	launcher := filepath.Join(environment.RootPath, LauncherPath)
	info, err := os.Lstat(launcher)
	if err != nil {
		return fmt.Errorf("shellenv: inspect launcher %q: %w", launcher, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("shellenv: launcher %q is not a regular file", launcher)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("shellenv: launcher %q is not executable", launcher)
	}

	arguments := make([]string, 0, len(command)+1)
	arguments = append(arguments, environment.RootPath)
	arguments = append(arguments, command...)
	process := exec.CommandContext(ctx, launcher, arguments...)
	process.Stdin = options.Stdin
	process.Stdout = options.Stdout
	process.Stderr = options.Stderr
	process.Dir = absoluteDirectory
	process.Env = slices.Clone(options.Env)
	if process.Env == nil {
		process.Env = []string{}
	}
	if err := process.Run(); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return err
	}
	return nil
}
