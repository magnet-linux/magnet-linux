package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
	"github.com/andrewchambers/magnet-linux/magpkg/shellenv"
	"github.com/spf13/pflag"
)

const (
	storeEnvironmentVariable       = "MAGPKG_STORE"
	binaryCacheEnvironmentVariable = "MAGPKG_BINARY_CACHE"
	defaultCleanAge                = 7 * 24 * time.Hour
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	if err := runCLIWithInput(
		ctx,
		os.Args[1:],
		os.Stdin,
		os.Stdout,
		os.Stderr,
	); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			if exitError.ExitCode() >= 0 {
				os.Exit(exitError.ExitCode())
			}
			if status, ok := exitError.ProcessState.Sys().(syscall.WaitStatus); ok &&
				status.Signaled() {
				os.Exit(128 + int(status.Signal()))
			}
		}
		fmt.Fprintf(os.Stderr, "magpkg: %v\n", err)
		os.Exit(1)
	}
}

func runCLIWithInput(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) error {
	if len(args) == 0 {
		writeUsage(stderr)
		return errors.New("a command is required")
	}

	switch args[0] {
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "build":
		return runBuild(ctx, args[1:], stdout, stderr)
	case "shell":
		return runShell(ctx, args[1:], stdin, stdout, stderr)
	case "export":
		return runExport(ctx, args[1:], stdout, stderr)
	case "sync":
		return runSync(ctx, args[1:], stdout, stderr)
	case "uninstall":
		return runUninstall(ctx, args[1:], stdout, stderr)
	case "clean":
		return runClean(ctx, args[1:], stdout, stderr)
	case "serve-cache":
		return runServeCache(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		writeUsage(stdout)
		return nil
	default:
		writeUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runClean(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	flags := pflag.NewFlagSet("clean", pflag.ContinueOnError)
	flags.SetOutput(stderr)
	storeFlag := flags.String(
		"store",
		"",
		"package store path (default: $MAGPKG_STORE or ~/.magpkg/store)",
	)
	cleanFetches := flags.Bool(
		"fetches",
		false,
		"clean cached source fetches",
	)
	cleanPackages := flags.Bool(
		"packages",
		false,
		"clean package artifacts and build workspaces",
	)
	cleanEnvironments := flags.Bool(
		"environments",
		false,
		"clean materialized shell environments and workspaces",
	)
	cleanTemp := flags.Bool(
		"tmp",
		false,
		"clean abandoned temporary files regardless of age",
	)
	cleanAll := flags.Bool(
		"all",
		false,
		"clean fetches, packages, shell environments, and temporary files",
	)
	var olderThan cleanAgeFlag
	flags.Var(
		&olderThan,
		"older-than",
		"minimum unused age (default 7d; supports d and w)",
	)
	flags.Usage = func() {
		fmt.Fprintln(
			stderr,
			"usage: magpkg clean (--all | --fetches | --packages | --environments | --tmp) [options]",
		)
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(
			stderr,
			"At least one cleanup target is required.",
		)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return errors.New("clean does not accept positional arguments")
	}

	if !*cleanAll && !*cleanFetches && !*cleanPackages &&
		!*cleanEnvironments && !*cleanTemp {
		return errors.New(
			"clean requires at least one of --all, --fetches, --packages, " +
				"--environments, or --tmp",
		)
	}
	if *cleanAll {
		*cleanFetches = true
		*cleanPackages = true
		*cleanEnvironments = true
		*cleanTemp = true
	}
	cleanAge := defaultCleanAge
	if olderThan.set {
		cleanAge = olderThan.duration
	}
	if cleanAge <= 0 {
		return errors.New("cleanup age must be positive")
	}

	cleanOptions := pkgstore.CleanOptions{
		OnRemove: func(event pkgstore.CleanEvent) {
			fmt.Fprintf(stdout, "cleaned %s %s\n", event.Kind, event.Path)
		},
	}
	if *cleanFetches || *cleanPackages || *cleanTemp {
		storePath, err := resolveStorePath(*storeFlag)
		if err != nil {
			return err
		}
		store, err := pkgstore.Open(storePath, pkgstore.OpenOptions{})
		if err != nil {
			return fmt.Errorf(
				"open store %q (run `magpkg init` first): %w",
				storePath,
				err,
			)
		}
		if *cleanFetches {
			if err := store.CleanFetches(
				ctx,
				cleanAge,
				cleanOptions,
			); err != nil {
				return fmt.Errorf("clean fetches: %w", err)
			}
		}
		if *cleanPackages {
			if err := store.CleanPackages(
				ctx,
				cleanAge,
				cleanOptions,
			); err != nil {
				return fmt.Errorf("clean packages: %w", err)
			}
		}
		if *cleanTemp {
			if err := store.CleanTempFiles(ctx, cleanOptions); err != nil {
				return fmt.Errorf("clean temporary files: %w", err)
			}
		}
	}
	if *cleanEnvironments {
		environmentsPath, err := resolveEnvironmentsPath()
		if err != nil {
			return err
		}
		if err := shellenv.Clean(
			ctx,
			environmentsPath,
			cleanAge,
			shellenv.CleanOptions{
				OnRemove: func(event shellenv.CleanEvent) {
					fmt.Fprintf(
						stdout,
						"cleaned %s %s\n",
						event.Kind,
						event.Path,
					)
				},
			},
		); err != nil {
			return fmt.Errorf("clean environments: %w", err)
		}
	}
	return nil
}

const (
	fetchProgressPrintInterval   = time.Second
	unknownFetchProgressInterval = int64(16 * 1024 * 1024)
)

type plainFetchProgress struct {
	mutex  sync.Mutex
	writer io.Writer
	now    func() time.Time
	states map[plainFetchProgressKey]plainFetchProgressState
}

type plainFetchProgressKey struct {
	filename string
	sha256   string
	url      string
	phase    pkgstore.FetchProgressPhase
}

type plainFetchProgressState struct {
	lastPercentBucket int64
	lastByteBucket    int64
	lastPrint         time.Time
}

func newPlainFetchProgress(writer io.Writer) *plainFetchProgress {
	return &plainFetchProgress{
		writer: writer,
		now:    time.Now,
		states: make(map[plainFetchProgressKey]plainFetchProgressState),
	}
}

func (progress *plainFetchProgress) Report(event pkgstore.FetchProgress) {
	if progress == nil || progress.writer == nil {
		return
	}

	progress.mutex.Lock()
	defer progress.mutex.Unlock()

	switch event.Phase {
	case pkgstore.FetchProgressDownloading:
		label := "downloading"
		if event.URL != "" {
			label += " " + event.URL
		}
		progress.reportTransfer(event, label)
	case pkgstore.FetchProgressCached:
		fmt.Fprintf(
			progress.writer,
			"fetch %q: cached%s\n",
			event.Filename,
			plainFetchSizeSuffix(event.CompletedBytes),
		)
	case pkgstore.FetchProgressURLFailed:
		fmt.Fprintf(
			progress.writer,
			"fetch %q: %s failed: %v\n",
			event.Filename,
			event.URL,
			event.Err,
		)
	case pkgstore.FetchProgressComplete:
		fmt.Fprintf(
			progress.writer,
			"fetch %q: complete%s\n",
			event.Filename,
			plainFetchSizeSuffix(event.CompletedBytes),
		)
	}
}

func (progress *plainFetchProgress) reportTransfer(
	event pkgstore.FetchProgress,
	label string,
) {
	key := plainFetchProgressKey{
		filename: event.Filename,
		sha256:   event.SHA256,
		url:      event.URL,
		phase:    event.Phase,
	}
	state, exists := progress.states[key]
	if !exists {
		fmt.Fprintf(
			progress.writer,
			"fetch %q: %s%s\n",
			event.Filename,
			label,
			plainFetchTotalSuffix(event.TotalBytes),
		)
		progress.states[key] = plainFetchProgressState{
			lastPrint: progress.now(),
		}
		return
	}

	if event.TotalBytes > 0 {
		percent := event.CompletedBytes * 100 / event.TotalBytes
		bucket := percent / 10
		if percent >= 100 || bucket <= state.lastPercentBucket {
			return
		}
		if progress.now().Sub(state.lastPrint) < fetchProgressPrintInterval {
			return
		}
		state.lastPercentBucket = bucket
		state.lastPrint = progress.now()
		progress.states[key] = state
		fmt.Fprintf(
			progress.writer,
			"fetch %q: %d%% (%s / %s)\n",
			event.Filename,
			percent,
			formatByteCount(event.CompletedBytes),
			formatByteCount(event.TotalBytes),
		)
		return
	}

	bucket := event.CompletedBytes / unknownFetchProgressInterval
	if bucket <= state.lastByteBucket {
		return
	}
	if progress.now().Sub(state.lastPrint) < fetchProgressPrintInterval {
		return
	}
	state.lastByteBucket = bucket
	state.lastPrint = progress.now()
	progress.states[key] = state
	fmt.Fprintf(
		progress.writer,
		"fetch %q: %s received\n",
		event.Filename,
		formatByteCount(event.CompletedBytes),
	)
}

func plainFetchTotalSuffix(total int64) string {
	if total < 0 {
		return ""
	}
	return " (" + formatByteCount(total) + ")"
}

func plainFetchSizeSuffix(size int64) string {
	if size < 0 {
		return ""
	}
	return " (" + formatByteCount(size) + ")"
}

func formatByteCount(bytes int64) string {
	const unit = int64(1024)
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	divisor := unit
	exponent := 0
	for quotient := bytes / unit; quotient >= unit && exponent < 5; quotient /= unit {
		divisor *= unit
		exponent++
	}
	return fmt.Sprintf(
		"%.1f %ciB",
		float64(bytes)/float64(divisor),
		"KMGTPE"[exponent],
	)
}

type stringMapFlag map[string]string

type cleanAgeFlag struct {
	duration time.Duration
	set      bool
}

func (value *cleanAgeFlag) Set(input string) error {
	duration, err := parseCleanAge(input)
	if err != nil {
		return err
	}
	value.duration = duration
	value.set = true
	return nil
}

func (value *cleanAgeFlag) String() string {
	if value == nil || !value.set {
		return ""
	}
	return value.duration.String()
}

func (*cleanAgeFlag) Type() string {
	return "age"
}

func parseCleanAge(value string) (time.Duration, error) {
	for suffix, unit := range map[string]time.Duration{
		"d": 24 * time.Hour,
		"w": 7 * 24 * time.Hour,
	} {
		number, found := strings.CutSuffix(value, suffix)
		if !found {
			continue
		}
		count, err := strconv.ParseUint(number, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid cleanup age %q", value)
		}
		maximum := uint64((time.Duration(1<<63 - 1)) / unit)
		if count > maximum {
			return 0, fmt.Errorf("cleanup age %q is too large", value)
		}
		return time.Duration(count) * unit, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid cleanup age %q: %w", value, err)
	}
	return duration, nil
}

func (values *stringMapFlag) Set(value string) error {
	name, stringValue, ok := strings.Cut(value, "=")
	if !ok || name == "" {
		return errors.New("expected NAME=VALUE")
	}
	if *values == nil {
		*values = make(map[string]string)
	}
	(*values)[name] = stringValue
	return nil
}

func (values *stringMapFlag) String() string {
	if values == nil || len(*values) == 0 {
		return ""
	}
	names := make([]string, 0, len(*values))
	for name := range *values {
		names = append(names, name)
	}
	sort.Strings(names)
	pairs := make([]string, 0, len(names))
	for _, name := range names {
		pairs = append(pairs, name+"="+(*values)[name])
	}
	return strings.Join(pairs, ",")
}

func (*stringMapFlag) Type() string {
	return "NAME=VALUE"
}

func writeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: magpkg <command> [options]")
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "commands:")
	fmt.Fprintln(writer, "  init   initialize a package store")
	fmt.Fprintln(writer, "  build  build roots from a Jsonnet package graph")
	fmt.Fprintln(writer, "  shell  enter a package-defined shell environment")
	fmt.Fprintln(writer, "  export  export runtime closures as a tar archive")
	fmt.Fprintln(writer, "  sync   synchronize a runtime closure into a destination")
	fmt.Fprintln(writer, "  uninstall  remove a synchronized runtime closure")
	fmt.Fprintln(writer, "  clean  remove old cached state")
	fmt.Fprintln(writer, "  serve-cache  serve package artifacts over HTTP")
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "Run `magpkg <command> --help` for command-specific help.")
}
