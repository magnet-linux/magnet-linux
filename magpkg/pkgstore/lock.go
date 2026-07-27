package pkgstore

import (
	"context"
	"time"

	"github.com/andrewchambers/magnet-linux/magpkg/internal/filelock"
)

const lockPollInterval = 25 * time.Millisecond

type fileLock = filelock.Lock

func acquireLock(ctx context.Context, path string) (*fileLock, error) {
	return filelock.AcquireExclusive(ctx, path)
}

func acquireSharedLock(ctx context.Context, path string) (*fileLock, error) {
	return filelock.AcquireShared(ctx, path)
}

// tryLock attempts to acquire path without waiting. busy is true when another
// process currently holds the lock.
func tryLock(ctx context.Context, path string) (*fileLock, bool, error) {
	return filelock.TryExclusive(ctx, path)
}
