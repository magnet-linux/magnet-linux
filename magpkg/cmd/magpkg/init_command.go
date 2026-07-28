package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
	"github.com/spf13/pflag"
)

func runInit(args []string, stdout io.Writer, stderr io.Writer) error {
	flags := pflag.NewFlagSet("init", pflag.ContinueOnError)
	flags.SetOutput(stderr)
	storeFlag := flags.String(
		"store",
		"",
		"package store path (default: $MAGPKG_STORE or ~/.magpkg/store)",
	)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: magpkg init [--store PATH] [PATH]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 1 {
		flags.Usage()
		return errors.New("init accepts at most one store path")
	}
	if flags.NArg() == 1 {
		if *storeFlag != "" {
			return errors.New(
				"store path specified both positionally and with --store",
			)
		}
		*storeFlag = flags.Arg(0)
	}

	storePath, err := resolveStorePath(*storeFlag)
	if err != nil {
		return err
	}
	if err := pkgstore.Init(storePath); err != nil {
		return fmt.Errorf("initialize store %q: %w", storePath, err)
	}
	store, err := pkgstore.Open(storePath, pkgstore.OpenOptions{})
	if err != nil {
		return fmt.Errorf("open initialized store %q: %w", storePath, err)
	}
	fmt.Fprintln(stdout, store.Path())
	return nil
}
