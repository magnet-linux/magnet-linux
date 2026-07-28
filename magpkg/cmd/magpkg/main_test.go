package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
	graphjsonnet "github.com/andrewchambers/magnet-linux/magpkg/pkggraph/jsonnet"
	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
)

func TestInitAndBuildCommands(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runCLI(
		context.Background(),
		[]string{"init", "--store", storePath},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("init: %v\nstderr:\n%s", err, stderr.String())
	}
	absoluteStore, err := filepath.Abs(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(stdout.String()); got != absoluteStore {
		t.Fatalf("init output = %q, want %q", got, absoluteStore)
	}

	sourceArchive := writeSourceArchive(t)
	sourceContents, err := os.ReadFile(sourceArchive)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(sourceContents)
	sourceURL := (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(sourceArchive),
	}).String()
	manifest := filepath.Join(t.TempDir(), "packages.jsonnet")
	manifestContents := fmt.Sprintf(`{
  bootstrap: {
    name: 'bootstrap',
    build: { kind: 'unpack' },
    runDeps: [],
    buildDeps: [],
    fetch: [{
      filename: 'source.tar',
      sha256: %q,
      urls: [std.extVar('source_url')],
    }],
  },
}
`, hex.EncodeToString(digest[:]))
	if err := os.WriteFile(manifest, []byte(manifestContents), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if err := runCLI(
		context.Background(),
		[]string{
			"build",
			"--store", storePath,
			"--jobs", "1",
			"--parallelism", "1",
			"--ext-str", "source_url=" + sourceURL,
			manifest,
			"bootstrap",
		},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("build: %v\nstderr:\n%s", err, stderr.String())
	}
	graph, err := graphjsonnet.EvaluateFile(
		context.Background(),
		manifest,
		graphjsonnet.Options{
			ExtVars: map[string]string{"source_url": sourceURL},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	packageID := graph.Roots["bootstrap"]
	wantArtifact := filepath.Join(
		absoluteStore,
		"packages",
		"bootstrap-"+string(packageID)+".tar.zst",
	)
	if got := strings.TrimSpace(stdout.String()); got != wantArtifact {
		t.Fatalf("build output = %q, want %q", got, wantArtifact)
	}
	for _, expected := range []string{
		`fetch "source.tar": downloading `,
		`fetch "source.tar": complete`,
	} {
		if !strings.Contains(stderr.String(), expected) {
			t.Fatalf(
				"build stderr does not contain %q:\n%s",
				expected,
				stderr.String(),
			)
		}
	}
	info, err := os.Stat(wantArtifact)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("artifact mode = %v, want a regular file", info.Mode())
	}

	// A package ID is accepted in place of an exported root name, and an
	// already-built package is reused.
	stdout.Reset()
	stderr.Reset()
	if err := runCLI(
		context.Background(),
		[]string{
			"build",
			string(packageID),
			"--store", storePath,
			"-f", manifest,
			"--ext-str", "source_url=" + sourceURL,
		},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("build by package ID: %v\nstderr:\n%s", err, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != wantArtifact {
		t.Fatalf("second build output = %q, want %q", got, wantArtifact)
	}
}

func TestExportCommandWritesStdoutAndAtomicFile(t *testing.T) {
	t.Setenv(binaryCacheEnvironmentVariable, "")
	storePath := filepath.Join(t.TempDir(), "store")
	if err := pkgstore.Init(storePath); err != nil {
		t.Fatal(err)
	}
	sourceArchive := writeSourceArchive(t)
	sourceContents, err := os.ReadFile(sourceArchive)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(sourceContents)
	sourceURL := (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(sourceArchive),
	}).String()
	graphFile := filepath.Join(t.TempDir(), "packages.jsonnet")
	graphContents := fmt.Sprintf(`{
  environment: {
    name: 'environment',
    build: { kind: 'unpack' },
    runDeps: [],
    buildDeps: [],
    fetch: [{
      filename: 'source.tar',
      sha256: %q,
      urls: [%q],
    }],
  },
}
`, hex.EncodeToString(digest[:]), sourceURL)
	if err := os.WriteFile(graphFile, []byte(graphContents), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runCLI(
		context.Background(),
		[]string{
			"export",
			"--store", storePath,
			"--format", "tar",
			"--output", "-",
			graphFile,
			"environment",
		},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("export to stdout: %v\nstderr:\n%s", err, stderr.String())
	}
	reader := tar.NewReader(bytes.NewReader(stdout.Bytes()))
	foundHello := false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read stdout archive: %v", err)
		}
		if header.Name != "hello" {
			continue
		}
		contents, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if string(contents) != "hello\n" {
			t.Fatalf("hello contents = %q, want hello", contents)
		}
		foundHello = true
	}
	if !foundHello {
		t.Fatal("stdout archive does not contain hello")
	}

	outputPath := filepath.Join(t.TempDir(), "environment.tar.xz")
	stdout.Reset()
	stderr.Reset()
	if err := runCLI(
		context.Background(),
		[]string{
			"export",
			"--store", storePath,
			"--no-build",
			"--output", outputPath,
			graphFile,
			"environment",
		},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("export to file: %v\nstderr:\n%s", err, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("file export wrote to stdout: %q", stdout.Bytes())
	}
	archive, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	xzMagic := []byte{0xfd, 0x37, 0x7a, 0x58, 0x5a, 0x00}
	if !bytes.HasPrefix(archive, xzMagic) {
		t.Fatalf("file export prefix = %x, want xz", archive[:min(len(archive), 6)])
	}
	if err := runCLI(
		context.Background(),
		[]string{
			"export",
			"--store", storePath,
			"--no-build",
			"--output", outputPath,
			graphFile,
			"environment",
		},
		&bytes.Buffer{},
		&bytes.Buffer{},
	); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second file export error = %v, want existing output error", err)
	}
	unchanged, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(archive, unchanged) {
		t.Fatal("existing export archive was modified")
	}
}

func TestResolveExportFormat(t *testing.T) {
	tests := []struct {
		output   string
		explicit string
		want     pkgstore.ExportFormat
	}{
		{output: "environment.tar", want: pkgstore.ExportFormatTar},
		{output: "environment.tar.gz", want: pkgstore.ExportFormatTarGzip},
		{output: "environment.tgz", want: pkgstore.ExportFormatTarGzip},
		{output: "environment.tar.xz", want: pkgstore.ExportFormatTarXZ},
		{output: "environment.txz", want: pkgstore.ExportFormatTarXZ},
		{output: "environment.tar.zst", want: pkgstore.ExportFormatTarZstd},
		{
			output:   "environment.bundle",
			explicit: "tar.zst",
			want:     pkgstore.ExportFormatTarZstd,
		},
		{output: "-", explicit: "tar", want: pkgstore.ExportFormatTar},
	}
	for _, test := range tests {
		got, err := resolveExportFormat(test.output, test.explicit)
		if err != nil {
			t.Fatalf(
				"resolveExportFormat(%q, %q): %v",
				test.output,
				test.explicit,
				err,
			)
		}
		if got != test.want {
			t.Fatalf(
				"resolveExportFormat(%q, %q) = %q, want %q",
				test.output,
				test.explicit,
				got,
				test.want,
			)
		}
	}
	for _, test := range []struct {
		output   string
		explicit string
	}{
		{output: "-"},
		{output: "environment"},
		{output: "environment.tar", explicit: "zip"},
	} {
		if _, err := resolveExportFormat(test.output, test.explicit); err == nil {
			t.Fatalf(
				"resolveExportFormat(%q, %q) succeeded",
				test.output,
				test.explicit,
			)
		}
	}
}

func TestWriteExportFileDoesNotPublishPartialOutput(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "environment.tar")
	writeErr := errors.New("injected write failure")
	err := writeExportFile(output, func(writer io.Writer) error {
		if _, err := writer.Write([]byte("partial")); err != nil {
			return err
		}
		return writeErr
	})
	if !errors.Is(err, writeErr) {
		t.Fatalf("writeExportFile() error = %v, want injected error", err)
	}
	if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial output was published: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary export files remain: %v", entries)
	}
}

