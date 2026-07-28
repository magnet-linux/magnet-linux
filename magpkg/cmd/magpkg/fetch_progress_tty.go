package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"golang.org/x/term"
)

const ttyFetchProgressRefreshInterval = 200 * time.Millisecond

type fetchProgressRenderer interface {
	Report(pkgstore.FetchProgress)
	Finish()
}

func newFetchProgressRenderer(
	ctx context.Context,
	writer io.Writer,
) fetchProgressRenderer {
	if os.Getenv("TERM") != "dumb" {
		if terminalWriter, ok := writer.(interface{ Fd() uintptr }); ok &&
			term.IsTerminal(int(terminalWriter.Fd())) {
			return newTTYFetchProgress(ctx, writer)
		}
	}
	return newPlainFetchProgress(writer)
}

type ttyFetchProgressKey struct {
	filename string
	sha256   string
	url      string
}

type ttyFetchProgressBar struct {
	bar   *mpb.Bar
	total int64
}

type ttyFetchProgress struct {
	mutex    sync.Mutex
	progress *mpb.Progress
	bars     map[ttyFetchProgressKey]ttyFetchProgressBar
}

func newTTYFetchProgress(ctx context.Context, writer io.Writer) *ttyFetchProgress {
	return &ttyFetchProgress{
		progress: mpb.NewWithContext(
			ctx,
			mpb.WithOutput(writer),
			mpb.WithAutoRefresh(),
			mpb.WithRefreshRate(ttyFetchProgressRefreshInterval),
		),
		bars: make(map[ttyFetchProgressKey]ttyFetchProgressBar),
	}
}

func (progress *ttyFetchProgress) Report(event pkgstore.FetchProgress) {
	if progress == nil || progress.progress == nil {
		return
	}

	progress.mutex.Lock()
	defer progress.mutex.Unlock()

	key := ttyFetchProgressKey{
		filename: event.Filename,
		sha256:   event.SHA256,
		url:      event.URL,
	}
	switch event.Phase {
	case pkgstore.FetchProgressDownloading:
		item, exists := progress.bars[key]
		if !exists {
			item = ttyFetchProgressBar{
				bar:   progress.newBar(event),
				total: event.TotalBytes,
			}
			progress.bars[key] = item
		}
		if item.total <= 0 && event.TotalBytes > 0 {
			item.bar.SetTotal(event.TotalBytes, false)
			item.total = event.TotalBytes
			progress.bars[key] = item
		}
		item.bar.SetCurrent(event.CompletedBytes)

	case pkgstore.FetchProgressURLFailed:
		progress.removeBar(key, true)
		progress.writeStatus(
			"fetch %q: %s failed: %v\n",
			event.Filename,
			event.URL,
			event.Err,
		)

	case pkgstore.FetchProgressComplete:
		if item, exists := progress.bars[key]; exists {
			total := event.CompletedBytes
			if total < 0 {
				total = item.total
			}
			item.bar.SetTotal(total, true)
			delete(progress.bars, key)
		}
		progress.writeStatus(
			"fetch %q: complete%s\n",
			event.Filename,
			plainFetchSizeSuffix(event.CompletedBytes),
		)

	case pkgstore.FetchProgressCached:
		progress.writeStatus(
			"fetch %q: cached%s\n",
			event.Filename,
			plainFetchSizeSuffix(event.CompletedBytes),
		)
	}
}

func (progress *ttyFetchProgress) newBar(event pkgstore.FetchProgress) *mpb.Bar {
	options := []mpb.BarOption{
		mpb.PrependDecorators(
			decor.Name("fetch " + strconv.Quote(event.Filename) + " "),
		),
		mpb.BarRemoveOnComplete(),
	}
	if event.TotalBytes > 0 {
		options = append(options, mpb.AppendDecorators(
			decor.CountersKibiByte("% .1f / % .1f "),
			decor.Percentage(),
			decor.AverageSpeed(decor.SizeB1024(0), " % .1f"),
		))
		bar := progress.progress.New(
			0,
			mpb.BarStyle(),
			options...,
		)
		bar.SetTotal(event.TotalBytes, false)
		return bar
	}
	options = append(options, mpb.AppendDecorators(
		decor.CurrentKibiByte("% .1f "),
		decor.AverageSpeed(decor.SizeB1024(0), "% .1f"),
	))
	return progress.progress.New(0, mpb.SpinnerStyle(), options...)
}

func (progress *ttyFetchProgress) removeBar(
	key ttyFetchProgressKey,
	abort bool,
) {
	item, exists := progress.bars[key]
	if !exists {
		return
	}
	if abort {
		item.bar.Abort(true)
	}
	delete(progress.bars, key)
}

func (progress *ttyFetchProgress) writeStatus(format string, arguments ...any) {
	_, _ = fmt.Fprintf(progress.progress, format, arguments...)
}

func (progress *ttyFetchProgress) Finish() {
	if progress == nil || progress.progress == nil {
		return
	}
	progress.mutex.Lock()
	for key := range progress.bars {
		progress.removeBar(key, true)
	}
	progress.mutex.Unlock()
	progress.progress.Wait()
}

func (progress *plainFetchProgress) Finish() {}
