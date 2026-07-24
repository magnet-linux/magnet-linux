package pkgstore

import (
	"errors"
	"os"
	"path/filepath"
)

// Store is a package store rooted at path.
type Store struct {
	path string
}

// Init initializes a package store at store.
func Init(store string) error {
	path, err := storePath(store)
	if err != nil {
		return err
	}
	return os.MkdirAll(path, 0o755)
}

// Open opens the package store at store.
func Open(store string) (*Store, error) {
	path, err := storePath(store)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("pkgstore: store path is not a directory")
	}

	return &Store{path: path}, nil
}

// Path returns the absolute path to the root of the store.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func storePath(store string) (string, error) {
	if store == "" {
		return "", errors.New("pkgstore: store path is empty")
	}
	return filepath.Abs(store)
}
