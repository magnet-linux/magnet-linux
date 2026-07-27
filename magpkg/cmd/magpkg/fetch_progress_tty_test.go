package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
)

func TestFetchProgressRendererUsesPlainOutputForNonTerminal(t *testing.T) {
	renderer := newFetchProgressRenderer(context.Background(), io.Discard)
	if _, ok := renderer.(*plainFetchProgress); !ok {
		t.Fatalf("non-terminal renderer has type %T, want plain progress", renderer)
	}
	renderer.Finish()
}

func TestTTYFetchProgressCompletesAndFailsBars(t *testing.T) {
	var output bytes.Buffer
	progress := newTTYFetchProgress(context.Background(), &output)

	download := pkgstore.FetchProgress{
		Filename:   "source.tar.zst",
		URL:        "https://example.com/source.tar.zst",
		Phase:      pkgstore.FetchProgressDownloading,
		TotalBytes: 100,
	}
	progress.Report(download)
	key := ttyFetchProgressKey{
		filename: download.Filename,
		url:      download.URL,
	}
	progress.mutex.Lock()
	completedBar := progress.bars[key].bar
	progress.mutex.Unlock()

	download.CompletedBytes = 50
	progress.Report(download)
	progress.Report(pkgstore.FetchProgress{
		Filename:       download.Filename,
		URL:            download.URL,
		Phase:          pkgstore.FetchProgressComplete,
		CompletedBytes: 100,
		TotalBytes:     100,
	})
	completedBar.Wait()
	if !completedBar.Completed() {
		t.Fatal("completed transfer bar is not complete")
	}

	failed := pkgstore.FetchProgress{
		Filename:   "fallback.tar.zst",
		URL:        "https://example.com/bad.tar.zst",
		Phase:      pkgstore.FetchProgressDownloading,
		TotalBytes: -1,
	}
	progress.Report(failed)
	failedKey := ttyFetchProgressKey{
		filename: failed.Filename,
		url:      failed.URL,
	}
	progress.mutex.Lock()
	failedBar := progress.bars[failedKey].bar
	progress.mutex.Unlock()
	progress.Report(pkgstore.FetchProgress{
		Filename: failed.Filename,
		URL:      failed.URL,
		Phase:    pkgstore.FetchProgressURLFailed,
		Err:      errors.New("connection reset"),
	})
	failedBar.Wait()
	if !failedBar.Aborted() {
		t.Fatal("failed transfer bar was not aborted")
	}

	progress.Report(pkgstore.FetchProgress{
		Filename:       "cached.tar.zst",
		Phase:          pkgstore.FetchProgressCached,
		CompletedBytes: 42,
		TotalBytes:     42,
	})
	progress.Finish()

	for _, expected := range []string{
		`fetch "source.tar.zst": complete (100 B)`,
		`fetch "fallback.tar.zst": https://example.com/bad.tar.zst failed: connection reset`,
		`fetch "cached.tar.zst": cached (42 B)`,
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("TTY output does not contain %q:\n%q", expected, output.String())
		}
	}
}

func TestTTYFetchProgressFinishAbortsActiveBars(t *testing.T) {
	progress := newTTYFetchProgress(context.Background(), io.Discard)
	event := pkgstore.FetchProgress{
		Filename:   "active.tar.zst",
		URL:        "https://example.com/active.tar.zst",
		Phase:      pkgstore.FetchProgressDownloading,
		TotalBytes: 100,
	}
	progress.Report(event)
	key := ttyFetchProgressKey{
		filename: event.Filename,
		url:      event.URL,
	}
	progress.mutex.Lock()
	bar := progress.bars[key].bar
	progress.mutex.Unlock()

	progress.Finish()
	if !bar.Aborted() {
		t.Fatal("active bar was not aborted during renderer shutdown")
	}
}

func TestTTYFetchProgressFinishesAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	progress := newTTYFetchProgress(ctx, io.Discard)
	progress.Report(pkgstore.FetchProgress{
		Filename:   "canceled.tar.zst",
		URL:        "https://example.com/canceled.tar.zst",
		Phase:      pkgstore.FetchProgressDownloading,
		TotalBytes: 100,
	})
	cancel()

	done := make(chan struct{})
	go func() {
		progress.Finish()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("TTY progress did not stop after context cancellation")
	}
}
