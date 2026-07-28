package pkgstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andrewchambers/magnet-linux/magpkg/pkggraph"
)

func TestBinaryCacheHitSkipsSourcesAndBuildDependencies(t *testing.T) {
	remoteStore := openTestStore(t)
	buildDependency := pkggraph.Definition{
		Name:  "build-tool",
		Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone},
	}
	buildDependencyID := testID(t, buildDependency)
	var sourceRequests atomic.Int64
	sourceServer := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		sourceRequests.Add(1)
		http.Error(writer, "source must not be fetched", http.StatusInternalServerError)
	}))
	defer sourceServer.Close()
	root := pkggraph.Definition{
		Name: "root",
		Build: pkggraph.BuildSpec{
			Kind:   pkggraph.BuildScript,
			Script: "exit 99",
		},
		BuildDeps: []pkggraph.ID{buildDependencyID},
		Fetch: []pkggraph.Fetch{{
			Filename: "source",
			SHA256:   strings.Repeat("0", sha256.Size*2),
			URLs:     []string{sourceServer.URL},
		}},
	}
	rootID := testID(t, root)
	wantArchive := writeBinaryCacheTestArtifact(
		t,
		remoteStore,
		rootID,
		root.Name,
		"bin/root",
		"from cache\n",
	)

	handler, err := NewBinaryCacheHandler(
		remoteStore,
		BinaryCacheHandlerOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	var requestMutex sync.Mutex
	requests := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		requestMutex.Lock()
		requests[request.URL.Path]++
		requestMutex.Unlock()
		handler.ServeHTTP(writer, request)
	}))
	defer server.Close()

	localStore := openTestStoreWithOptions(t, OpenOptions{
		BinaryCacheURL: server.URL,
		HTTPClient:     server.Client(),
	})
	graph := &pkggraph.Graph{
		Nodes: map[pkggraph.ID]pkggraph.Definition{
			buildDependencyID: buildDependency,
			rootID:            root,
		},
		Roots: map[string]pkggraph.ID{"root": rootID},
	}
	artifacts, err := localStore.BuildPackagesWithOptions(
		context.Background(),
		graph,
		[]pkggraph.ID{rootID},
		BuildOptions{NoBuild: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 || artifacts[0].ID != rootID {
		t.Fatalf("artifacts = %+v, want root %s", artifacts, rootID)
	}
	gotArchive, err := os.ReadFile(artifacts[0].StorePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotArchive) != string(wantArchive) {
		t.Fatal("substituted artifact differs from served artifact")
	}
	if sourceRequests.Load() != 0 {
		t.Fatalf("source requests = %d, want 0", sourceRequests.Load())
	}
	requestMutex.Lock()
	defer requestMutex.Unlock()
	rootPath := binaryCachePackagesPath +
		packageArtifactFilename(rootID, root.Name)
	buildPath := binaryCachePackagesPath +
		packageArtifactFilename(buildDependencyID, buildDependency.Name)
	if requests[rootPath] != 1 {
		t.Fatalf("root cache requests = %d, want 1", requests[rootPath])
	}
	if requests[buildPath] != 0 {
		t.Fatalf(
			"build-dependency cache requests = %d, want 0",
			requests[buildPath],
		)
	}
}

func TestBinaryCacheArtifactURLPreservesBasePath(t *testing.T) {
	cache, err := newBinaryCacheClient(
		"https://cache.example/base/",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	packageID := pkggraph.ID(strings.Repeat("a", pkggraph.IDLength))
	artifactURL, err := cache.artifactURL(packageID, "package+name")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://cache.example/base/v1/packages/package+name-" +
		string(packageID) + ".tar.zst"
	if artifactURL.String() != want {
		t.Fatalf("artifact URL = %q, want %q", artifactURL, want)
	}
}

func TestBinaryCacheMissFallsBackToLocalBuild(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		requests.Add(1)
		http.NotFound(writer, request)
	}))
	defer server.Close()
	store := openTestStoreWithOptions(t, OpenOptions{
		BinaryCacheURL: server.URL,
		HTTPClient:     server.Client(),
	})
	definition := pkggraph.Definition{
		Name:  "empty",
		Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone},
	}
	packageID := testID(t, definition)
	graph := &pkggraph.Graph{
		Nodes: map[pkggraph.ID]pkggraph.Definition{packageID: definition},
	}

	artifacts, err := store.BuildPackages(
		context.Background(),
		graph,
		packageID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifact count = %d, want 1", len(artifacts))
	}
	if requests.Load() != 1 {
		t.Fatalf("binary-cache requests = %d, want 1", requests.Load())
	}
	if err := validatePackageArchive(
		context.Background(),
		artifacts[0].StorePath,
	); err != nil {
		t.Fatalf("locally built artifact is invalid: %v", err)
	}
}

