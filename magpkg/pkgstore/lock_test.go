package pkgstore

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSharedLocksExcludeCleanupLock(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "package.lock")
	first, err := acquireSharedLock(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := acquireSharedLock(context.Background(), lockPath)
	if err != nil {
		_ = first.Close()
		t.Fatal(err)
	}

	if lock, busy, err := tryLock(context.Background(), lockPath); err != nil {
		t.Fatal(err)
	} else if !busy {
		_ = lock.Close()
		t.Fatal("exclusive cleanup lock acquired while shared locks were held")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}

	lock, busy, err := tryLock(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if busy {
		t.Fatal("exclusive cleanup lock remained busy after shared locks closed")
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}
