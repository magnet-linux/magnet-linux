package shellenv

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
	"github.com/andrewchambers/magnet-linux/magpkg/pkgstore"
)

func TestAcquireReusesEnvironmentAndRunUsesPackageLauncher(t *testing.T) {
	store, graph, rootID := buildLauncherPackage(t, `#!/bin/sh
root=$1
shift
printf 'launcher-root=%s\n' "$root"
if test "$#" -eq 0; then
	printf 'default-shell\n'
	exit 0
fi
exec "$@"
`)
	environmentsPath := filepath.Join(t.TempDir(), "environments")

	first, err := Acquire(
		context.Background(),
		environmentsPath,
		store,
		graph,
		[]pkggraph.ID{rootID},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close() //nolint:errcheck
	if first.ID == "" {
		t.Fatal("environment ID is empty")
	}
	if want := []pkggraph.ID{rootID}; !slices.Equal(first.Packages, want) {
		t.Fatalf("environment packages = %v, want %v", first.Packages, want)
	}
	if info, err := os.Lstat(
		filepath.Join(first.RootPath, LauncherPath),
	); err != nil {
		t.Fatal(err)
	} else if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("launcher mode = %v, want executable regular file", info.Mode())
	}

	second, err := Acquire(
		context.Background(),
		environmentsPath,
		store,
		graph,
		[]pkggraph.ID{rootID},
	)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.RootPath != first.RootPath {
		t.Fatalf(
			"second environment = (%q, %q), want (%q, %q)",
			second.ID,
			second.RootPath,
			first.ID,
			first.RootPath,
		)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	workingDirectory := t.TempDir()
	if err := Run(
		context.Background(),
		first,
		[]string{"/bin/sh", "-c", "printf 'command-ran\\n'"},
		RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    []string{"PATH=/bin:/usr/bin"},
			Dir:    workingDirectory,
		},
	); err != nil {
		t.Fatalf("Run(): %v\nstderr:\n%s", err, stderr.String())
	}
	if got := stdout.String(); got !=
		"launcher-root="+first.RootPath+"\ncommand-ran\n" {
		t.Fatalf("Run() stdout = %q", got)
	}

	stdout.Reset()
	if err := Run(
		context.Background(),
		first,
		nil,
		RunOptions{
			Stdout: &stdout,
			Stderr: &stderr,
			Env:    []string{"PATH=/bin:/usr/bin"},
			Dir:    workingDirectory,
		},
	); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(stdout.String(), "default-shell\n") {
		t.Fatalf("default launcher stdout = %q", stdout.String())
	}
}

