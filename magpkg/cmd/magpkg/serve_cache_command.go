package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
	"github.com/spf13/pflag"
)

func runServeCache(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	flags := pflag.NewFlagSet("serve-cache", pflag.ContinueOnError)
	flags.SetOutput(stderr)
	storeFlag := flags.String(
		"store",
		"",
		"package store path (default: $MAGPKG_STORE or ~/.magpkg/store)",
	)
	listen := flags.String(
		"listen",
		"127.0.0.1:8080",
		"TCP address on which to serve the cache",
	)
	noTouch := flags.Bool(
		"no-touch",
		false,
		"do not refresh artifact timestamps for GET requests",
	)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: magpkg serve-cache [options]")
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
		return errors.New("serve-cache does not accept positional arguments")
	}
	if *listen == "" {
		return errors.New("serve-cache listen address is empty")
	}

	storePath, err := resolveStorePath(*storeFlag)
	if err != nil {
		return err
	}
	store, err := pkgstore.Open(storePath, pkgstore.OpenOptions{})
	if err != nil {
		return fmt.Errorf(
			"open store %q (run `magpkg init` first): %w",
			storePath,
			err,
		)
	}
	handler, err := pkgstore.NewBinaryCacheHandler(
		store,
		pkgstore.BinaryCacheHandlerOptions{DisableTouch: *noTouch},
	)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("listen for binary cache: %w", err)
	}
	fmt.Fprintf(stdout, "http://%s\n", listener.Addr())

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.Serve(listener)
	}()

	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve binary cache: %w", err)
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		shutdownErr := server.Shutdown(shutdownContext)
		serveErr := <-serveErrors
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		if err := errors.Join(shutdownErr, serveErr); err != nil {
			return fmt.Errorf("shut down binary cache: %w", err)
		}
		return nil
	}
}
