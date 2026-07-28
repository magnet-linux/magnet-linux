package pkgstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

const (
	fetchDirectoryName = "fetch"
	fetchLockSuffix    = ".lock"
	fetchTempSuffix    = ".tmp"
)

// FetchProgressPhase describes the current state of one source fetch.
type FetchProgressPhase string

const (
	FetchProgressCached      FetchProgressPhase = "cached"
	FetchProgressDownloading FetchProgressPhase = "downloading"
	FetchProgressURLFailed   FetchProgressPhase = "url-failed"
	FetchProgressComplete    FetchProgressPhase = "complete"
)

// FetchProgress is a point-in-time update for one source. TotalBytes is -1
// when the source size is unknown.
type FetchProgress struct {
	Filename string
	// SHA256 is the normalized lowercase digest identifying the fetch.
	SHA256         string
	URL            string
	Phase          FetchProgressPhase
	CompletedBytes int64
	TotalBytes     int64
	Err            error
}

// FetchProgressFunc receives fetch progress updates. Callbacks must be safe for
// concurrent use and should return quickly.
type FetchProgressFunc func(FetchProgress)

// FetchOptions controls a single source fetch.
type FetchOptions struct {
	// Progress receives source-fetch progress updates.
	Progress FetchProgressFunc
}

// Fetch downloads source into the store's content-addressed fetch cache and
// returns the absolute path to the verified cached file.
//
// The URLs are attempted in order. Fetch currently supports file, http, and
// https URLs. The SHA-256 digest is always verified before a file is returned.
func (s *Store) Fetch(ctx context.Context, fetch pkggraph.Fetch) (path string, retErr error) {
	return s.FetchWithOptions(ctx, fetch, FetchOptions{})
}

// FetchWithOptions is Fetch with progress reporting.
func (s *Store) FetchWithOptions(
	ctx context.Context,
	fetch pkggraph.Fetch,
	options FetchOptions,
) (path string, retErr error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s == nil || s.path == "" {
		return "", errors.New("pkgstore: store is not initialized")
	}
	if fetch.Filename == "" {
		return "", errors.New("pkgstore: fetch filename is empty")
	}

	expectedSHA256, digest, err := parseFetchSHA256(fetch.SHA256)
	if err != nil {
		return "", err
	}

	fetchRoot := filepath.Join(s.path, fetchDirectoryName)
	if err := os.MkdirAll(fetchRoot, 0o755); err != nil {
		return "", fmt.Errorf("create fetch cache: %w", err)
	}

	destination := filepath.Join(fetchRoot, digest)
	lockPath := destination + fetchLockSuffix
	lock, err := acquireLock(ctx, lockPath)
	if err != nil {
		return "", fmt.Errorf("lock fetch %q: %w", fetch.Filename, err)
	}
	defer func() {
		if err := touchFile(lockPath); retErr == nil && err != nil {
			retErr = fmt.Errorf("touch fetch lock: %w", err)
		}
		if err := lock.Close(); retErr == nil && err != nil {
			retErr = fmt.Errorf("unlock fetch %q: %w", fetch.Filename, err)
		}
	}()

	if err := ctx.Err(); err != nil {
		return "", err
	}

	valid, verifiedBytes, err := verifyFileSHA256(
		ctx,
		destination,
		expectedSHA256,
	)
	switch {
	case err == nil && valid:
		if err := touchFile(destination); err != nil {
			return "", fmt.Errorf("touch cached fetch %q: %w", fetch.Filename, err)
		}
		reportFetchProgress(options.Progress, FetchProgress{
			Filename:       fetch.Filename,
			SHA256:         digest,
			Phase:          FetchProgressCached,
			CompletedBytes: verifiedBytes,
			TotalBytes:     verifiedBytes,
		})
		return destination, nil
	case err == nil:
		if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("remove invalid cached fetch %q: %w", fetch.Filename, err)
		}
	case errors.Is(err, os.ErrNotExist):
		// The source is not cached yet.
	default:
		return "", fmt.Errorf("verify cached fetch %q: %w", fetch.Filename, err)
	}

	if len(fetch.URLs) == 0 {
		return "", fmt.Errorf("pkgstore: no URLs provided for fetch %q", fetch.Filename)
	}

	urlErrors := make([]error, 0, len(fetch.URLs))
	for _, rawURL := range fetch.URLs {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		err := s.fetchURL(
			ctx,
			rawURL,
			destination,
			expectedSHA256,
			fetch.Filename,
			digest,
			options.Progress,
		)
		if err == nil {
			if err := touchFile(destination); err != nil {
				return "", fmt.Errorf("touch fetched source %q: %w", fetch.Filename, err)
			}
			info, statErr := os.Stat(destination)
			var size int64 = -1
			if statErr == nil {
				size = info.Size()
			}
			reportFetchProgress(options.Progress, FetchProgress{
				Filename:       fetch.Filename,
				SHA256:         digest,
				URL:            redactedFetchURL(rawURL),
				Phase:          FetchProgressComplete,
				CompletedBytes: size,
				TotalBytes:     size,
			})
			return destination, nil
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		reportFetchProgress(options.Progress, FetchProgress{
			Filename:   fetch.Filename,
			SHA256:     digest,
			URL:        redactedFetchURL(rawURL),
			Phase:      FetchProgressURLFailed,
			TotalBytes: -1,
			Err:        err,
		})
		urlErrors = append(urlErrors, fmt.Errorf("%q: %w", rawURL, err))
	}

	return "", fmt.Errorf(
		"fetch %q: all URLs failed: %w",
		fetch.Filename,
		errors.Join(urlErrors...),
	)
}