func TestNoBuildCacheMissDoesNotFetchSources(t *testing.T) {
	cacheServer := httptest.NewServer(http.NotFoundHandler())
	defer cacheServer.Close()
	var sourceRequests atomic.Int64
	sourceContents := []byte("source")
	sourceDigest := sha256.Sum256(sourceContents)
	sourceServer := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		sourceRequests.Add(1)
		_, _ = writer.Write(sourceContents)
	}))
	defer sourceServer.Close()

	store := openTestStoreWithOptions(t, OpenOptions{
		BinaryCacheURL: cacheServer.URL,
		HTTPClient:     cacheServer.Client(),
	})
	definition := pkggraph.Definition{
		Name:  "uncached",
		Build: pkggraph.BuildSpec{Kind: pkggraph.BuildUnpack},
		Fetch: []pkggraph.Fetch{{
			Filename: "source.tar",
			SHA256:   hex.EncodeToString(sourceDigest[:]),
			URLs:     []string{sourceServer.URL},
		}},
	}
	packageID := testID(t, definition)
	graph := &pkggraph.Graph{
		Nodes: map[pkggraph.ID]pkggraph.Definition{packageID: definition},
	}

	_, err := store.BuildPackagesWithOptions(
		context.Background(),
		graph,
		[]pkggraph.ID{packageID},
		BuildOptions{NoBuild: true},
	)
	if err == nil || !strings.Contains(err.Error(), "local builds are disabled") {
		t.Fatalf("BuildPackagesWithOptions() error = %v, want no-build miss", err)
	}
	if sourceRequests.Load() != 0 {
		t.Fatalf("source requests = %d, want 0", sourceRequests.Load())
	}
	if available, err := store.packageAvailable(packageID, definition.Name); err != nil {
		t.Fatal(err)
	} else if available {
		t.Fatal("no-build miss published an artifact")
	}
}

func TestInvalidBinaryCacheArtifactIsNotPublished(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("not an archive"))
	}))
	defer server.Close()
	store := openTestStoreWithOptions(t, OpenOptions{
		BinaryCacheURL: server.URL,
		HTTPClient:     server.Client(),
	})
	definition := pkggraph.Definition{
		Name:  "invalid",
		Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone},
	}
	packageID := testID(t, definition)
	graph := &pkggraph.Graph{
		Nodes: map[pkggraph.ID]pkggraph.Definition{packageID: definition},
	}

	_, err := store.BuildPackages(
		context.Background(),
		graph,
		packageID,
	)
	if err == nil || !strings.Contains(err.Error(), "validate binary-cache artifact") {
		t.Fatalf("BuildPackages() error = %v, want validation error", err)
	}
	if available, inspectErr := store.packageAvailable(
		packageID,
		definition.Name,
	); inspectErr != nil {
		t.Fatal(inspectErr)
	} else if available {
		t.Fatal("invalid binary-cache artifact was published")
	}
}

func TestConcurrentBinaryCacheSubstitutionDownloadsOnce(t *testing.T) {
	remoteStore := openTestStore(t)
	definition := pkggraph.Definition{
		Name:  "shared",
		Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone},
	}
	packageID := testID(t, definition)
	writeBinaryCacheTestArtifact(
		t,
		remoteStore,
		packageID,
		definition.Name,
		"shared",
		"shared\n",
	)
	handler, err := NewBinaryCacheHandler(
		remoteStore,
		BinaryCacheHandlerOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		requests.Add(1)
		handler.ServeHTTP(writer, request)
	}))
	defer server.Close()
	store := openTestStoreWithOptions(t, OpenOptions{
		BinaryCacheURL: server.URL,
		HTTPClient:     server.Client(),
	})
	graph := &pkggraph.Graph{
		Nodes: map[pkggraph.ID]pkggraph.Definition{packageID: definition},
	}

	start := make(chan struct{})
	errorsFound := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := store.BuildPackagesWithOptions(
				context.Background(),
				graph,
				[]pkggraph.ID{packageID},
				BuildOptions{NoBuild: true},
			)
			errorsFound <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-errorsFound; err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("binary-cache requests = %d, want 1", requests.Load())
	}
}

