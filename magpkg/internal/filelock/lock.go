// Package filelock provides context-aware advisory filesystem locks.
package filelock

import (
	"context"
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

const pollInterval = 25 * time.Millisecond

// Lock is an advisory lock associated with a filesystem path.
//
// Lock files may be unlinked while locked by cleanup code. Acquisition
// verifies that the inode it locked is still the inode named by path before
// returning.
type Lock struct {
	file *os.File
	path string
}

// AcquireExclusive waits for an exclusive lock on path.
func AcquireExclusive(ctx context.Context, path string) (*Lock, error) {
	return acquire(ctx, path, unix.LOCK_EX)
}

// AcquireShared waits for a shared lock on path.
func AcquireShared(ctx context.Context, path string) (*Lock, error) {
	return acquire(ctx, path, unix.LOCK_SH)
}

func acquire(ctx context.Context, path string, mode int) (*Lock, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, err
		}

		for {
			err = unix.Flock(int(file.Fd()), mode|unix.LOCK_NB)
			switch {
			case err == nil:
			case errors.Is(err, unix.EINTR):
				continue
			case errors.Is(err, unix.EWOULDBLOCK), errors.Is(err, unix.EAGAIN):
				timer := time.NewTimer(pollInterval)
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						<-timer.C
					}
					_ = file.Close()
					return nil, ctx.Err()
				case <-timer.C:
				}
				continue
			default:
				_ = file.Close()
				return nil, err
			}
			break
		}

		current, err := lockFileIsCurrent(file, path)
		if err != nil {
			return nil, errors.Join(err, unlockAndClose(file))
		}
		if current {
			return &Lock{file: file, path: path}, nil
		}
		if err := unlockAndClose(file); err != nil {
			return nil, err
		}
	}
}

// TryExclusive attempts to acquire an exclusive lock without waiting. Busy
// is true when another process currently holds the lock.
func TryExclusive(ctx context.Context, path string) (
	lock *Lock,
	busy bool,
	err error,
) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, false, err
		}

		for {
			err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			break
		}
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			_ = file.Close()
			return nil, true, nil
		}
		if err != nil {
			_ = file.Close()
			return nil, false, err
		}

		current, err := lockFileIsCurrent(file, path)
		if err != nil {
			return nil, false, errors.Join(err, unlockAndClose(file))
		}
		if current {
			return &Lock{file: file, path: path}, false, nil
		}
		if err := unlockAndClose(file); err != nil {
			return nil, false, err
		}
	}
}

func lockFileIsCurrent(file *os.File, path string) (bool, error) {
	lockedInfo, err := file.Stat()
	if err != nil {
		return false, err
	}
	pathInfo, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(lockedInfo, pathInfo), nil
}

func unlockAndClose(file *os.File) error {
	unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN)
	closeErr := file.Close()
	return errors.Join(unlockErr, closeErr)
}

// Unlink removes the lock path while retaining the lock on its inode.
func (lock *Lock) Unlink() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	current, err := lockFileIsCurrent(lock.file, lock.path)
	if err != nil {
		return err
	}
	if !current {
		if _, err := os.Stat(lock.path); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return err
		}
		return errors.New("lock path changed while locked")
	}
	if err := os.Remove(lock.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Close releases the lock.
func (lock *Lock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	err := unlockAndClose(lock.file)
	lock.file = nil
	return err
}
