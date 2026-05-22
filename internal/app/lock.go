package app

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// AcquireSingleInstanceLock takes an exclusive, non-blocking advisory lock on
// <stateDir>/model-loader.lock. It stops a second process from racing on the
// shared state files (instances.json / proxy-state.json).
//
// Returns (release, true, nil) when the lock was acquired — call release on
// exit to drop it. Returns (nil, false, nil) when another live process already
// holds the lock. The kernel releases the lock automatically if this process
// dies, so a crash never leaves a stale lock behind.
func AcquireSingleInstanceLock(stateDir string) (release func(), acquired bool, err error) {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, false, fmt.Errorf("mkdir state dir: %w", err)
	}
	path := filepath.Join(stateDir, "model-loader.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("flock: %w", err)
	}
	release = func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
	return release, true, nil
}
