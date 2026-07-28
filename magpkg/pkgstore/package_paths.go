package pkgstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

const (
	packageDirectoryName  = "packages"
	packageArtifactSuffix = ".tar.zst"
	packageLockSuffix     = ".lock"
	packageWorkSuffix     = ".workdir"
)

func validateStore(store *Store) error {
	if store == nil || store.path == "" {
		return errors.New("pkgstore: store is not initialized")
	}
	return nil
}

// packageKey is the shared `[name-]id` base of a package's artifact, lock,
// and workspace names. The name is part of the definition hashed into the
// package ID, so the key is unique per package.
func packageKey(packageID pkggraph.ID, packageName string) string {
	key := string(packageID)
	if packageName != "" {
		key = packageName + "-" + key
	}
	return key
}

func packageArtifactFilename(packageID pkggraph.ID, packageName string) string {
	return packageKey(packageID, packageName) + packageArtifactSuffix
}

func (s *Store) packageDirectory() string {
	return filepath.Join(s.path, packageDirectoryName)
}

func (s *Store) packageArtifactPath(
	packageID pkggraph.ID,
	packageName string,
) string {
	return filepath.Join(
		s.packageDirectory(),
		packageArtifactFilename(packageID, packageName),
	)
}

func (s *Store) packageLockPath(
	packageID pkggraph.ID,
	packageName string,
) string {
	return filepath.Join(
		s.packageDirectory(),
		packageKey(packageID, packageName)+packageLockSuffix,
	)
}

func (s *Store) packageWorkPath(
	packageID pkggraph.ID,
	packageName string,
) string {
	return filepath.Join(
		s.packageDirectory(),
		packageKey(packageID, packageName)+packageWorkSuffix,
	)
}

func (s *Store) packageAvailable(
	packageID pkggraph.ID,
	packageName string,
) (bool, error) {
	return regularPackageArtifact(s.packageArtifactPath(packageID, packageName))
}

func (s *Store) packageAvailableForUse(
	ctx context.Context,
	packageID pkggraph.ID,
	packageName string,
) (available bool, retErr error) {
	artifactPath := s.packageArtifactPath(packageID, packageName)
	available, err := regularPackageArtifact(artifactPath)
	if err != nil || !available {
		return available, err
	}
	if err := os.MkdirAll(s.packageDirectory(), 0o755); err != nil {
		return false, fmt.Errorf("create package directory: %w", err)
	}
	lock, err := acquireSharedLock(ctx, s.packageLockPath(packageID, packageName))
	if err != nil {
		return false, err
	}
	defer func() {
		retErr = errors.Join(retErr, lock.Close())
	}()

	available, err = regularPackageArtifact(artifactPath)
	if err != nil || !available {
		return available, err
	}
	if err := touchFile(artifactPath); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) usePackageArtifact(
	ctx context.Context,
	packageID pkggraph.ID,
	packageName string,
	use func(string) error,
) (retErr error) {
	if err := os.MkdirAll(s.packageDirectory(), 0o755); err != nil {
		return fmt.Errorf("create package directory: %w", err)
	}
	lock, err := acquireSharedLock(ctx, s.packageLockPath(packageID, packageName))
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, lock.Close())
	}()

	artifactPath := s.packageArtifactPath(packageID, packageName)
	available, err := regularPackageArtifact(artifactPath)
	if err != nil {
		return err
	}
	if !available {
		return fmt.Errorf("package %s has no package artifact", packageID)
	}
	if err := touchFile(artifactPath); err != nil {
		return err
	}
	return use(artifactPath)
}

func regularPackageArtifact(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("package artifact %q is not a regular file", path)
	}
	return true, nil
}