func parseFetchSHA256(value string) ([sha256.Size]byte, string, error) {
	var expected [sha256.Size]byte
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return expected, "", fmt.Errorf(
			"pkgstore: fetch SHA-256 must be a %d-character hexadecimal digest",
			sha256.Size*2,
		)
	}
	copy(expected[:], decoded)
	return expected, hex.EncodeToString(expected[:]), nil
}

func (s *Store) fetchURL(
	ctx context.Context,
	rawURL string,
	destination string,
	expectedSHA256 [sha256.Size]byte,
	filename string,
	digest string,
	progress FetchProgressFunc,
) (retErr error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "file", "http", "https":
	case "":
		return errors.New("fetch URL has no scheme")
	default:
		return fmt.Errorf("unsupported fetch URL scheme %q", parsed.Scheme)
	}

	tempPath := destination + fetchTempSuffix
	if err := os.Remove(tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale partial fetch: %w", err)
	}
	temp, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create partial fetch: %w", err)
	}
	tempOpen := true
	defer func() {
		if tempOpen {
			if err := temp.Close(); retErr == nil && err != nil {
				retErr = fmt.Errorf("close partial fetch: %w", err)
			}
		}
		if err := os.Remove(tempPath); retErr == nil &&
			err != nil && !errors.Is(err, os.ErrNotExist) {
			retErr = fmt.Errorf("remove partial fetch: %w", err)
		}
	}()

	hasher := sha256.New()
	writer := io.MultiWriter(temp, hasher)
	reportTransfer := func(completed int64, total int64) {
		reportFetchProgress(progress, FetchProgress{
			Filename:       filename,
			SHA256:         digest,
			URL:            parsed.Redacted(),
			Phase:          FetchProgressDownloading,
			CompletedBytes: completed,
			TotalBytes:     total,
		})
	}
	switch scheme {
	case "file":
		err = fetchFileURL(ctx, parsed, writer, reportTransfer)
	case "http", "https":
		err = fetchHTTPURL(ctx, s.httpClient, parsed, writer, reportTransfer)
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync partial fetch: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close partial fetch: %w", err)
	}
	tempOpen = false

	if !bytes.Equal(hasher.Sum(nil), expectedSHA256[:]) {
		return fmt.Errorf(
			"SHA-256 mismatch: got %s, want %s",
			hex.EncodeToString(hasher.Sum(nil)),
			hex.EncodeToString(expectedSHA256[:]),
		)
	}

	if err := os.Rename(tempPath, destination); err != nil {
		return fmt.Errorf("publish fetched source: %w", err)
	}
	if err := syncDirectory(filepath.Dir(destination)); err != nil {
		return fmt.Errorf("sync fetch cache: %w", err)
	}
	return nil
}

