package shellenv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/andrewchambers/magnet-linux/magpkg/internal/filelock"
	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
)

const (
	rootMetadataFilename = ".magpkg-environments.json"
	rootFormat           = "magpkg-environments"
	rootFormatVersion    = 1

	environmentMetadataFilename = "metadata.json"
	environmentFormat           = "magpkg-environment"
	environmentFormatVersion    = 1

	lockSuffix = ".lock"
	workSuffix = ".workdir"

	environmentIdentityDomain = "magpkg-environment-v1\x00"

	// LauncherPath is the path, relative to an environment root, of the
	// package-provided host launcher used by Run.
	LauncherPath = "libexec/magpkg-shell"
)

type rootMetadata struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
}

type environmentMetadata struct {
	Format   string        `json:"format"`
	Version  int           `json:"version"`
	ID       string        `json:"id"`
	Packages []pkggraph.ID `json:"packages"`
}

// ErrNotFound indicates that no valid cached environment exists for a
// requested runtime closure.
var ErrNotFound = errors.New("shellenv: environment is not cached")

// Environment is a materialized runtime closure protected from cleanup by a
// shared lease. Call Close after the environment's last child process exits.
type Environment struct {
	ID       string
	RootPath string
	Packages []pkggraph.ID

	metadataPath string
	lock         *filelock.Lock
	mutex        sync.Mutex
	closed       bool
}

// Open returns a lease for an already-materialized environment. It returns
// ErrNotFound when the environments directory or matching valid environment
// does not exist.
func Open(
	ctx context.Context,
	environmentsPath string,
	graph *pkggraph.Graph,
	roots []pkggraph.ID,
) (*Environment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if environmentsPath == "" {
		return nil, errors.New("shellenv: environments path is empty")
	}
	if graph == nil {
		return nil, errors.New("shellenv: package graph is nil")
	}
	closure, err := graph.RuntimeClosure(roots...)
	if err != nil {
		return nil, err
	}
	absolutePath, err := filepath.Abs(environmentsPath)
	if err != nil {
		return nil, fmt.Errorf("shellenv: resolve environments path: %w", err)
	}
	if _, err := os.Lstat(absolutePath); errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if err := validateRoot(absolutePath); err != nil {
		return nil, fmt.Errorf("shellenv: open environments metadata: %w", err)
	}
	return openEnvironment(
		ctx,
		absolutePath,
		identityForClosure(closure),
		closure,
	)
}

// Acquire returns a leased environment containing roots' runtime closure.
// Missing environments are composed from artifacts already in store and
// published atomically. Acquire does not build or fetch missing packages.
func Acquire(
	ctx context.Context,
	environmentsPath string,
	store *pkgstore.Store,
	graph *pkggraph.Graph,
	roots []pkggraph.ID,
) (*Environment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if environmentsPath == "" {
		return nil, errors.New("shellenv: environments path is empty")
	}
	if store == nil || store.Path() == "" {
		return nil, errors.New("shellenv: package store is nil")
	}
	if graph == nil {
		return nil, errors.New("shellenv: package graph is nil")
	}
	closure, err := graph.RuntimeClosure(roots...)
	if err != nil {
		return nil, err
	}
	absolutePath, err := filepath.Abs(environmentsPath)
	if err != nil {
		return nil, fmt.Errorf("shellenv: resolve environments path: %w", err)
	}
	if err := initializeRoot(absolutePath); err != nil {
		return nil, fmt.Errorf("shellenv: initialize environments: %w", err)
	}

	environmentID := identityForClosure(closure)
	lockPath := environmentLockPath(absolutePath, environmentID)
	for {
		environment, err := openEnvironment(
			ctx,
			absolutePath,
			environmentID,
			closure,
		)
		if err == nil {
			return environment, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}

		exclusive, err := filelock.AcquireExclusive(ctx, lockPath)
		if err != nil {
			return nil, fmt.Errorf(
				"shellenv: lock environment for materialization: %w",
				err,
			)
		}
		valid, inspectErr := validateEnvironment(
			absolutePath,
			environmentID,
			closure,
		)
		if inspectErr != nil || !valid {
			materializeErr := materializeEnvironment(
				ctx,
				absolutePath,
				environmentID,
				store,
				graph,
				closure,
			)
			if err := errors.Join(materializeErr, exclusive.Close()); err != nil {
				return nil, err
			}
		} else if err := exclusive.Close(); err != nil {
			return nil, fmt.Errorf(
				"shellenv: release materialization lock: %w",
				err,
			)
		}
	}
}

func openEnvironment(
	ctx context.Context,
	environmentsPath string,
	environmentID string,
	closure []pkggraph.ID,
) (*Environment, error) {
	shared, err := filelock.AcquireShared(
		ctx,
		environmentLockPath(environmentsPath, environmentID),
	)
	if err != nil {
		return nil, fmt.Errorf("shellenv: lease environment: %w", err)
	}
	valid, inspectErr := validateEnvironment(
		environmentsPath,
		environmentID,
		closure,
	)
	if inspectErr != nil || !valid {
		return nil, errors.Join(ErrNotFound, shared.Close())
	}
	metadataPath := environmentMetadataPath(environmentsPath, environmentID)
	if err := touch(metadataPath); err != nil {
		return nil, errors.Join(
			fmt.Errorf("shellenv: touch environment: %w", err),
			shared.Close(),
		)
	}
	return &Environment{
		ID:           environmentID,
		RootPath:     environmentRootPath(environmentsPath, environmentID),
		Packages:     slices.Clone(closure),
		metadataPath: metadataPath,
		lock:         shared,
	}, nil
}

