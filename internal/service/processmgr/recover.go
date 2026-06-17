package processmgr

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/procutil"
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
	survivors, dropped, total, err := m.refreshTracked("reconcile")
	if err != nil {
		return err
	}
	if err := saveRegistry(m.registryPath, survivors); err != nil {
		m.logger.Error("reconcile_failed", "step", "save", "err", err)
		return fmt.Errorf("rewrite registry: %w", err)
	}
	m.logger.Info("reconcile_done",
		"kept", len(survivors), "dropped", dropped, "total", total)
	return nil
}

// RefreshFromDisk re-reads instances.json and replaces the in-memory tracked
// set with the live subset, WITHOUT writing the registry back. Safe to call
// periodically from observer processes (the TUI): persisting drops is the
// owning process's job — a concurrent writer's fresh launch must not be
// erased by an observer's stale snapshot.
func (m *fsManager) RefreshFromDisk() error {
	survivors, dropped, total, err := m.refreshTracked("refresh")
	if err != nil {
		return err
	}
	m.logger.Info("refresh_done",
		"kept", len(survivors), "dropped", dropped, "total", total)
	return nil
}

// refreshTracked loads the on-disk registry, filters it down to the entries
// whose PID is alive AND still names the expected binary, and replaces
// m.tracked wholesale with the live subset. It returns the survivors so the
// caller decides whether to persist them (Reconcile) or not (RefreshFromDisk).
// event prefixes the structured log records ("reconcile" or "refresh").
func (m *fsManager) refreshTracked(event string) (survivors []domain.RunningInstance, dropped, total int, err error) {
	loaded, err := loadRegistry(m.registryPath)
	if err != nil {
		m.logger.Error(event+"_failed", "step", "load", "err", err)
		return nil, 0, 0, err
	}
	survivors = make([]domain.RunningInstance, 0, len(loaded))
	tracked := make(map[int]domain.RunningInstance, len(loaded))
	for _, ri := range loaded {
		binary := ri.BinaryPath
		if binary == "" {
			binary = m.defaultBinary
		}
		// For compound command strings (e.g. "python -m sglang.launch_server"),
		// extract the executable token before taking basename.
		exeToken := exeFromBinaryPath(binary)
		if !pidAliveAndNameMatches(ri.PID, filepath.Base(exeToken)) {
			if !pidAliveAndNameMatches(ri.PID, filepath.Base(m.defaultBinary)) {
				m.logger.Info(event+"_dropped",
					"pid", ri.PID, "profile_id", ri.ProfileID,
					"reason", "pid_or_comm_mismatch")
				dropped++
				continue
			}
		}
		// For compound commands, also verify the cmdline contains the expected
		// module/prefix args to avoid recovering an unrelated process.
		if exeToken != binary {
			cmdlineToken := strings.TrimSpace(binary[len(exeToken):])
			if cmdlineToken != "" && !pidAliveAndCmdlineContains(ri.PID, cmdlineToken) {
				m.logger.Info(event+"_dropped",
					"pid", ri.PID, "profile_id", ri.ProfileID,
					"reason", "cmdline_mismatch")
				dropped++
				continue
			}
		}
		m.logger.Info(event+"_kept",
			"pid", ri.PID, "profile_id", ri.ProfileID, "binary", binary)
		survivors = append(survivors, ri)
		tracked[ri.PID] = ri
	}

	m.mu.Lock()
	m.tracked = tracked
	m.mu.Unlock()

	return survivors, dropped, len(loaded), nil
}

// pidAliveAndNameMatches returns true iff pid is alive AND the basename of
// /proc/<pid>/comm contains expectedComm. Reads /proc directly (Linux).
//
// Linux truncates /proc/<pid>/comm to TASK_COMM_LEN-1 (15 bytes), so
// expectedComm is truncated to the same length before comparison.
func pidAliveAndNameMatches(pid int, expectedComm string) bool {
	if !procutil.Alive(pid) {
		return false
	}
	commBytes, err := os.ReadFile(filepath.Join("/proc", fmt.Sprintf("%d", pid), "comm"))
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			return true
		}
		return false
	}
	comm := strings.TrimSpace(string(commBytes))
	const taskCommLen = 15 // Linux TASK_COMM_LEN - 1
	if len(expectedComm) > taskCommLen {
		expectedComm = expectedComm[:taskCommLen]
	}
	return strings.Contains(comm, expectedComm)
}

// pidAliveAndCmdlineContains returns true iff pid is alive AND
// /proc/<pid>/cmdline contains the given token. Used for compound
// commands (e.g. "python -m sglang.launch_server") where /proc/comm
// only shows the executable basename.
func pidAliveAndCmdlineContains(pid int, token string) bool {
	if !procutil.Alive(pid) {
		return false
	}
	cmdlineBytes, err := os.ReadFile(filepath.Join("/proc", fmt.Sprintf("%d", pid), "cmdline"))
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			return true
		}
		return false
	}
	// cmdline uses null bytes as separators; join with spaces for matching.
	cmdline := strings.Join(strings.Split(string(cmdlineBytes), "\x00"), " ")
	return strings.Contains(cmdline, token)
}