func fetchFileURL(
	ctx context.Context,
	source *url.URL,
	destination io.Writer,
	progress func(int64, int64),
) error {
	path, err := fileURLPath(source)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file URL: %w", err)
	}
	defer file.Close()

	total := int64(-1)
	if info, err := file.Stat(); err == nil && info.Mode().IsRegular() {
		total = info.Size()
	}
	if progress != nil {
		progress(0, total)
	}
	if _, err := copyContextWithProgress(
		ctx,
		destination,
		file,
		func(completed int64) {
			if progress != nil {
				progress(completed, total)
			}
		},
	); err != nil {
		return fmt.Errorf("read file URL: %w", err)
	}
	return nil
}

func fileURLPath(source *url.URL) (string, error) {
	if source.Scheme != "file" && !strings.EqualFold(source.Scheme, "file") {
		return "", fmt.Errorf("expected file URL, got %q", source.String())
	}
	if source.User != nil {
		return "", errors.New("file URL must not contain user information")
	}
	if source.Host != "" && !strings.EqualFold(source.Host, "localhost") {
		return "", fmt.Errorf("unsupported file URL host %q", source.Host)
	}
	if source.RawQuery != "" || source.Fragment != "" {
		return "", errors.New("file URL must not contain a query or fragment")
	}

	path := filepath.FromSlash(source.Path)
	if !filepath.IsAbs(path) {
		return "", errors.New("file URL must reference an absolute path")
	}
	return path, nil
}

func fetchHTTPURL(
	ctx context.Context,
	client *http.Client,
	source *url.URL,
	destination io.Writer,
	progress func(int64, int64),
) error {
	if client == nil {
		return errors.New("HTTP client is not configured")
	}
	if source.Host == "" {
		return errors.New("HTTP URL has no host")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.String(), nil)
	if err != nil {
		return fmt.Errorf("create HTTP request: %w", err)
	}
	// Fetch and verify the bytes as served, without transparent compression.
	request.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("download: HTTP %s", response.Status)
	}
	total := response.ContentLength
	if progress != nil {
		progress(0, total)
	}
	if _, err := copyContextWithProgress(
		ctx,
		destination,
		response.Body,
		func(completed int64) {
			if progress != nil {
				progress(completed, total)
			}
		},
	); err != nil {
		return fmt.Errorf("download response: %w", err)
	}
	return nil
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	return copyContextWithProgress(ctx, destination, source, nil)
}

func copyContextWithProgress(
	ctx context.Context,
	destination io.Writer,
	source io.Reader,
	progress func(int64),
) (int64, error) {
	buffer := make([]byte, 128*1024)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}

		read, readErr := source.Read(buffer)
		if read > 0 {
			count, writeErr := destination.Write(buffer[:read])
			written += int64(count)
			if writeErr != nil {
				return written, writeErr
			}
			if count != read {
				return written, io.ErrShortWrite
			}
			if progress != nil {
				progress(written)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return written, nil
			}
			return written, readErr
		}
	}
}

func verifyFileSHA256(
	ctx context.Context,
	path string,
	expected [sha256.Size]byte,
) (bool, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, 0, err
	}
	defer file.Close()

	hasher := sha256.New()
	size, err := copyContext(ctx, hasher, file)
	if err != nil {
		return false, size, err
	}
	return bytes.Equal(hasher.Sum(nil), expected[:]), size, nil
}

func reportFetchProgress(progress FetchProgressFunc, event FetchProgress) {
	if progress != nil {
		progress(event)
	}
}

func redactedFetchURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return parsed.Redacted()
}

func touchFile(path string) error {
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
