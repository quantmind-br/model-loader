package processmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// Reconcile reads the on-disk registry, validates each entry against the
// running process table, drops zombies, and updates the registry.
//
// An entry survives only if BOTH:
//   - the PID is alive (signal 0 succeeds), AND
//   - /proc/<pid>/comm contains the basename of the binary stored on the
//     instance, falling back to the manager binary (default: "llama-server"). This avoids
//     mistaking a recycled PID for a live server.
//
// Must be called at boot, before any Launch/Kill. It rewrites m.tracked
// wholesale and saves the registry without coordinating with concurrent
// callers — concurrent Launch/Kill would race the saveRegistry write.
//
// Linux-only. Other platforms: this method drops every entry (safe default).
func (m *fsManager) Reconcile() error {
	loaded, err := loadRegistry(m.registryPath)
	if err != nil {
		return err
	}
	survivors := make([]domain.RunningInstance, 0, len(loaded))
	tracked := make(map[int]domain.RunningInstance, len(loaded))
	for _, ri := range loaded {
		binary := ri.BinaryPath
		if binary == "" {
			binary = m.defaultBinary
		}
		if !pidAliveAndNameMatches(ri.PID, filepath.Base(binary)) {
			continue
		}
		survivors = append(survivors, ri)
		tracked[ri.PID] = ri
	}

	m.mu.Lock()
	m.tracked = tracked
	m.mu.Unlock()

	if err := saveRegistry(m.registryPath, survivors); err != nil {
		return fmt.Errorf("rewrite registry: %w", err)
	}
	return nil
}

// pidAliveAndNameMatches returns true iff pid is alive AND the basename of
// /proc/<pid>/comm contains expectedComm. Reads /proc directly (Linux).
//
// Linux truncates /proc/<pid>/comm to TASK_COMM_LEN-1 (15 bytes), so
// expectedComm is truncated to the same length before comparison.
func pidAliveAndNameMatches(pid int, expectedComm string) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	commBytes, err := os.ReadFile(filepath.Join("/proc", fmt.Sprintf("%d", pid), "comm"))
	if err != nil {
		return false
	}
	comm := strings.TrimSpace(string(commBytes))
	const taskCommLen = 15 // Linux TASK_COMM_LEN - 1
	if len(expectedComm) > taskCommLen {
		expectedComm = expectedComm[:taskCommLen]
	}
	return strings.Contains(comm, expectedComm)
}