func TestRunReturnsLauncherExitStatus(t *testing.T) {
	store, graph, rootID := buildLauncherPackage(t, `#!/bin/sh
root=$1
shift
exec "$@"
`)
	environment, err := Acquire(
		context.Background(),
		filepath.Join(t.TempDir(), "environments"),
		store,
		graph,
		[]pkggraph.ID{rootID},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer environment.Close() //nolint:errcheck

	err = Run(
		context.Background(),
		environment,
		[]string{"/bin/sh", "-c", "exit 7"},
		RunOptions{
			Env: []string{"PATH=/bin:/usr/bin"},
			Dir: t.TempDir(),
		},
	)
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 7 {
		t.Fatalf("Run() error = %v, want exit status 7", err)
	}
}

func TestCleanSkipsLeasedEnvironmentAndRemovesItAfterRelease(t *testing.T) {
	store, graph, rootID := buildLauncherPackage(t, `#!/bin/sh
exit 0
`)
	environmentsPath := filepath.Join(t.TempDir(), "environments")
	environment, err := Acquire(
		context.Background(),
		environmentsPath,
		store,
		graph,
		[]pkggraph.ID{rootID},
	)
	if err != nil {
		t.Fatal(err)
	}
	finalPath := environmentPath(environmentsPath, environment.ID)
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(environment.metadataPath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := Clean(
		context.Background(),
		environmentsPath,
		time.Hour,
		CleanOptions{},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Fatalf("leased environment was removed: %v", err)
	}

	if err := environment.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(environment.metadataPath, old, old); err != nil {
		t.Fatal(err)
	}
	var events []CleanEvent
	if err := Clean(
		context.Background(),
		environmentsPath,
		time.Hour,
		CleanOptions{
			OnRemove: func(event CleanEvent) {
				events = append(events, event)
			},
		},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(finalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired environment still exists: %v", err)
	}
	want := []CleanEvent{{
		Kind: CleanKindEnvironment,
		Path: finalPath,
	}}
	if !slices.Equal(events, want) {
		t.Fatalf("cleanup events = %#v, want %#v", events, want)
	}
}

func TestAcquireRepairsInvalidEnvironment(t *testing.T) {
	store, graph, rootID := buildLauncherPackage(t, `#!/bin/sh
exit 0
`)
	environmentsPath := filepath.Join(t.TempDir(), "environments")
	environment, err := Acquire(
		context.Background(),
		environmentsPath,
		store,
		graph,
		[]pkggraph.ID{rootID},
	)
	if err != nil {
		t.Fatal(err)
	}
	environmentID := environment.ID
	finalPath := environmentPath(environmentsPath, environmentID)
	if err := environment.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		environmentMetadataPath(environmentsPath, environmentID),
		[]byte("invalid\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	repaired, err := Acquire(
		context.Background(),
		environmentsPath,
		store,
		graph,
		[]pkggraph.ID{rootID},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer repaired.Close() //nolint:errcheck
	if repaired.ID != environmentID {
		t.Fatalf("repaired environment ID = %q, want %q", repaired.ID, environmentID)
	}
	if _, err := os.Stat(filepath.Join(finalPath, "root", LauncherPath)); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentAcquireSharesPublishedEnvironment(t *testing.T) {
	store, graph, rootID := buildLauncherPackage(t, `#!/bin/sh
exit 0
`)
	environmentsPath := filepath.Join(t.TempDir(), "environments")
	start := make(chan struct{})
	results := make(chan struct {
		environment *Environment
		err         error
	}, 2)
	for range 2 {
		go func() {
			<-start
			environment, err := Acquire(
				context.Background(),
				environmentsPath,
				store,
				graph,
				[]pkggraph.ID{rootID},
			)
			results <- struct {
				environment *Environment
				err         error
			}{environment: environment, err: err}
		}()
	}
	close(start)

	environments := make([]*Environment, 0, 2)
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		environments = append(environments, result.environment)
	}
	if environments[0].ID != environments[1].ID ||
		environments[0].RootPath != environments[1].RootPath {
		t.Fatalf("concurrent environments differ: %+v", environments)
	}
	for _, environment := range environments {
		if err := environment.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIdentityForClosureIncludesOrder(t *testing.T) {
	first := pkggraph.ID(strings.Repeat("a", pkggraph.IDLength))
	second := pkggraph.ID(strings.Repeat("b", pkggraph.IDLength))
	if identityForClosure([]pkggraph.ID{first, second}) ==
		identityForClosure([]pkggraph.ID{second, first}) {
		t.Fatal("environment identity ignores package overlay order")
	}
	if identityForClosure([]pkggraph.ID{first, second}) !=
		identityForClosure([]pkggraph.ID{first, second}) {
		t.Fatal("environment identity is not deterministic")
	}
}

func buildLauncherPackage(
	t *testing.T,
	launcher string,
) (*pkgstore.Store, *pkggraph.Graph, pkggraph.ID) {
	t.Helper()
	storePath := filepath.Join(t.TempDir(), "store")
	if err := pkgstore.Init(storePath); err != nil {
		t.Fatal(err)
	}
	store, err := pkgstore.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}

	sourcePath := filepath.Join(t.TempDir(), "launcher.tar")
	writeLauncherArchive(t, sourcePath, launcher)
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(source)
	sourceURL := (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(sourcePath),
	}).String()
	definition := pkggraph.Definition{
		Name:  "test-shell",
		Build: pkggraph.BuildSpec{Kind: pkggraph.BuildUnpack},
		Fetch: []pkggraph.Fetch{{
			Filename: "launcher.tar",
			SHA256:   hex.EncodeToString(digest[:]),
			URLs:     []string{sourceURL},
		}},
	}
	rootID, err := pkggraph.IDForDefinition(definition)
	if err != nil {
		t.Fatal(err)
	}
	graph := &pkggraph.Graph{
		Nodes: map[pkggraph.ID]pkggraph.Definition{rootID: definition},
		Roots: map[string]pkggraph.ID{"shell": rootID},
	}
	if _, err := store.BuildPackages(
		context.Background(),
		graph,
		rootID,
	); err != nil {
		t.Fatal(err)
	}
	return store, graph, rootID
}

func writeLauncherArchive(t *testing.T, destination string, launcher string) {
	t.Helper()
	file, err := os.OpenFile(
		destination,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	for _, header := range []*tar.Header{
		{
			Name:     "libexec/",
			Typeflag: tar.TypeDir,
			Mode:     0o755,
		},
		{
			Name:     LauncherPath,
			Typeflag: tar.TypeReg,
			Mode:     0o755,
			Size:     int64(len(launcher)),
		},
	} {
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := writer.Write([]byte(launcher)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
