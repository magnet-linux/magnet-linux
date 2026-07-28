package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
	"github.com/spf13/pflag"
)

func runUninstall(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	flags := pflag.NewFlagSet("uninstall", pflag.ContinueOnError)
	flags.SetOutput(stderr)
	destination := flags.String(
		"dest",
		"",
		"destination root to uninstall (required)",
	)
	dryRun := flags.Bool(
		"dry-run",
		false,
		"plan and verify the uninstall without changing managed files",
	)
	force := flags.Bool(
		"force",
		false,
		"remove manifest-owned paths even when they have been modified",
	)
	flags.Usage = func() {
		fmt.Fprintln(
			stderr,
			"usage: magpkg uninstall --dest PATH [options]",
		)
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(
			stderr,
			"Removes paths recorded in the destination's magpkg manifest.",
		)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return errors.New("uninstall does not accept positional arguments")
	}
	if *destination == "" {
		flags.Usage()
		return errors.New("uninstall requires --dest")
	}

	result, err := pkgstore.Uninstall(
		ctx,
		*destination,
		pkgstore.UninstallOptions{
			DryRun: *dryRun,
			Force:  *force,
		},
	)
	if err != nil {
		return err
	}
	writeDestinationChanges(stdout, result)
	return nil
}
