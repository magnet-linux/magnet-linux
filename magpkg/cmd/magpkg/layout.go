package main

import (
	"fmt"
	"os"
	"path/filepath"
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
