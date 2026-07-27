package filelock

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLockDetectsReplacementInode(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "entry.lock")
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

	current, err := lockFileIsCurrent(oldFile, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if current {
		t.Fatal("unlinked lock file was considered current")
	}
	current, err = lockFileIsCurrent(replacement, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !current {
		t.Fatal("replacement lock file was not considered current")
	}

	oldLock := &Lock{file: oldFile, path: lockPath}
	if err := oldLock.Unlink(); err == nil {
		t.Fatal("stale lock unlinked the replacement path")
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("replacement lock was removed: %v", err)
	}
}
