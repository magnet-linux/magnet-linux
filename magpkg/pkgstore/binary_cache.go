package pkgstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

const binaryCacheProtocolPath = "v1/packages"

type binaryCacheClient struct {
	baseURL *url.URL
	client  *http.Client
}

func newBinaryCacheClient(
	rawURL string,
	client *http.Client,
) (*binaryCacheClient, error) {
	if rawURL == "" {
		return nil, nil
	}
	baseURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("pkgstore: parse binary cache URL: %w", err)
	}
	switch strings.ToLower(baseURL.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf(
			"pkgstore: binary cache URL has unsupported scheme %q",
			baseURL.Scheme,
		)
	}
	if baseURL.Host == "" {
		return nil, errors.New("pkgstore: binary cache URL has no host")
	}
	if baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New(
			"pkgstore: binary cache URL must not contain a query or fragment",
		)
	}
	if client == nil {
		client = environmentIndependentHTTPClient()
	}
	return &binaryCacheClient{
		baseURL: baseURL,
		client:  client,
	}, nil
}

func (cache *binaryCacheClient) artifactURL(
	packageID pkggraph.ID,
	packageName string,
) (*url.URL, error) {
	filename := packageArtifactFilename(packageID, packageName)
	joined, err := url.JoinPath(
		cache.baseURL.String(),
		binaryCacheProtocolPath,
		filename,
	)
	if err != nil {
		return nil, err
	}
	return url.Parse(joined)
}

// fetchPackageFromBinaryCache downloads and atomically publishes packageID.
// The caller need not hold the package lock.
func (s *Store) fetchPackageFromBinaryCache(
	ctx context.Context,
	packageID pkggraph.ID,
	packageName string,
) (found bool, retErr error) {
	if s.binaryCache == nil {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	for _, directory := range []string{
		filepath.Join(s.path, packageDirectoryName),
		filepath.Join(s.path, buildLockDirectory),
		filepath.Join(s.path, buildWorkDirectory),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return false, fmt.Errorf(
				"create binary-cache store directory: %w",
				err,
			)
		}
	}

	lock, err := acquireLock(ctx, s.packageLockPath(packageID))
	if err != nil {
		return false, fmt.Errorf("lock package %s for binary cache: %w", packageID, err)
	}
	defer func() {
		retErr = errors.Join(retErr, lock.Close())
	}()

	available, err := s.packageAvailable(packageID, packageName)
	if err != nil {
		return false, err
	}
	if available {
		if err := touchFile(s.packageArtifactPath(packageID, packageName)); err != nil {
			return false, err
		}
		return true, nil
	}

	artifactURL, err := s.binaryCache.artifactURL(packageID, packageName)
	if err != nil {
		return false, fmt.Errorf("construct binary cache URL: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		artifactURL.String(),
		nil,
	)
	if err != nil {
		return false, fmt.Errorf("create binary cache request: %w", err)
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := s.binaryCache.client.Do(request)
	if err != nil {
		return false, fmt.Errorf(
			"fetch package %s from binary cache %s: %w",
			packageID,
			s.binaryCache.baseURL.Redacted(),
			err,
		)
	}
	defer func() {
		retErr = errors.Join(retErr, response.Body.Close())
	}()

	switch response.StatusCode {
	case http.StatusNotFound:
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return false, nil
	case http.StatusOK:
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return false, fmt.Errorf(
			"binary cache %s returned %s for package %s",
			s.binaryCache.baseURL.Redacted(),
			response.Status,
			packageID,
		)
	}

	workRoot := s.packageWorkPath(packageID)
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		return false, fmt.Errorf("create binary-cache workspace: %w", err)
	}
	temporary, err := os.CreateTemp(workRoot, "binary-cache-*.tmp")
	if err != nil {
		return false, fmt.Errorf("create binary-cache temporary artifact: %w", err)
	}
	temporaryPath := temporary.Name()
	temporaryOpen := true
	defer func() {
		if temporaryOpen {
			retErr = errors.Join(retErr, temporary.Close())
		}
		if err := os.Remove(temporaryPath); retErr == nil &&
			err != nil && !errors.Is(err, os.ErrNotExist) {
			retErr = fmt.Errorf("remove binary-cache temporary artifact: %w", err)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return false, err
	}
	if _, err := copyContext(ctx, temporary, response.Body); err != nil {
		return false, fmt.Errorf("download binary-cache artifact: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return false, fmt.Errorf("sync binary-cache artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return false, fmt.Errorf("close binary-cache artifact: %w", err)
	}
	temporaryOpen = false

	if err := validatePackageArchive(ctx, temporaryPath); err != nil {
		return false, fmt.Errorf(
			"validate binary-cache artifact for package %s: %w",
			packageID,
			err,
		)
	}
	finalPath := s.packageArtifactPath(packageID, packageName)
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return false, fmt.Errorf("publish binary-cache artifact: %w", err)
	}
	if err := syncDirectory(filepath.Dir(finalPath)); err != nil {
		return false, fmt.Errorf("sync binary-cache package directory: %w", err)
	}
	if err := os.RemoveAll(workRoot); err != nil {
		return false, fmt.Errorf("remove binary-cache workspace: %w", err)
	}
	return true, nil
}