func TestBinaryCacheHandlerProtocolAndTouching(t *testing.T) {
	store := openTestStore(t)
	definition := pkggraph.Definition{
		Name:  "served",
		Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone},
	}
	packageID := testID(t, definition)
	archive := writeBinaryCacheTestArtifact(
		t,
		store,
		packageID,
		definition.Name,
		"served",
		"served\n",
	)
	artifactPath := store.packageArtifactPath(packageID, definition.Name)
	handler, err := NewBinaryCacheHandler(
		store,
		BinaryCacheHandlerOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := server.Client().Get(server.URL + binaryCacheInfoPath)
	if err != nil {
		t.Fatal(err)
	}
	info, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK ||
		string(info) != "{\"format\":\"magpkg-binary-cache\",\"version\":1}\n" {
		t.Fatalf("cache info = %s %q", response.Status, info)
	}

	old := time.Unix(10, 0)
	if err := os.Chtimes(artifactPath, old, old); err != nil {
		t.Fatal(err)
	}
	artifactURL := server.URL + binaryCachePackagesPath +
		packageArtifactFilename(packageID, definition.Name)
	response, err = server.Client().Head(artifactURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("HEAD status = %s", response.Status)
	}
	assertBinaryCacheTestModTime(t, artifactPath, old)

	response, err = server.Client().Get(artifactURL)
	if err != nil {
		t.Fatal(err)
	}
	gotArchive, readErr := io.ReadAll(response.Body)
	closeErr = response.Body.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %s", response.Status)
	}
	if string(gotArchive) != string(archive) {
		t.Fatal("served artifact bytes differ")
	}
	infoAfterGet, err := os.Stat(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	if !infoAfterGet.ModTime().After(old) {
		t.Fatalf(
			"GET artifact mtime = %v, want after %v",
			infoAfterGet.ModTime(),
			old,
		)
	}

	noTouchHandler, err := NewBinaryCacheHandler(
		store,
		BinaryCacheHandlerOptions{DisableTouch: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(artifactPath, old, old); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, artifactURL, nil)
	recorder := httptest.NewRecorder()
	noTouchHandler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("no-touch GET status = %d", recorder.Code)
	}
	assertBinaryCacheTestModTime(t, artifactPath, old)

	request = httptest.NewRequest(http.MethodPost, artifactURL, nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", recorder.Code)
	}
}

func TestBinaryCacheHandlerRejectsSymlinkArtifact(t *testing.T) {
	store := openTestStore(t)
	definition := pkggraph.Definition{
		Name:  "symlink",
		Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone},
	}
	packageID := testID(t, definition)
	artifactPath := store.packageArtifactPath(packageID, definition.Name)
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, artifactPath); err != nil {
		t.Fatal(err)
	}
	handler, err := NewBinaryCacheHandler(
		store,
		BinaryCacheHandlerOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		binaryCachePackagesPath+
			packageArtifactFilename(packageID, definition.Name),
		nil,
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("symlink artifact status = %d, want 404", recorder.Code)
	}
}

func TestBinaryCacheHandlerOpenDescriptorSurvivesUnlink(t *testing.T) {
	store := openTestStore(t)
	definition := pkggraph.Definition{
		Name:  "open-descriptor",
		Build: pkggraph.BuildSpec{Kind: pkggraph.BuildNone},
	}
	packageID := testID(t, definition)
	artifactPath := store.packageArtifactPath(packageID, definition.Name)
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := bytes.Repeat([]byte("artifact block\n"), 128*1024)
	if err := os.WriteFile(artifactPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := NewBinaryCacheHandler(
		store,
		BinaryCacheHandlerOptions{DisableTouch: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		binaryCachePackagesPath+
			packageArtifactFilename(packageID, definition.Name),
		nil,
	)
	writer := newBlockingBinaryCacheResponseWriter()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(writer, request)
		close(done)
	}()
	select {
	case <-writer.started:
	case <-time.After(5 * time.Second):
		t.Fatal("binary-cache response did not start")
	}
	if err := os.Remove(artifactPath); err != nil {
		t.Fatal(err)
	}
	close(writer.resume)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("binary-cache response did not finish")
	}
	if writer.status != http.StatusOK {
		t.Fatalf("response status = %d, want 200", writer.status)
	}
	if !bytes.Equal(writer.body.Bytes(), contents) {
		t.Fatalf(
			"served %d bytes after unlink, want %d",
			writer.body.Len(),
			len(contents),
		)
	}
}

func openTestStoreWithOptions(t *testing.T, options OpenOptions) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "store")
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	store, err := OpenWithOptions(path, options)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func writeBinaryCacheTestArtifact(
	t *testing.T,
	store *Store,
	packageID pkggraph.ID,
	packageName string,
	relativePath string,
	contents string,
) []byte {
	t.Helper()
	root := t.TempDir()
	target := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	artifactPath := store.packageArtifactPath(packageID, packageName)
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := createPackageArchive(
		context.Background(),
		root,
		artifactPath,
	); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

func assertBinaryCacheTestModTime(
	t *testing.T,
	path string,
	want time.Time,
) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(want) {
		t.Fatalf("artifact mtime = %v, want %v", info.ModTime(), want)
	}
}

type blockingBinaryCacheResponseWriter struct {
	header  http.Header
	status  int
	body    bytes.Buffer
	started chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func newBlockingBinaryCacheResponseWriter() *blockingBinaryCacheResponseWriter {
	return &blockingBinaryCacheResponseWriter{
		header:  make(http.Header),
		status:  http.StatusOK,
		started: make(chan struct{}),
		resume:  make(chan struct{}),
	}
}

func (writer *blockingBinaryCacheResponseWriter) Header() http.Header {
	return writer.header
}

func (writer *blockingBinaryCacheResponseWriter) WriteHeader(status int) {
	writer.status = status
}

func (writer *blockingBinaryCacheResponseWriter) Write(data []byte) (int, error) {
	writer.once.Do(func() {
		close(writer.started)
		<-writer.resume
	})
	return writer.body.Write(data)
}
