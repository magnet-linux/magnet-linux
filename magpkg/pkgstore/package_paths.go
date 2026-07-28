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
	buildLockDirectory    = "locks"
	buildWorkDirectory    = "work"
	packageArtifactSuffix = ".tar.zst"
	buildLockSuffix       = ".lock"
)

func validateStore(store *Store) error {
	if store == nil || store.path == "" {
		return errors.New("pkgstore: store is not initialized")
	}
	return nil
}

func packageArtifactFilename(packageID pkggraph.ID, packageName string) string {
	filename := string(packageID) + packageArtifactSuffix
	if packageName != "" {
		filename = packageName + "-" + filename
	}
	return filename
}

func (s *Store) packageArtifactPath(
	packageID pkggraph.ID,
	packageName string,
) string {
	return filepath.Join(
		s.path,
		packageDirectoryName,
		packageArtifactFilename(packageID, packageName),
	)
}

func (s *Store) packageLockPath(packageID pkggraph.ID) string {
	return filepath.Join(
		s.path,
		buildLockDirectory,
		string(packageID)+buildLockSuffix,
	)
}

func (s *Store) packageWorkPath(packageID pkggraph.ID) string {
	return filepath.Join(s.path, buildWorkDirectory, string(packageID))
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
	if err := os.MkdirAll(
		filepath.Join(s.path, buildLockDirectory),
		0o755,
	); err != nil {
		return false, fmt.Errorf("create package lock directory: %w", err)
	}
	lock, err := acquireSharedLock(ctx, s.packageLockPath(packageID))
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
	if err := os.MkdirAll(
		filepath.Join(s.path, buildLockDirectory),
		0o755,
	); err != nil {
		return fmt.Errorf("create package lock directory: %w", err)
	}
	lock, err := acquireSharedLock(ctx, s.packageLockPath(packageID))
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