func TestBuildUsesBinaryCacheFromEnvironment(t *testing.T) {
	t.Setenv(binaryCacheEnvironmentVariable, "")
	graphFile := filepath.Join(t.TempDir(), "packages.jsonnet")
	if err := os.WriteFile(
		graphFile,
		[]byte(`{
  cached: {
    name: 'cached',
    build: { kind: 'none' },
    runDeps: [],
    buildDeps: [],
    fetch: [],
  },
}
`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	remotePath := filepath.Join(t.TempDir(), "remote")
	if err := pkgstore.Init(remotePath); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runCLI(
		context.Background(),
		[]string{"build", "--store", remotePath, graphFile, "cached"},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("populate remote store: %v\nstderr:\n%s", err, stderr.String())
	}
	remoteArtifact := strings.TrimSpace(stdout.String())
	remoteStore, err := pkgstore.Open(remotePath)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := pkgstore.NewBinaryCacheHandler(
		remoteStore,
		pkgstore.BinaryCacheHandlerOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	localPath := filepath.Join(t.TempDir(), "local")
	if err := pkgstore.Init(localPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv(binaryCacheEnvironmentVariable, server.URL)
	stdout.Reset()
	stderr.Reset()
	if err := runCLI(
		context.Background(),
		[]string{
			"build",
			"--store", localPath,
			"--cache-only",
			graphFile,
			"cached",
		},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("cache-only build: %v\nstderr:\n%s", err, stderr.String())
	}
	localArtifact := strings.TrimSpace(stdout.String())
	remoteBytes, err := os.ReadFile(remoteArtifact)
	if err != nil {
		t.Fatal(err)
	}
	localBytes, err := os.ReadFile(localArtifact)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(localBytes, remoteBytes) {
		t.Fatal("local artifact differs from binary-cache artifact")
	}
}

func TestServeCacheCommand(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store")
	if err := pkgstore.Init(storePath); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	result := make(chan error, 1)
	go func() {
		err := runCLI(
			ctx,
			[]string{
				"serve-cache",
				"--store", storePath,
				"--listen", "127.0.0.1:0",
				"--no-touch",
			},
			writer,
			&bytes.Buffer{},
		)
		_ = writer.CloseWithError(err)
		result <- err
	}()

	addressResult := make(chan struct {
		address string
		err     error
	}, 1)
	go func() {
		address, err := bufio.NewReader(reader).ReadString('\n')
		addressResult <- struct {
			address string
			err     error
		}{address: strings.TrimSpace(address), err: err}
	}()
	var address string
	select {
	case read := <-addressResult:
		if read.err != nil {
			t.Fatal(read.err)
		}
		address = read.address
	case <-time.After(5 * time.Second):
		t.Fatal("serve-cache did not report its listening address")
	}

	response, err := http.Get(address + "/v1/info")
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("cache info status = %s", response.Status)
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve-cache did not shut down")
	}
}

func TestSyncAndUninstallCommands(t *testing.T) {
	t.Setenv(binaryCacheEnvironmentVariable, "")
	storePath := filepath.Join(t.TempDir(), "store")
	if err := pkgstore.Init(storePath); err != nil {
		t.Fatal(err)
	}
	sourceArchive := writeSourceArchive(t)
	sourceContents, err := os.ReadFile(sourceArchive)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(sourceContents)
	sourceURL := (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(sourceArchive),
	}).String()
	graphFile := filepath.Join(t.TempDir(), "packages.jsonnet")
	graphContents := fmt.Sprintf(`{
  hello: {
    name: 'hello',
    build: { kind: 'unpack' },
    runDeps: [],
    buildDeps: [],
    fetch: [{
      filename: 'hello.tar',
      sha256: %q,
      urls: [%q],
    }],
  },
}
`, hex.EncodeToString(digest[:]), sourceURL)
	if err := os.WriteFile(graphFile, []byte(graphContents), 0o644); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "root")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(destination, "hello"),
		[]byte("unmanaged\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runCLI(
		context.Background(),
		[]string{
			"sync",
			"--store", storePath,
			"--dest", destination,
			"--force",
			"--no-build",
			graphFile,
			"hello",
		},
		&stdout,
		&stderr,
	); err == nil || !strings.Contains(err.Error(), "has no package artifact") {
		t.Fatalf("sync --no-build error = %v, want missing artifact error", err)
	}
	stdout.Reset()
	stderr.Reset()
	if err := runCLI(
		context.Background(),
		[]string{
			"sync",
			"--store", storePath,
			"--dest", destination,
			"--force",
			"--cache-only",
			graphFile,
			"hello",
		},
		&stdout,
		&stderr,
	); err == nil || !strings.Contains(err.Error(), "no binary cache is configured") {
		t.Fatalf("sync --cache-only error = %v, want cache configuration error", err)
	}
	stdout.Reset()
	stderr.Reset()
	if err := runCLI(
		context.Background(),
		[]string{
			"sync",
			"--store", storePath,
			"--dest", destination,
			"--force",
			"--jobs", "1",
			"--parallelism", "1",
			graphFile,
			"hello",
		},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("sync: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "replace-unmanaged hello\n") {
		t.Fatalf("sync output = %q", stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(destination, "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("installed contents = %q, want hello", data)
	}
	if err := os.WriteFile(
		filepath.Join(destination, "hello"),
		[]byte("modified\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if err := runCLI(
		context.Background(),
		[]string{"uninstall", "--dest", destination, "--force"},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("uninstall: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "remove-modified hello\n") {
		t.Fatalf("uninstall output = %q", stdout.String())
	}
	if _, err := os.Lstat(filepath.Join(destination, "hello")); !errors.Is(
		err,
		os.ErrNotExist,
	) {
		t.Fatalf("installed file still exists after uninstall: %v", err)
	}
}

func TestSyncCommandDeleteUnmanagedRequiresForce(t *testing.T) {
	err := runCLI(
		context.Background(),
		[]string{
			"sync",
			"--dest", t.TempDir(),
			"--delete-unmanaged",
			"packages.jsonnet",
		},
		&bytes.Buffer{},
		&bytes.Buffer{},
	)
	if err == nil || !strings.Contains(
		err.Error(),
		"--delete-unmanaged requires --force",
	) {
		t.Fatalf("sync error = %v, want force requirement", err)
	}
}

func TestPlainFetchProgress(t *testing.T) {
	var output bytes.Buffer
	progress := newPlainFetchProgress(&output)
	now := time.Unix(0, 0)
	progress.now = func() time.Time {
		return now
	}
	download := pkgstore.FetchProgress{
		Filename:   "source.tar",
		URL:        "https://example.com/source.tar",
		Phase:      pkgstore.FetchProgressDownloading,
		TotalBytes: 100,
	}
	progress.Report(download)
	download.CompletedBytes = 5
	progress.Report(download)
	download.CompletedBytes = 15
	progress.Report(download)
	now = now.Add(fetchProgressPrintInterval - time.Millisecond)
	download.CompletedBytes = 25
	progress.Report(download)
	now = now.Add(time.Millisecond)
	download.CompletedBytes = 35
	progress.Report(download)
	download.CompletedBytes = 55
	progress.Report(download)
	now = now.Add(fetchProgressPrintInterval)
	download.CompletedBytes = 65
	progress.Report(download)
	progress.Report(pkgstore.FetchProgress{
		Filename:       "source.tar",
		Phase:          pkgstore.FetchProgressComplete,
		CompletedBytes: 100,
		TotalBytes:     100,
	})

	want := "" +
		"fetch \"source.tar\": downloading https://example.com/source.tar (100 B)\n" +
		"fetch \"source.tar\": 35% (35 B / 100 B)\n" +
		"fetch \"source.tar\": 65% (65 B / 100 B)\n" +
		"fetch \"source.tar\": complete (100 B)\n"
	if output.String() != want {
		t.Fatalf("progress output:\n%s\nwant:\n%s", output.String(), want)
	}
}

func TestInitUsesStoreEnvironmentVariable(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "environment-store")
	t.Setenv(storeEnvironmentVariable, storePath)
	var stdout bytes.Buffer
	if err := runCLI(
		context.Background(),
		[]string{"init"},
		&stdout,
		&bytes.Buffer{},
	); err != nil {
		t.Fatal(err)
	}
	absoluteStore, err := filepath.Abs(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(stdout.String()); got != absoluteStore {
		t.Fatalf("init output = %q, want %q", got, absoluteStore)
	}
}

func TestCleanCommandSelectsTargetsAndUsesAge(t *testing.T) {
	storePath := t.TempDir()
	fetchPath, packagePath := writeCleanCommandEntries(t, storePath)
	now := time.Now()
	if err := os.Chtimes(
		fetchPath,
		now.Add(-10*24*time.Hour),
		now.Add(-10*24*time.Hour),
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(
		packagePath,
		now.Add(-40*24*time.Hour),
		now.Add(-40*24*time.Hour),
	); err != nil {
		t.Fatal(err)
	}

	if err := runCLI(
		context.Background(),
		[]string{
			"clean",
			"--store", storePath,
			"--fetches",
			"--older-than", "5d",
		},
		&bytes.Buffer{},
		&bytes.Buffer{},
	); err != nil {
		t.Fatal(err)
	}
	assertMainPathMissing(t, fetchPath)
	assertMainRegularFile(t, packagePath)

	if err := runCLI(
		context.Background(),
		[]string{
			"clean",
			"--store", storePath,
			"--packages",
			"--older-than", "30d",
		},
		&bytes.Buffer{},
		&bytes.Buffer{},
	); err != nil {
		t.Fatal(err)
	}
	assertMainPathMissing(t, packagePath)
}

func TestCleanCommandDefaultAge(t *testing.T) {
	t.Setenv("HOME", filepath.Join(t.TempDir(), "home"))
	storePath := t.TempDir()
	fetchPath, packagePath := writeCleanCommandEntries(t, storePath)
	old := time.Now().Add(-8 * 24 * time.Hour)
	for _, path := range []string{fetchPath, packagePath} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}

	if err := runCLI(
		context.Background(),
		[]string{"clean", "--store", storePath, "--all"},
		&bytes.Buffer{},
		&bytes.Buffer{},
	); err != nil {
		t.Fatal(err)
	}
	assertMainPathMissing(t, fetchPath)
	assertMainPathMissing(t, packagePath)
}

func TestCleanCommandAllAcceptsAgeOptions(t *testing.T) {
	t.Setenv("HOME", filepath.Join(t.TempDir(), "home"))
	storePath := t.TempDir()
	fetchPath, packagePath := writeCleanCommandEntries(t, storePath)
	old := time.Now().Add(-2 * 24 * time.Hour)
	for _, path := range []string{fetchPath, packagePath} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	var stdout bytes.Buffer
	if err := runCLI(
		context.Background(),
		[]string{
			"clean",
			"--store", storePath,
			"--all",
			"--older-than", "1d",
		},
		&stdout,
		&bytes.Buffer{},
	); err != nil {
		t.Fatal(err)
	}
	assertMainPathMissing(t, fetchPath)
	assertMainPathMissing(t, packagePath)
	wantOutput := fmt.Sprintf(
		"cleaned fetch %s\ncleaned package %s\n",
		fetchPath,
		packagePath,
	)
	if stdout.String() != wantOutput {
		t.Fatalf("clean output = %q, want %q", stdout.String(), wantOutput)
	}
}

func TestCleanCommandRejectsInvalidOptions(t *testing.T) {
	for _, args := range [][]string{
		{"clean"},
		{"clean", "--older-than", "1d"},
		{"clean", "--all", "--older-than", "0"},
		{"clean", "--fetches", "--fetches-older-than", "1d"},
		{"clean", "--all", "fetches"},
	} {
		if err := runCLI(
			context.Background(),
			args,
			&bytes.Buffer{},
			&bytes.Buffer{},
		); err == nil {
			t.Fatalf("runCLI(%v) succeeded", args)
		}
	}
}

func TestResolveStorePathUsesDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(storeEnvironmentVariable, "")
	storePath, err := resolveStorePath("")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".magpkg", "store"); storePath != want {
		t.Fatalf("store path = %q, want %q", storePath, want)
	}
}

func TestCommandStoreOpenOptions(t *testing.T) {
	options := commandStoreOpenOptions("https://cache.example")
	if options.BinaryCacheURL != "https://cache.example" {
		t.Fatalf("binary cache URL = %q", options.BinaryCacheURL)
	}
	if options.HTTPClient != http.DefaultClient {
		t.Fatal("HTTP client is not the command's host-aware default client")
	}
}

func TestParseCleanAge(t *testing.T) {
	for input, want := range map[string]time.Duration{
		"30d":   30 * 24 * time.Hour,
		"2w":    14 * 24 * time.Hour,
		"1h30m": 90 * time.Minute,
	} {
		got, err := parseCleanAge(input)
		if err != nil {
			t.Fatalf("parseCleanAge(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("parseCleanAge(%q) = %v, want %v", input, got, want)
		}
	}
	for _, input := range []string{"", "day", "1.5d"} {
		if _, err := parseCleanAge(input); err == nil {
			t.Fatalf("parseCleanAge(%q) succeeded", input)
		}
	}
}

func TestSelectBuildRootsSortsAndDeduplicatesDefaults(t *testing.T) {
	first := pkggraph.ID(strings.Repeat("a", 52))
	second := pkggraph.ID(strings.Repeat("b", 52))
	graph := &pkggraph.Graph{
		Roots: map[string]pkggraph.ID{
			"zeta":  second,
			"alpha": first,
			"alias": first,
		},
	}
	roots, err := selectBuildRoots(graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 || roots[0] != first || roots[1] != second {
		t.Fatalf("selected roots = %v, want [%s %s]", roots, first, second)
	}
}

func TestStringMapFlagRejectsMissingName(t *testing.T) {
	var values stringMapFlag
	if err := values.Set("=value"); err == nil {
		t.Fatal("stringMapFlag accepted an empty name")
	}
	if err := values.Set("name=value=with=equals"); err != nil {
		t.Fatal(err)
	}
	if values["name"] != "value=with=equals" {
		t.Fatalf("parsed value = %q", values["name"])
	}
}

func writeCleanCommandEntries(t *testing.T, storePath string) (string, string) {
	t.Helper()
	if err := pkgstore.Init(storePath); err != nil {
		t.Fatal(err)
	}
	fetchRoot := filepath.Join(storePath, "fetch")
	packageRoot := filepath.Join(storePath, "packages")
	for _, directory := range []string{fetchRoot, packageRoot} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fetchPath := filepath.Join(fetchRoot, strings.Repeat("1", 64))
	packagePath := filepath.Join(
		packageRoot,
		"package-"+strings.Repeat("a", 52)+".tar.zst",
	)
	for _, path := range []string{fetchPath, packagePath} {
		if err := os.WriteFile(path, []byte("cached"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return fetchPath, packagePath
}

func assertMainRegularFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%q is not a regular file", path)
	}
}

func assertMainPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %q exists or cannot be inspected: %v", path, err)
	}
}

func writeSourceArchive(t *testing.T) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "source.tar")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	contents := []byte("hello\n")
	if err := writer.WriteHeader(&tar.Header{
		Name:     "hello",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     int64(len(contents)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return archivePath
}
