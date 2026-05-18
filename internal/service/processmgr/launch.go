package processmgr

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// Launch spawns llama-server with the args derived from p. mode chooses
// between background (detached, log-to-file) and foreground (stdout/stderr
// inherit; only one allowed at a time — covered in Task 6).
func (m *fsManager) Launch(p domain.Profile, mode LaunchMode, attemptID string) (domain.RunningInstance, error) {

	if p.Model == "" {
		return domain.RunningInstance{}, ErrModelNotFound
	}
	_, err := os.Stat(p.Model)
	if err != nil {
		// Only skip the missing-file error for HuggingFace-style repo IDs.
		// Local paths that exist are accepted above; local paths that
		// don't exist AND don't look like a HF repo are rejected.
		if !domain.LooksLikeHFRepo(p.Model) {
			if errors.Is(err, fs.ErrNotExist) {
				return domain.RunningInstance{}, fmt.Errorf("%w: %s", ErrModelNotFound, p.Model)
			}
			return domain.RunningInstance{}, fmt.Errorf("stat model: %w", err)
		}
	}
	port, ok := portFromProfile(p)
	if !ok {
		return domain.RunningInstance{}, fmt.Errorf("profile %q: missing or invalid port arg", p.ID)
	}
	if err := checkPortFree(port); err != nil {
		return domain.RunningInstance{}, err
	}
	if mode == LaunchForeground {
		return m.launchForeground(p, port, attemptID)
	}

	if err := os.MkdirAll(m.logDir, 0o755); err != nil {
		return domain.RunningInstance{}, fmt.Errorf("mkdir log dir: %w", err)
	}
	resolvedBinary := p.Launch.ResolvedExecutable
	if resolvedBinary == "" {
		var err error
		resolvedBinary, err = m.resolver(p)
		if err != nil {
			return domain.RunningInstance{}, fmt.Errorf("resolve backend executable: %w", err)
		}
	}
	logPath := filepath.Join(m.logDir, fmt.Sprintf("%s-%d.log", p.ID, port))
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return domain.RunningInstance{}, fmt.Errorf("open log: %w", err)
	}

	profileArgs, err := BuildArgsForBackend(p, p.Launch.ResolvedBackendKind, resolvedBinary)
	if err != nil {
		_ = logF.Close()
		return domain.RunningInstance{}, fmt.Errorf("build args: %w", err)
	}
	cmd := makeCommand(resolvedBinary, profileArgs)
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if env := applyProfileEnv(p.Launch.Env); env != nil {
		cmd.Env = env
	}

	if err := cmd.Start(); err != nil {
		_ = logF.Close()
		return domain.RunningInstance{}, fmt.Errorf("start process: %w", err)
	}
	_ = logF.Close() // child inherited its own fd; drop ours

	inst := domain.RunningInstance{
		ProfileID:      p.ID,
		PID:            cmd.Process.Pid,
		Port:           port,
		LogPath:        logPath,
		BinaryPath:     resolvedBinary,
		StartedAt:      time.Now().UTC(),
		Background:     true,
		RestartPolicy:  string(p.Launch.RestartPolicy),
		MaxRestarts:    p.Launch.MaxRestarts,
		BackoffSeconds: p.Launch.BackoffSeconds,
	}

	m.mu.Lock()
	m.tracked[inst.PID] = inst
	delete(m.exitInfos, inst.PID) // see comment above
	delete(m.historyRecorded, inst.PID)
	m.fgPID = 0 // background launch does not claim fg
	all := snapshotLocked(m.tracked)
	m.mu.Unlock()

	m.logger.Info("launch_started",
		"pid", inst.PID, "port", inst.Port,
		"profile_id", p.ID, "attempt_id", attemptID,
		"mode", "background", "binary", resolvedBinary)

	// MOVED from pre-Start to here (post-insert) so the waitEnrichment
	// body's re-read of m.tracked[inst.PID] sees a populated entry.
	go m.waitEnrichment(cmd, inst.PID, logPath, attemptID)

	if err := saveRegistry(m.registryPath, all); err != nil {
		return inst, fmt.Errorf("instance started (pid %d) but registry save failed: %w", inst.PID, err)
	}
	return inst, nil
}

