//go:build !windows

package store

import (
	"fmt"
	"os"
	"syscall"
)

// LockFile acquires an exclusive advisory lock on the given file.
// Blocks until the lock is acquired or returns an error.
func LockFile(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock exclusive: %w", err)
	}
	return nil
}

// LockFileShared acquires a shared (non-exclusive) advisory lock on the given
// file. Multiple readers can hold shared locks concurrently, but writers need
// exclusive locks which block until all shared locks are released.
func LockFileShared(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil {
		return fmt.Errorf("flock shared: %w", err)
	}
	return nil
}

// UnlockFile releases the lock on the given file.
func UnlockFile(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err != nil {
		return fmt.Errorf("flock unlock: %w", err)
	}
	return nil
}
