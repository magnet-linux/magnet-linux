package pkgstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestFetchLockDetectsReplacementInode(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), strings.Repeat("0", 64)+fetchLockSuffix)
	oldFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer oldFile.Close()
	if err := unix.Flock(int(oldFile.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer unix.Flock(int(oldFile.Fd()), unix.LOCK_UN) //nolint:errcheck

	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()

	current, err := fetchLockFileIsCurrent(oldFile, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if current {
		t.Fatal("unlinked lock file was considered current")
	}
	current, err = fetchLockFileIsCurrent(replacement, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !current {
		t.Fatal("replacement lock file was not considered current")
	}

	oldLock := &fetchLock{file: oldFile, path: lockPath}
	if err := oldLock.Unlink(); err == nil {
		t.Fatal("stale lock unlinked the replacement path")
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("replacement lock was removed: %v", err)
	}
}