func portFromProfile(p domain.Profile) (int, bool) {
	v, ok := p.Args["port"]
	if !ok {
		return 0, false
	}
	switch v := v.(type) {
	case float64:
		if v <= 0 || v > 65535 {
			return 0, false
		}
		return int(v), true
	case int:
		if v <= 0 || v > 65535 {
			return 0, false
		}
		return v, true
	case string:
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 65535 {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

func checkPortFree(port int) error {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("port %d: %w", port, ErrPortBusy)
	}
	_ = l.Close()
	return nil
}

// launchForeground spawns a single foreground instance. Stdout/Stderr are
// redirected to a log file so the monitor can tail them, and the process
// is NOT detached via Setsid: it remains in the TUI's process group so
// Ctrl+C from the TUI propagates if desired.
func (m *fsManager) launchForeground(p domain.Profile, port int, attemptID string) (domain.RunningInstance, error) {
	resolvedBinary := p.Launch.ResolvedExecutable
	if resolvedBinary == "" {
		var err error
		resolvedBinary, err = m.resolver(p)
		if err != nil {
			return domain.RunningInstance{}, fmt.Errorf("resolve backend executable: %w", err)
		}
	}

	m.mu.Lock()
	if m.fgPID != 0 {
		m.mu.Unlock()
		return domain.RunningInstance{}, ErrForegroundBusy
	}
	m.fgPID = -1 // sentinel: launching in progress
	m.mu.Unlock()

	if err := os.MkdirAll(m.logDir, 0o755); err != nil {
		return domain.RunningInstance{}, fmt.Errorf("mkdir log dir: %w", err)
	}
	logPath := filepath.Join(m.logDir, fmt.Sprintf("%s-%d.log", p.ID, port))
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return domain.RunningInstance{}, fmt.Errorf("open log: %w", err)
	}

	profileArgs, err := BuildArgsForBackend(p, p.Launch.ResolvedBackendKind, resolvedBinary)
	if err != nil {
		_ = logF.Close()
		return domain.RunningInstance{}, fmt.Errorf("build args: %w", err)
	}
	cmd := makeCommand(resolvedBinary, profileArgs)
	cmd.Stdout = logF
	cmd.Stderr = logF
	if env := applyProfileEnv(p.Launch.Env); env != nil {
		cmd.Env = env
	}
	if err := cmd.Start(); err != nil {
		_ = logF.Close()
		// Roll back sentinel so future calls can proceed.
		m.mu.Lock()
		m.fgPID = 0
		m.mu.Unlock()
		return domain.RunningInstance{}, fmt.Errorf("start process (fg): %w", err)
	}
	_ = logF.Close() // child inherited its own fd; drop ours

	inst := domain.RunningInstance{
		ProfileID:  p.ID,
		PID:        cmd.Process.Pid,
		Port:       port,
		LogPath:    logPath,
		BinaryPath: resolvedBinary,
		StartedAt:  time.Now().UTC(),
		Background: false,
	}

	m.mu.Lock()
	m.tracked[inst.PID] = inst
	delete(m.exitInfos, inst.PID) // see Launch background comment above
	delete(m.historyRecorded, inst.PID)
	m.fgPID = inst.PID // replaces -1 sentinel
	all := snapshotLocked(m.tracked)
	m.mu.Unlock()

	m.logger.Info("launch_started",
		"pid", inst.PID, "port", inst.Port,
		"profile_id", p.ID, "attempt_id", attemptID,
		"mode", "foreground", "binary", resolvedBinary)

	// MOVED from pre-Start to here (post-insert) so the waitEnrichment
	// body's re-read of m.tracked[inst.PID] sees a populated entry.
	go m.waitEnrichment(cmd, inst.PID, logPath, attemptID)

	if err := saveRegistry(m.registryPath, all); err != nil {
		return inst, fmt.Errorf("fg started but registry save failed: %w", err)
	}
	return inst, nil
}

// applyProfileEnv overlays profile env vars on the inherited process env.
// Returns nil when envs is empty so callers leave cmd.Env nil — Go then
// inherits os.Environ() implicitly, preserving legacy behavior bit-for-bit
// for profiles without env vars. On a non-empty input the inherited env is
// snapshot at launch time and each EnvVar overrides any existing key or is
// appended otherwise. Last value wins on intra-profile duplicates.
func applyProfileEnv(envs []domain.EnvVar) []string {
	if len(envs) == 0 {
		return nil
	}
	base := os.Environ()
	idx := make(map[string]int, len(base))
	for i, kv := range base {
		if eq := strings.IndexByte(kv, '='); eq > 0 {
			idx[kv[:eq]] = i
		}
	}
	out := append([]string(nil), base...)
	for _, ev := range envs {
		if ev.Key == "" {
			continue
		}
		kv := ev.Key + "=" + ev.Value
		if i, ok := idx[ev.Key]; ok {
			out[i] = kv
		} else {
			idx[ev.Key] = len(out)
			out = append(out, kv)
		}
	}
	return out
}

// makeCommand builds an exec.Command from a possibly compound command string
// (e.g. "python -m sglang.launch_server") and the profile args.
func makeCommand(resolvedBinary string, profileArgs []string) *exec.Cmd {
	fields, err := splitCommandLine(resolvedBinary)
	if err != nil || len(fields) == 0 {
		return exec.Command("")
	}
	if len(fields) == 1 {
		return exec.Command(fields[0], profileArgs...)
	}
	all := make([]string, 0, len(fields)-1+len(profileArgs))
	all = append(all, fields[1:]...)
	all = append(all, profileArgs...)
	return exec.Command(fields[0], all...)
}
