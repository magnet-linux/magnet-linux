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

	"golang.org/x/sys/unix"
)

const (
	fetchDirectoryName = "fetch"
	fetchLockSuffix    = ".lock"
	fetchTempSuffix    = ".tmp"
	fetchLockPoll      = 25 * time.Millisecond
)

// Fetch downloads source into the store's content-addressed fetch cache and
// returns the absolute path to the verified cached file.
//
// The URLs are attempted in order. Fetch currently supports file, http, and
// https URLs. The SHA-256 digest is always verified before a file is returned.
func (s *Store) Fetch(ctx context.Context, fetch Fetch) (path string, retErr error) {
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
	lock, err := acquireFetchLock(ctx, lockPath)
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

	valid, err := verifyFileSHA256(ctx, destination, expectedSHA256)
	switch {
	case err == nil && valid:
		if err := touchFile(destination); err != nil {
			return "", fmt.Errorf("touch cached fetch %q: %w", fetch.Filename, err)
		}
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

		err := s.fetchURL(ctx, rawURL, destination, expectedSHA256)
		if err == nil {
			if err := touchFile(destination); err != nil {
				return "", fmt.Errorf("touch fetched source %q: %w", fetch.Filename, err)
			}
			return destination, nil
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
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
	switch scheme {
	case "file":
		err = fetchFileURL(ctx, parsed, writer)
	case "http", "https":
		err = fetchHTTPURL(ctx, parsed, writer)
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

func fetchFileURL(ctx context.Context, source *url.URL, destination io.Writer) error {
	path, err := fileURLPath(source)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file URL: %w", err)
	}
	defer file.Close()

	if _, err := copyContext(ctx, destination, file); err != nil {
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

func fetchHTTPURL(ctx context.Context, source *url.URL, destination io.Writer) error {
	if source.Host == "" {
		return errors.New("HTTP URL has no host")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source.String(), nil)
	if err != nil {
		return fmt.Errorf("create HTTP request: %w", err)
	}
	// Fetch and verify the bytes as served, without transparent compression.
	request.Header.Set("Accept-Encoding", "identity")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("download: HTTP %s", response.Status)
	}
	if _, err := copyContext(ctx, destination, response.Body); err != nil {
		return fmt.Errorf("download response: %w", err)
	}
	return nil
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
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
) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := copyContext(ctx, hasher, file); err != nil {
		return false, err
	}
	return bytes.Equal(hasher.Sum(nil), expected[:]), nil
}

type fetchLock struct {
	file *os.File
	path string
}

func acquireFetchLock(ctx context.Context, path string) (*fetchLock, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, err
		}

		for {
			err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
			switch {
			case err == nil:
			case errors.Is(err, unix.EINTR):
				continue
			case errors.Is(err, unix.EWOULDBLOCK), errors.Is(err, unix.EAGAIN):
				timer := time.NewTimer(fetchLockPoll)
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						<-timer.C
					}
					_ = file.Close()
					return nil, ctx.Err()
				case <-timer.C:
				}
				continue
			default:
				_ = file.Close()
				return nil, err
			}
			break
		}

		// Cleanup may unlink the pathname while a fetcher is waiting on the
		// old inode. Only proceed if the inode we locked is still current.
		current, err := fetchLockFileIsCurrent(file, path)
		if err != nil {
			return nil, errors.Join(err, unlockAndCloseFetchLockFile(file))
		}
		if current {
			return &fetchLock{file: file, path: path}, nil
		}
		if err := unlockAndCloseFetchLockFile(file); err != nil {
			return nil, err
		}
	}
}

func tryAcquireFetchLock(
	ctx context.Context,
	path string,
) (*fetchLock, bool, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, false, err
		}

		for {
			err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			break
		}
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			_ = file.Close()
			return nil, true, nil
		}
		if err != nil {
			_ = file.Close()
			return nil, false, err
		}

		current, err := fetchLockFileIsCurrent(file, path)
		if err != nil {
			return nil, false, errors.Join(err, unlockAndCloseFetchLockFile(file))
		}
		if current {
			return &fetchLock{file: file, path: path}, false, nil
		}
		if err := unlockAndCloseFetchLockFile(file); err != nil {
			return nil, false, err
		}
	}
}

func fetchLockFileIsCurrent(file *os.File, path string) (bool, error) {
	lockedInfo, err := file.Stat()
	if err != nil {
		return false, err
	}
	pathInfo, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(lockedInfo, pathInfo), nil
}

func unlockAndCloseFetchLockFile(file *os.File) error {
	unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN)
	closeErr := file.Close()
	return errors.Join(unlockErr, closeErr)
}

func (lock *fetchLock) Unlink() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	current, err := fetchLockFileIsCurrent(lock.file, lock.path)
	if err != nil {
		return err
	}
	if !current {
		if _, err := os.Stat(lock.path); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		return errors.New("fetch lock path changed while locked")
	}
	if err := os.Remove(lock.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (lock *fetchLock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	err := unlockAndCloseFetchLockFile(lock.file)
	lock.file = nil
	return err
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
