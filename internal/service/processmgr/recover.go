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

// kindLegacyToken maps a backend kind to a cmdline substring that identifies a
// live process of that kind. Used ONLY for legacy registry entries (written
// before StartTicks existed) whose wrapper-exec comm no longer matches the
// wrapper basename (audit A1: vllm-serve.sh → comm "vllm"). New launches carry
// StartTicks and never reach this path.
var kindLegacyToken = map[domain.BackendKind]string{
	domain.BackendKindLlamaServer:  "llama-server",
	domain.BackendKindBeeLlamaCpp:  "llama-server",
	domain.BackendKindBuunLlamaCpp: "llama-server",
	domain.BackendKindIkLlamaCpp:   "llama-server",
	domain.BackendKindVLLM:         "vllm",
	domain.BackendKindSGLang:       "sglang",
	domain.BackendKindDFlash:       "dflash",
	domain.BackendKindUnsloth:      "unsloth",
	domain.BackendKindTabby:        "main.py",
	domain.BackendKindTokenSpeed:   "tokenspeed",
}

// entryAlive reports whether a registry entry still refers to the process it
// recorded. StartTicks (when present) is authoritative — comm/cmdline
// heuristics apply only to legacy entries written before StartTicks existed
// (they mis-drop wrapper-exec backends whose comm no longer matches the
// wrapper basename — audit A1).
func (m *fsManager) entryAlive(ri domain.RunningInstance) bool {
	if ri.StartTicks != 0 {
		return procutil.SameProcess(ri.PID, ri.StartTicks)
	}
	// Legacy entry: pre-StartTicks comm/cmdline heuristics.
	binary := ri.BinaryPath
	if binary == "" {
		binary = m.defaultBinary
	}
	// For compound command strings (e.g. "python -m sglang.launch_server"),
	// extract the executable token before taking basename.
	exeToken := exeFromBinaryPath(binary)
	if !pidAliveAndNameMatches(ri.PID, filepath.Base(exeToken)) &&
		!pidAliveAndNameMatches(ri.PID, filepath.Base(m.defaultBinary)) {
		// Name checks failed. Accept a live wrapper-exec backend by its
		// kind-specific cmdline token — keeps vLLM/SGLang alive across the
		// first post-upgrade reconcile (audit A1).
		if ri.Kind != "" {
			if tok, ok := kindLegacyToken[ri.Kind]; ok {
				return pidAliveAndCmdlineContains(ri.PID, tok)
			}
		}
		return false
	}
	// For compound commands, also verify the cmdline contains the expected
	// module/prefix args to avoid recovering an unrelated process.
	if exeToken != binary {
		cmdlineToken := strings.TrimSpace(binary[len(exeToken):])
		if cmdlineToken != "" && !pidAliveAndCmdlineContains(ri.PID, cmdlineToken) {
			return false
		}
	}
	return true
}

// Reconcile reads the on-disk registry, validates each entry via entryAlive,
// drops zombies, and rewrites the registry. It is the 6th out-of-m.mu
// mutateRegistry callsite (flock-guarded delta): validation happens inside the
// lock so decisions apply to fresh data and a concurrent writer's fresh launch
// is never erased (audit A7). Must run at boot, before Launch/Kill.
//
// Linux-only. Other platforms: entryAlive drops every entry (safe default).
func (m *fsManager) Reconcile() error {
	var survivors map[int]domain.RunningInstance
	var dropped, total int
	err := mutateRegistry(m.registryPath, func(reg map[int]domain.RunningInstance) {
		total = len(reg)
		for pid, ri := range reg {
			if m.entryAlive(ri) {
				m.logger.Info("reconcile_kept",
					"pid", ri.PID, "profile_id", ri.ProfileID)
				continue
			}
			m.logger.Info("reconcile_dropped",
				"pid", ri.PID, "profile_id", ri.ProfileID,
				"reason", "pid_recycled_or_dead")
			delete(reg, pid)
			dropped++
		}
		survivors = make(map[int]domain.RunningInstance, len(reg))
		for pid, ri := range reg {
			survivors[pid] = ri
		}
	})
	if err != nil {
		m.logger.Error("reconcile_failed", "step", "mutate", "err", err)
		return fmt.Errorf("rewrite registry: %w", err)
	}
	m.mu.Lock()
	m.tracked = survivors
	m.mu.Unlock()
	m.logger.Info("reconcile_done",
		"kept", len(survivors), "dropped", dropped, "total", total)
	return nil
}

// RefreshFromDisk re-reads instances.json and replaces the in-memory tracked
// set with the live subset (entryAlive), WITHOUT writing the registry back.
// Safe to call periodically from observer processes (the TUI): persisting
// drops is the owning process's job — a concurrent writer's fresh launch must
// not be erased by an observer's stale snapshot.
func (m *fsManager) RefreshFromDisk() error {
	loaded, err := loadRegistry(m.registryPath)
	if err != nil {
		m.logger.Error("refresh_failed", "step", "load", "err", err)
		return err
	}
	tracked := make(map[int]domain.RunningInstance, len(loaded))
	dropped := 0
	for _, ri := range loaded {
		if !m.entryAlive(ri) {
			m.logger.Info("refresh_dropped",
				"pid", ri.PID, "profile_id", ri.ProfileID,
				"reason", "pid_recycled_or_dead")
			dropped++
			continue
		}
		m.logger.Info("refresh_kept",
			"pid", ri.PID, "profile_id", ri.ProfileID)
		tracked[ri.PID] = ri
	}
	m.mu.Lock()
	m.tracked = tracked
	m.mu.Unlock()
	m.logger.Info("refresh_done",
		"kept", len(tracked), "dropped", dropped, "total", len(loaded))
	return nil
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
