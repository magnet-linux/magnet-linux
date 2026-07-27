package pkgstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildUntarPackage(t *testing.T) {
	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "hello"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sourceArchive := filepath.Join(t.TempDir(), "source.tar.zst")
	if err := createPackageArchive(context.Background(), sourceRoot, sourceArchive); err != nil {
		t.Fatal(err)
	}
	archiveContents, err := os.ReadFile(sourceArchive)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archiveContents)
	definition := PackageDefinition{
		Name:  "bootstrap",
		Build: "untar",
		Fetch: []Fetch{{
			Filename: "source.tar.zst",
			SHA256:   hex.EncodeToString(digest[:]),
			URLs: []string{(&url.URL{
				Scheme: "file",
				Path:   filepath.ToSlash(sourceArchive),
			}).String()},
		}},
	}
	packageID := testPackageID(t, definition)
	graph := &PackageGraph{
		Nodes: map[PackageID]PackageDefinition{packageID: definition},
		Roots: map[string]PackageID{"bootstrap": packageID},
	}
	store := openTestStore(t)

	pkg, err := store.Build(context.Background(), graph, packageID)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.ID != packageID ||
		pkg.StorePath != store.packageArtifactPath(packageID, definition.Name) {
		t.Fatalf("built package = %+v", pkg)
	}
	extracted := t.TempDir()
	if err := extractArchive(context.Background(), pkg.StorePath, extracted); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(extracted, "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "hello\n" {
		t.Fatalf("built contents = %q, want %q", contents, "hello\n")
	}
}

func TestBuildInsideBootstrapSandbox(t *testing.T) {
	if os.Getenv("MAGPKG_TEST_BOOTSTRAP") == "" {
		t.Skip("set MAGPKG_TEST_BOOTSTRAP=1 to run the bootstrap sandbox integration test")
	}
	bootstrapArchive, err := filepath.Abs(
		filepath.Join("..", "..", "bootstrap", "out", "bootstrap.tar.zst"),
	)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapDigest, err := sha256File(bootstrapArchive)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("bootstrap/out/bootstrap.tar.zst has not been built")
	}
	if err != nil {
		t.Fatal(err)
	}

	bootstrapDefinition := PackageDefinition{
		Name:  "bootstrap",
		Build: "untar",
		Fetch: []Fetch{{
			Filename: "bootstrap.tar.zst",
			SHA256:   hex.EncodeToString(bootstrapDigest[:]),
			URLs: []string{(&url.URL{
				Scheme: "file",
				Path:   filepath.ToSlash(bootstrapArchive),
			}).String()},
		}},
	}
	bootstrapID := testPackageID(t, bootstrapDefinition)
	helloDefinition := PackageDefinition{
		Name: "hello",
		Build: "" +
			"test \"$PATH\" = /bin\n" +
			"test \"$HOME\" = /build\n" +
			"test \"$MAGPKG_TEST_VALUE\" = 'custom value'\n" +
			"test \"$BUILD_PARALLELISM\" = 3\n" +
			"printf 'hello on stdout\\n'\n" +
			"printf 'hello on stderr\\n' >&2\n" +
			"printf 'built inside bootstrap\\n' > /out/hello\n",
		BuildEnv: map[string]string{
			"PATH":              "/bin",
			"MAGPKG_TEST_VALUE": "custom value",
		},
		BuildDeps: []PackageID{bootstrapID},
	}
	helloID := testPackageID(t, helloDefinition)
	graph := &PackageGraph{
		Nodes: map[PackageID]PackageDefinition{
			bootstrapID: bootstrapDefinition,
			helloID:     helloDefinition,
		},
		Roots: map[string]PackageID{"hello": helloID},
	}
	store := openTestStore(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	pkg, err := store.BuildWithOptions(
		context.Background(),
		graph,
		helloID,
		BuildOptions{
			BuildParallelism: 3,
			Stdout:           &stdout,
			Stderr:           &stderr,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	prefix := buildOutputPrefix(PlannedPackage{
		ID:         helloID,
		Definition: helloDefinition,
	})
	if got, want := stdout.String(), prefix+"hello on stdout\n"; got != want {
		t.Fatalf("build stdout = %q, want %q", got, want)
	}
	if got, want := stderr.String(), prefix+"hello on stderr\n"; got != want {
		t.Fatalf("build stderr = %q, want %q", got, want)
	}

	extracted := t.TempDir()
	if err := extractArchive(context.Background(), pkg.StorePath, extracted); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(extracted, "hello"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "built inside bootstrap\n" {
		t.Fatalf("built contents = %q", contents)
	}
}

func sha256File(path string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	file, err := os.Open(path)
	if err != nil {
		return digest, err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return digest, err
	}
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
}
