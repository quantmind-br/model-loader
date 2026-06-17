// Package procutil holds small process-related helpers shared across the
// service layer. It deliberately imports nothing from the service packages so
// it can be used by processmgr, downloadmgr, and proxysupervisor without
// creating an inter-service import cycle.
package procutil

import (
	"errors"
	"syscall"
)

// Alive reports whether a process with the given PID exists and is signalable.
// It sends signal 0, which performs error checking without delivering a signal.
// EPERM (the process exists but is owned by another user) counts as alive.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}
