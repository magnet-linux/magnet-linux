package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
)

func resolveStorePath(explicitStore string) (string, error) {
	store := explicitStore
	if store == "" {
		store = os.Getenv(storeEnvironmentVariable)
	}
	if store == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("determine home directory: %w", err)
		}
		store = filepath.Join(home, ".magpkg", "store")
	}
	absolute, err := filepath.Abs(store)
	if err != nil {
		return "", fmt.Errorf("resolve package store path: %w", err)
	}
	return absolute, nil
}

func resolveBinaryCacheURL(explicitCache string) string {
	if explicitCache != "" {
		return explicitCache
	}
	return os.Getenv(binaryCacheEnvironmentVariable)
}

func commandStoreOpenOptions(binaryCacheURL string) pkgstore.OpenOptions {
	return pkgstore.OpenOptions{
		BinaryCacheURL: binaryCacheURL,
		HTTPClient:     http.DefaultClient,
	}
}
