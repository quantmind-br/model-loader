package processmgr

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/procutil"
	"github.com/quantmind-br/model-loader/internal/service/internal/shellsplit"
)

type launchPlan struct {
	binary string
	args   []string
	env    []string
	port   int
	kind   domain.BackendKind
}

func (m *fsManager) prepareLaunch(p domain.Profile) (launchPlan, error) {
	port, err := allocateEphemeralPort()
	if err != nil {
		return launchPlan{}, err
	}
	// The manager owns the port. Clone Args so the caller's map is untouched,
	// discard any user-supplied value, and inject the allocated one so every
	// backend arg builder emits its --port flag.
	args := make(map[string]any, len(p.Args)+1)
	for k, v := range p.Args {
		args[k] = v
	}
	args["port"] = port
	p.Args = args
	resolvedBinary := p.Launch.ResolvedExecutable
	resolvedKind := p.Launch.ResolvedBackendKind
	// Resolve when either the executable or the backend kind is missing. Callers
	// that pre-resolve (TUI, benchmark, restart) set both; the HTTP proxy's
	// on-demand launch sets neither, so without this the kind would default to
	// llama-server and emit --model for sglang/vllm/dflash backends.
	if resolvedBinary == "" || resolvedKind == "" {
		exe, kind, err := m.resolver(p)
		if err != nil {
			return launchPlan{}, fmt.Errorf("resolve backend executable: %w", err)
		}
		if resolvedBinary == "" {
			resolvedBinary = exe
		}
		if resolvedKind == "" {
			resolvedKind = kind
		}
	}
	profileArgs, err := BuildArgsForBackend(p, resolvedKind, resolvedBinary)
	if err != nil {
		return launchPlan{}, fmt.Errorf("build args: %w", err)
	}
	return launchPlan{
		binary: resolvedBinary,
		args:   profileArgs,
		env:    buildLaunchEnv(resolvedKind, p.Launch.Env),
		port:   port,
		kind:   resolvedKind,
	}, nil
}

