package pkgstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	storeMetadataFilename = ".magpkg-store.json"
	storeFormat           = "magpkg-store"
	storeFormatVersion    = 1
)

type storeMetadata struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
}

// OpenOptions controls optional integrations used by a package store.
type OpenOptions struct {
	// BinaryCacheURL is the base URL of a trusted magpkg binary cache.
	// An empty value disables binary-cache substitution.
	BinaryCacheURL string

	// HTTPClient performs source-fetch and binary-cache requests. Nil uses a
	// client that does not consult proxy environment variables.
	HTTPClient *http.Client
}

// Store is a package store rooted at path.
type Store struct {
	path        string
	httpClient  *http.Client
	binaryCache *binaryCacheClient
}

// Init creates or validates a versioned package store at store.
func Init(store string) error {
	path, err := storePath(store)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	if err := initializeStoreMetadata(path); err != nil {
		return fmt.Errorf("initialize package store metadata: %w", err)
	}
	return nil
}

// Open opens a directory containing supported package-store metadata.
func Open(store string) (*Store, error) {
	return OpenWithOptions(store, OpenOptions{})
}

// OpenWithOptions opens a package store with optional integrations. The binary
// cache is contacted lazily when a build needs a remote artifact.
func OpenWithOptions(store string, options OpenOptions) (*Store, error) {
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
	if _, err := readStoreMetadata(path); err != nil {
		return nil, fmt.Errorf("pkgstore: open store metadata: %w", err)
	}

	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = environmentIndependentHTTPClient()
	}
	cache, err := newBinaryCacheClient(options.BinaryCacheURL, httpClient)
	if err != nil {
		return nil, err
	}
	return &Store{
		path:        path,
		httpClient:  httpClient,
		binaryCache: cache,
	}, nil
}

func environmentIndependentHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Client{Transport: &http.Transport{
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}}
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

func initializeStoreMetadata(store string) (retErr error) {
	if _, err := readStoreMetadata(store); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	metadata := storeMetadata{
		Format:  storeFormat,
		Version: storeFormatVersion,
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	temporary, err := os.CreateTemp(store, ".magpkg-store-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	temporaryOpen := true
	defer func() {
		if temporaryOpen {
			retErr = errors.Join(retErr, temporary.Close())
		}
		retErr = errors.Join(retErr, removeIfExists(temporaryPath))
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
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

	metadataPath := filepath.Join(store, storeMetadataFilename)
	if err := os.Link(temporaryPath, metadataPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			_, validateErr := readStoreMetadata(store)
			return validateErr
		}
		return err
	}
	if err := syncDirectory(store); err != nil {
		return err
	}
	return nil
}

func readStoreMetadata(store string) (storeMetadata, error) {
	path := filepath.Join(store, storeMetadataFilename)
	info, err := os.Lstat(path)
	if err != nil {
		return storeMetadata{}, err
	}
	if !info.Mode().IsRegular() {
		return storeMetadata{}, errors.New("metadata is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return storeMetadata{}, err
	}
	var metadata storeMetadata
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return storeMetadata{}, fmt.Errorf("decode metadata: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected trailing JSON value")
		}
		return storeMetadata{}, fmt.Errorf("decode metadata: %w", err)
	}
	if metadata.Format != storeFormat {
		return storeMetadata{}, fmt.Errorf(
			"unexpected store format %q",
			metadata.Format,
		)
	}
	if metadata.Version != storeFormatVersion {
		return storeMetadata{}, fmt.Errorf(
			"unsupported store format version %d",
			metadata.Version,
		)
	}
	return metadata, nil
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
