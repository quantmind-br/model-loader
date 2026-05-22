package cli

import (
	"os"
	"path/filepath"
	"syscall"
)

// withProfileLock serializes a read-modify-write on a single profile across
// processes via an advisory flock on a sibling ".<id>.lock" file in the
// profiles directory. It is CLI-local: the TUI does not take this lock, so
// CLI-vs-TUI safety still relies on the store's atomic writes and the TUI's
// reload. This closes the CLI-vs-CLI race on the same profile.
//
// Lock-file creation or flock failures are non-fatal: the mutation proceeds
// unserialized rather than being blocked by an environment quirk.
func withProfileLock(profilesDir, id string, fn func() error) error {
	if profilesDir == "" || id == "" {
		return fn()
	}
	lockPath := filepath.Join(profilesDir, "."+id+".lock")
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