// Launch spawns llama-server with the args derived from p. mode chooses
// between background (detached, log-to-file) and foreground (stdout/stderr
// inherit; only one allowed at a time — covered in Task 6).
func (m *fsManager) Launch(p domain.Profile, mode LaunchMode, attemptID string) (domain.RunningInstance, error) {

	if p.Model == "" {
		return domain.RunningInstance{}, ErrModelNotFound
	}
	// Resolve the backend first: whether the model needs a local file depends
	// on the kind. LM Studio addresses models by daemon-side key resolved
	// fuzzy server-side by `lms load`, so there is nothing to stat.
	plan, err := m.prepareLaunch(p)
	if err != nil {
		return domain.RunningInstance{}, err
	}
	if plan.kind != domain.BackendKindLMStudio {
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
	}
	if mode == LaunchForeground {
		return m.launchForeground(p, plan, attemptID)
	}

	if err := os.MkdirAll(m.logDir, 0o755); err != nil {
		return domain.RunningInstance{}, fmt.Errorf("mkdir log dir: %w", err)
	}
	logPath := filepath.Join(m.logDir, fmt.Sprintf("%s-%d.log", p.ID, plan.port))
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return domain.RunningInstance{}, fmt.Errorf("open log: %w", err)
	}

	cmd := makeCommand(plan.binary, plan.args)
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if plan.env != nil {
		cmd.Env = plan.env
	}

	if err := cmd.Start(); err != nil {
		_ = logF.Close()
		return domain.RunningInstance{}, fmt.Errorf("start process: %w", err)
	}
	_ = logF.Close() // child inherited its own fd; drop ours

	ticks, _ := procutil.StartTicks(cmd.Process.Pid) // 0 on failure → identity disabled
	inst := domain.RunningInstance{
		ProfileID:      p.ID,
		PID:            cmd.Process.Pid,
		Port:           plan.port,
		LogPath:        logPath,
		BinaryPath:     plan.binary,
		Kind:           plan.kind,
		StartedAt:      time.Now().UTC(),
		StartTicks:     ticks,
		Background:     true,
		RestartPolicy:  string(p.Launch.RestartPolicy),
		MaxRestarts:    p.Launch.MaxRestarts,
		BackoffSeconds: p.Launch.BackoffSeconds,
	}

	m.mu.Lock()
	// Carry the restart generation across the death→relaunch boundary so
	// MaxRestarts bounds a crash loop (audit A5).
	if n, ok := m.pendingRestarts[p.ID]; ok {
		inst.RestartCount = n
		delete(m.pendingRestarts, p.ID)
	}
	m.tracked[inst.PID] = inst
	m.hasReaper[inst.PID] = struct{}{}
	delete(m.restartScheduled, inst.PID)
	delete(m.exitInfos, inst.PID) // see comment above
	delete(m.historyRecorded, inst.PID)
	m.fgPID = 0 // background launch does not claim fg
	m.mu.Unlock()

	m.logger.Info("launch_started",
		"pid", inst.PID, "port", inst.Port,
		"profile_id", p.ID, "attempt_id", attemptID,
		"mode", "background", "binary", plan.binary)

	// Flock-guarded delta upsert (audit A7). On save failure the child is live
	// but unregistered — kill it so it cannot leak as an orphan (audit A12);
	// return the zero instance so no caller uses a half-registered process.
	if err := mutateRegistry(m.registryPath, func(reg map[int]domain.RunningInstance) {
		reg[inst.PID] = inst
	}); err != nil {
		// Reap the child we are about to kill so it cannot linger as a zombie
		// and so TerminateTree's aliveness poll can observe the exit.
		go func() { _ = cmd.Wait() }()
		_ = procutil.TerminateTree(inst.PID, 2*time.Second)
		m.mu.Lock()
		delete(m.tracked, inst.PID)
		delete(m.hasReaper, inst.PID)
		m.mu.Unlock()
		m.logger.Error("launch_rolled_back_registry_error",
			"pid", inst.PID, "profile_id", p.ID, "err", err)
		return domain.RunningInstance{}, fmt.Errorf("registry save failed, instance killed: %w", err)
	}
	// Start the reaper only AFTER the registry upsert commits: a fast-crash's
	// Crashed delta (written by waitEnrichment, enrichment.go) must never be
	// clobbered by this launch's running-state delta above (audit P-C4). The
	// in-memory m.tracked insert already happened, so waitEnrichment's re-read
	// of m.tracked[inst.PID] still sees a populated entry.
	go m.waitEnrichment(cmd, inst.PID, logPath, attemptID)
	return inst, nil
}

// allocateEphemeralPort asks the OS for a free loopback port by binding
// 127.0.0.1:0 and immediately releasing it. The window between Close and the
// backend's own bind is accepted — the OS does not reuse a just-released
// ephemeral port under normal churn.
func allocateEphemeralPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate port: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port, nil
}

