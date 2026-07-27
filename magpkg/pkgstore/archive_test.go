package pkgstore

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPackageArchiveIsDeterministicAndPreservesSymlinks(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "bin", "tool"), []byte("tool\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/lib/libc.so", filepath.Join(source, "loader")); err != nil {
		t.Fatal(err)
	}

	firstArchive := filepath.Join(t.TempDir(), "first.tar.zst")
	secondArchive := filepath.Join(t.TempDir(), "second.tar.zst")
	if err := createPackageArchive(context.Background(), source, firstArchive); err != nil {
		t.Fatal(err)
	}
	if err := createPackageArchive(context.Background(), source, secondArchive); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(firstArchive)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(secondArchive)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("identical package outputs produced different archives")
	}

	extracted := t.TempDir()
	if err := extractArchive(context.Background(), firstArchive, extracted); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(extracted, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "tool\n" {
		t.Fatalf("extracted contents = %q, want %q", contents, "tool\n")
	}
	target, err := os.Readlink(filepath.Join(extracted, "loader"))
	if err != nil {
		t.Fatal(err)
	}
	if target != "/lib/libc.so" {
		t.Fatalf("symlink target = %q, want %q", target, "/lib/libc.so")
	}
}

func TestExtractArchiveRejectsParentTraversal(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "malicious.tar")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	if err := writer.WriteHeader(&tar.Header{
		Name:     "../escaped",
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	destination := t.TempDir()
	if err := extractArchive(context.Background(), archivePath, destination); err == nil {
		t.Fatal("extractArchive accepted a path that escaped its destination")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(destination), "escaped")); !os.IsNotExist(err) {
		t.Fatalf("archive created a file outside its destination: %v", err)
	}
}
