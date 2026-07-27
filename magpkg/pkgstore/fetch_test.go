package pkgstore_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
	"golang.org/x/sys/unix"
)

func TestFetchFileURL(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store")
	if err := pkgstore.Init(storePath); err != nil {
		t.Fatal(err)
	}
	store, err := pkgstore.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}

	content := []byte("source from a local file\n")
	sourcePath := filepath.Join(t.TempDir(), "source file.tar.gz")
	if err := os.WriteFile(sourcePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256Hex(content)
	sourceURL := (&url.URL{Scheme: "file", Path: sourcePath}).String()

	cachedPath, err := store.Fetch(context.Background(), pkgstore.Fetch{
		Filename: "source.tar.gz",
		SHA256:   strings.ToUpper(digest),
		URLs:     []string{sourceURL},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(store.Path(), "fetch", digest)
	if cachedPath != wantPath {
		t.Fatalf("Fetch() path = %q, want %q", cachedPath, wantPath)
	}
	got, err := os.ReadFile(cachedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("cached content = %q, want %q", got, content)
	}

	// A valid cached file is usable even if the original source is gone.
	if err := os.Remove(sourcePath); err != nil {
		t.Fatal(err)
	}
	secondPath, err := store.Fetch(context.Background(), pkgstore.Fetch{
		Filename: "source.tar.gz",
		SHA256:   digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if secondPath != cachedPath {
		t.Fatalf("cached Fetch() path = %q, want %q", secondPath, cachedPath)
	}
}

func TestFetchProgressReportsTransferAndCacheHit(t *testing.T) {
	store := newTestStore(t)
	content := []byte(strings.Repeat("progress", 40*1024))
	sourcePath := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(sourcePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	source := pkgstore.Fetch{
		Filename: "source",
		SHA256:   sha256Hex(content),
		URLs: []string{(&url.URL{
			Scheme: "file",
			Path:   sourcePath,
		}).String()},
	}

	var first []pkgstore.FetchProgress
	if _, err := store.FetchWithOptions(
		context.Background(),
		source,
		pkgstore.FetchOptions{
			Progress: func(event pkgstore.FetchProgress) {
				first = append(first, event)
			},
		},
	); err != nil {
		t.Fatal(err)
	}
	assertFetchProgressPhase(
		t,
		first,
		pkgstore.FetchProgressDownloading,
		0,
		int64(len(content)),
	)
	assertFetchProgressPhase(
		t,
		first,
		pkgstore.FetchProgressComplete,
		int64(len(content)),
		int64(len(content)),
	)

	var second []pkgstore.FetchProgress
	if _, err := store.FetchWithOptions(
		context.Background(),
		source,
		pkgstore.FetchOptions{
			Progress: func(event pkgstore.FetchProgress) {
				second = append(second, event)
			},
		},
	); err != nil {
		t.Fatal(err)
	}
	assertFetchProgressPhase(
		t,
		second,
		pkgstore.FetchProgressCached,
		int64(len(content)),
		int64(len(content)),
	)
	for _, event := range second {
		if event.Phase != pkgstore.FetchProgressCached {
			t.Fatalf("cached fetch reported unexpected phase %q", event.Phase)
		}
	}
}

func TestFetchHTTPFallsBackAfterChecksumMismatch(t *testing.T) {
	content := []byte("verified HTTP response")
	var badRequests atomic.Int32
	var goodRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		switch request.URL.Path {
		case "/bad":
			badRequests.Add(1)
			_, _ = response.Write([]byte("wrong content"))
		case "/good":
			goodRequests.Add(1)
			_, _ = response.Write(content)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	store := newTestStore(t)
	fetch := pkgstore.Fetch{
		Filename: "source",
		SHA256:   sha256Hex(content),
		URLs:     []string{server.URL + "/bad", server.URL + "/good"},
	}
	var events []pkgstore.FetchProgress
	cachedPath, err := store.FetchWithOptions(
		context.Background(),
		fetch,
		pkgstore.FetchOptions{
			Progress: func(event pkgstore.FetchProgress) {
				events = append(events, event)
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertFetchProgressPhase(
		t,
		events,
		pkgstore.FetchProgressURLFailed,
		0,
		-1,
	)
	assertFetchProgressPhase(
		t,
		events,
		pkgstore.FetchProgressComplete,
		int64(len(content)),
		int64(len(content)),
	)
	if badRequests.Load() != 1 || goodRequests.Load() != 1 {
		t.Fatalf(
			"request counts = bad:%d good:%d, want bad:1 good:1",
			badRequests.Load(),
			goodRequests.Load(),
		)
	}
	if _, err := os.Stat(cachedPath + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial fetch still exists: %v", err)
	}

	got, err := os.ReadFile(cachedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("cached content = %q, want %q", got, content)
	}
}

func assertFetchProgressPhase(
	t *testing.T,
	events []pkgstore.FetchProgress,
	phase pkgstore.FetchProgressPhase,
	completed int64,
	total int64,
) {
	t.Helper()
	for _, event := range events {
		if event.Phase == phase &&
			event.CompletedBytes == completed &&
			event.TotalBytes == total {
			return
		}
	}
	t.Fatalf(
		"fetch progress has no %q event with bytes (%d, %d): %+v",
		phase,
		completed,
		total,
		events,
	)
}

func TestFetchReplacesInvalidCacheEntry(t *testing.T) {
	content := []byte("replacement content")
	server := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		_, _ = response.Write(content)
	}))
	defer server.Close()

	store := newTestStore(t)
	digest := sha256Hex(content)
	fetchRoot := filepath.Join(store.Path(), "fetch")
	if err := os.MkdirAll(fetchRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	cachedPath := filepath.Join(fetchRoot, digest)
	if err := os.WriteFile(cachedPath, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}

	gotPath, err := store.Fetch(context.Background(), pkgstore.Fetch{
		Filename: "source",
		SHA256:   digest,
		URLs:     []string{server.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != cachedPath {
		t.Fatalf("Fetch() path = %q, want %q", gotPath, cachedPath)
	}
	got, err := os.ReadFile(cachedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("cached content = %q, want %q", got, content)
	}
}

func TestFetchAllURLsFailWithoutPublishingPartial(t *testing.T) {
	store := newTestStore(t)
	content := []byte("expected")
	digest := sha256Hex(content)
	cachedPath := filepath.Join(store.Path(), "fetch", digest)

	_, err := store.Fetch(context.Background(), pkgstore.Fetch{
		Filename: "source",
		SHA256:   digest,
		URLs: []string{
			"ipfs://example",
			"file:///definitely/not/a/magpkg/source",
		},
	})
	if err == nil {
		t.Fatal("Fetch() succeeded, want an error")
	}
	if !strings.Contains(err.Error(), `unsupported fetch URL scheme "ipfs"`) {
		t.Fatalf("error = %v, want unsupported IPFS scheme error", err)
	}
	if !strings.Contains(err.Error(), "open file URL") {
		t.Fatalf("error = %v, want file URL error", err)
	}
	for _, path := range []string{cachedPath, cachedPath + ".tmp"} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("failed fetch published %q: %v", path, statErr)
		}
	}
	if _, err := os.Stat(cachedPath + ".lock"); err != nil {
		t.Fatalf("fetch lock was not retained: %v", err)
	}
}

func TestFetchWaitForLockHonorsContext(t *testing.T) {
	store := newTestStore(t)
	content := []byte("locked source")
	digest := sha256Hex(content)
	fetchRoot := filepath.Join(store.Path(), "fetch")
	if err := os.MkdirAll(fetchRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(fetchRoot, digest+".lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lockFile.Close()
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer unix.Flock(int(lockFile.Fd()), unix.LOCK_UN) //nolint:errcheck

	sourcePath := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(sourcePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	sourceURL := (&url.URL{Scheme: "file", Path: sourcePath}).String()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = store.Fetch(ctx, pkgstore.Fetch{
		Filename: "source",
		SHA256:   digest,
		URLs:     []string{sourceURL},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if _, err := os.Stat(filepath.Join(fetchRoot, digest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Fetch() wrote while lock was held: %v", err)
	}
}

func TestFetchValidatesDefinition(t *testing.T) {
	store := newTestStore(t)
	tests := []struct {
		name      string
		fetch     pkgstore.Fetch
		wantError string
	}{
		{
			name: "filename",
			fetch: pkgstore.Fetch{
				SHA256: strings.Repeat("0", 64),
			},
			wantError: "fetch filename is empty",
		},
		{
			name: "checksum",
			fetch: pkgstore.Fetch{
				Filename: "source",
				SHA256:   "invalid",
			},
			wantError: "fetch SHA-256",
		},
		{
			name: "URLs",
			fetch: pkgstore.Fetch{
				Filename: "source",
				SHA256:   strings.Repeat("0", 64),
			},
			wantError: "no URLs provided",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := store.Fetch(context.Background(), test.fetch)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want error containing %q", err, test.wantError)
			}
		})
	}
}

func newTestStore(t *testing.T) *pkgstore.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "store")
	if err := pkgstore.Init(path); err != nil {
		t.Fatal(err)
	}
	store, err := pkgstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