// launchForeground spawns a single foreground instance. Stdout/Stderr are
// redirected to a log file so the monitor can tail them, and the process
// is NOT detached via Setsid: it remains in the TUI's process group so
// Ctrl+C from the TUI propagates if desired.
func (m *fsManager) launchForeground(p domain.Profile, plan launchPlan, attemptID string) (domain.RunningInstance, error) {
	m.mu.Lock()
	if m.fgPID != 0 {
		m.mu.Unlock()
		return domain.RunningInstance{}, ErrForegroundBusy
	}
	m.fgPID = -1 // sentinel: launching in progress
	m.mu.Unlock()

	if err := os.MkdirAll(m.logDir, 0o755); err != nil {
		m.mu.Lock()
		m.fgPID = 0 // reset sentinel so future launches can proceed (audit N-C8)
		m.mu.Unlock()
		return domain.RunningInstance{}, fmt.Errorf("mkdir log dir: %w", err)
	}
	logPath := filepath.Join(m.logDir, fmt.Sprintf("%s-%d.log", p.ID, plan.port))
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		m.mu.Lock()
		m.fgPID = 0 // reset sentinel so future launches can proceed (audit N-C8)
		m.mu.Unlock()
		return domain.RunningInstance{}, fmt.Errorf("open log: %w", err)
	}

	cmd := makeCommand(plan.binary, plan.args)
	cmd.Stdout = logF
	cmd.Stderr = logF
	if plan.env != nil {
		cmd.Env = plan.env
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

	ticks, _ := procutil.StartTicks(cmd.Process.Pid) // 0 on failure → identity disabled
	inst := domain.RunningInstance{
		ProfileID:  p.ID,
		PID:        cmd.Process.Pid,
		Port:       plan.port,
		LogPath:    logPath,
		BinaryPath: plan.binary,
		Kind:       plan.kind,
		StartedAt:  time.Now().UTC(),
		StartTicks: ticks,
		Background: false,
	}

	m.mu.Lock()
	// Carry the restart generation across the death→relaunch boundary (A5).
	if n, ok := m.pendingRestarts[p.ID]; ok {
		inst.RestartCount = n
		delete(m.pendingRestarts, p.ID)
	}
	m.tracked[inst.PID] = inst
	m.hasReaper[inst.PID] = struct{}{}
	delete(m.restartScheduled, inst.PID)
	delete(m.exitInfos, inst.PID) // see Launch background comment above
	delete(m.historyRecorded, inst.PID)
	m.fgPID = inst.PID // replaces -1 sentinel
	m.mu.Unlock()

	m.logger.Info("launch_started",
		"pid", inst.PID, "port", inst.Port,
		"profile_id", p.ID, "attempt_id", attemptID,
		"mode", "foreground", "binary", plan.binary)

	// Flock-guarded delta upsert (audit A7). On save failure kill the live but
	// unregistered child (audit A12) and return the zero instance.
	if err := mutateRegistry(m.registryPath, func(reg map[int]domain.RunningInstance) {
		reg[inst.PID] = inst
	}); err != nil {
		// Reap the child we are about to kill so it cannot linger as a zombie
		// and so TerminateTree's aliveness poll can observe the exit.
		go func() { _ = cmd.Wait() }()
		_ = procutil.TerminateTree(inst.PID, 2*time.Second)
		m.mu.Lock()
		delete(m.tracked, inst.PID)
		delete(m.hasReaper, inst.PID)
		m.fgPID = 0
		m.mu.Unlock()
		m.logger.Error("launch_rolled_back_registry_error",
			"pid", inst.PID, "profile_id", p.ID, "err", err)
		return domain.RunningInstance{}, fmt.Errorf("registry save failed, instance killed: %w", err)
	}
	// Start the reaper only AFTER the registry upsert commits so a fast-crash's
	// Crashed delta can never be clobbered by this launch's running delta
	// (audit P-C4). m.tracked[inst.PID] is already populated for waitEnrichment.
	go m.waitEnrichment(cmd, inst.PID, logPath, attemptID)
	return inst, nil
}

// pythonBackends are launched through a Python interpreter (vllm serve,
// unsloth studio run, python -m sglang.launch_server). Python block-buffers
// stdout when it is redirected to a file (the managed log) instead of a TTY,
// so log lines reach the file in large delayed chunks instead of per-line.
// The monitor tails that file via fsnotify Write events, so buffered output
// makes the Server tab show logs in late bursts — unlike the C++ llama-server,
// which flushes per line. PYTHONUNBUFFERED=1 forces unbuffered stdio so the
// logs stream live. (dflash is the native dflash_server binary, not Python.)
var pythonBackends = map[domain.BackendKind]bool{
	domain.BackendKindVLLM:    true,
	domain.BackendKindSGLang:  true,
	domain.BackendKindUnsloth: true,
	domain.BackendKindTabby:   true,
}

// buildLaunchEnv overlays the profile env (applyProfileEnv) and, for
// Python-based backends, ensures PYTHONUNBUFFERED=1 so backend logs stream live
// in the Server tab. A profile (or the inherited env) that already sets
// PYTHONUNBUFFERED wins. Non-Python backends keep the legacy behavior: a nil
// env so Go inherits os.Environ() implicitly.
func buildLaunchEnv(kind domain.BackendKind, profileEnv []domain.EnvVar) []string {
	env := applyProfileEnv(profileEnv)
	if !pythonBackends[kind] {
		return env
	}
	if env == nil {
		env = os.Environ()
	}
	if !envHasKey(env, "PYTHONUNBUFFERED") {
		env = append(env, "PYTHONUNBUFFERED=1")
	}
	return env
}

// envHasKey reports whether env (KEY=VALUE entries) already defines key.
func envHasKey(env []string, key string) bool {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return true
		}
	}
	return false
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
	fields, err := shellsplit.Split(resolvedBinary)
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
