package fsx

import (
	"os"
	"syscall"
)

// WithFileLock runs fn while holding an exclusive blocking advisory flock on
// lockPath (created 0644 if absent). Lock acquisition failures are non-fatal:
// fn runs unserialized rather than failing on an environment quirk — same
// tolerance as the CLI profile lock (internal/cli/filelock.go).
func WithFileLock(lockPath string, fn func() error) error {
	if lockPath == "" {
		return fn()
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fn()
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fn()
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return fn()
}