// Close refreshes the environment's last-use time and releases its lease.
func (environment *Environment) Close() error {
	if environment == nil {
		return nil
	}
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	if environment.closed {
		return nil
	}
	environment.closed = true
	touchErr := touch(environment.metadataPath)
	closeErr := environment.lock.Close()
	environment.lock = nil
	return errors.Join(touchErr, closeErr)
}

func (environment *Environment) isOpen() bool {
	if environment == nil {
		return false
	}
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return !environment.closed && environment.lock != nil
}

func identityForClosure(closure []pkggraph.ID) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(environmentIdentityDomain))
	for _, packageID := range closure {
		_, _ = hasher.Write([]byte(packageID))
		_, _ = hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func materializeEnvironment(
	ctx context.Context,
	environmentsPath string,
	environmentID string,
	store *pkgstore.Store,
	graph *pkggraph.Graph,
	closure []pkggraph.ID,
) (retErr error) {
	finalPath := environmentPath(environmentsPath, environmentID)
	workPath := environmentWorkPath(environmentsPath, environmentID)
	if err := os.RemoveAll(finalPath); err != nil {
		return fmt.Errorf("shellenv: remove invalid environment: %w", err)
	}
	if err := os.RemoveAll(workPath); err != nil {
		return fmt.Errorf("shellenv: remove previous environment workspace: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(workPath); err != nil {
			retErr = errors.Join(
				retErr,
				fmt.Errorf("shellenv: remove environment workspace: %w", err),
			)
		}
	}()

	rootPath := filepath.Join(workPath, "root")
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		return fmt.Errorf("shellenv: create environment workspace: %w", err)
	}
	if err := store.ExtractPackages(ctx, graph, closure, rootPath); err != nil {
		return fmt.Errorf("shellenv: compose environment: %w", err)
	}
	metadata := environmentMetadata{
		Format:   environmentFormat,
		Version:  environmentFormatVersion,
		ID:       environmentID,
		Packages: slices.Clone(closure),
	}
	if err := writeJSONFile(
		filepath.Join(workPath, environmentMetadataFilename),
		metadata,
	); err != nil {
		return fmt.Errorf("shellenv: write environment metadata: %w", err)
	}
	if err := os.Rename(workPath, finalPath); err != nil {
		return fmt.Errorf("shellenv: publish environment: %w", err)
	}
	if err := syncDirectory(filepath.Dir(finalPath)); err != nil {
		return fmt.Errorf("shellenv: sync environment directory: %w", err)
	}
	return nil
}

func validateEnvironment(
	environmentsPath string,
	environmentID string,
	closure []pkggraph.ID,
) (bool, error) {
	finalPath := environmentPath(environmentsPath, environmentID)
	info, err := os.Lstat(finalPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, errors.New("environment path is not a directory")
	}
	rootInfo, err := os.Lstat(filepath.Join(finalPath, "root"))
	if err != nil {
		return false, err
	}
	if !rootInfo.IsDir() {
		return false, errors.New("environment root is not a directory")
	}
	var metadata environmentMetadata
	if err := readJSONFile(
		filepath.Join(finalPath, environmentMetadataFilename),
		&metadata,
	); err != nil {
		return false, err
	}
	if metadata.Format != environmentFormat ||
		metadata.Version != environmentFormatVersion ||
		metadata.ID != environmentID ||
		!slices.Equal(metadata.Packages, closure) {
		return false, errors.New("environment metadata does not match its identity")
	}
	return true, nil
}

func initializeRoot(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	metadataPath := filepath.Join(path, rootMetadataFilename)
	if err := validateRoot(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	metadata := rootMetadata{
		Format:  rootFormat,
		Version: rootFormatVersion,
	}
	temporary, err := os.CreateTemp(path, ".magpkg-environments-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	temporaryOpen := true
	defer func() {
		if temporaryOpen {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	temporaryOpen = false
	if err := os.Link(temporaryPath, metadataPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return validateRoot(path)
		}
		return err
	}
	return syncDirectory(path)
}

func validateRoot(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("environments path is not a directory")
	}
	var metadata rootMetadata
	if err := readJSONFile(
		filepath.Join(path, rootMetadataFilename),
		&metadata,
	); err != nil {
		return err
	}
	if metadata.Format != rootFormat {
		return fmt.Errorf(
			"unexpected environments format %q",
			metadata.Format,
		)
	}
	if metadata.Version != rootFormatVersion {
		return fmt.Errorf(
			"unsupported environments format version %d",
			metadata.Version,
		)
	}
	return nil
}

func readJSONFile(path string, destination any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("metadata is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode metadata: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected trailing JSON value")
		}
		return fmt.Errorf("decode metadata: %w", err)
	}
	return nil
}

func writeJSONFile(path string, value any) (retErr error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, file.Close())
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func environmentPath(environmentsPath string, environmentID string) string {
	return filepath.Join(environmentsPath, environmentID)
}

func environmentRootPath(environmentsPath string, environmentID string) string {
	return filepath.Join(environmentPath(environmentsPath, environmentID), "root")
}

func environmentMetadataPath(
	environmentsPath string,
	environmentID string,
) string {
	return filepath.Join(
		environmentPath(environmentsPath, environmentID),
		environmentMetadataFilename,
	)
}

func environmentLockPath(environmentsPath string, environmentID string) string {
	return filepath.Join(environmentsPath, environmentID+lockSuffix)
}

func environmentWorkPath(environmentsPath string, environmentID string) string {
	return filepath.Join(environmentsPath, environmentID+workSuffix)
}

func touch(path string) error {
	now := time.Now()
	return os.Chtimes(path, now, now)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
