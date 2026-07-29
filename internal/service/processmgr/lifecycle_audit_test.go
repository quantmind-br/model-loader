package processmgr

// Regression tests for BACKEND-LIFECYCLE-AUDIT.md findings A1, A3, A4, A5, A7,
// A10, A11, A12. White-box (package processmgr) so they can seed unexported
// state and observe the tracking table directly.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/procutil"
)

func waitDead(pid int, within time.Duration) bool {
	d := time.Now().Add(within)
	for time.Now().Before(d) {
		if !procutil.Alive(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// spawnGroupLeader starts a Setsid group leader that backgrounds a child sleep
// and execs sleep, returning leader pid, child pid, and leader StartTicks.
func spawnGroupLeader(t *testing.T) (leader, child int, ticks uint64) {
	t.Helper()
	childFile := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command("sh", "-c", `sleep 60 & echo $! > "$CF"; exec sleep 60`)
	cmd.Env = append(os.Environ(), "CF="+childFile)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start leader: %v", err)
	}
	leader = cmd.Process.Pid
	go func() { _ = cmd.Wait() }()
	ticks, _ = procutil.StartTicks(leader)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(childFile); err == nil {
			if n, e := strconv.Atoi(strings.TrimSpace(string(b))); e == nil && n > 0 {
				child = n
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if child == 0 {
		_ = procutil.TerminateTree(leader, 0)
		t.Fatal("child pid never appeared")
	}
	return leader, child, ticks
}

// TestMutateRegistry_ConcurrentWritersLoseNoEntries — audit A7.
func TestMutateRegistry_ConcurrentWritersLoseNoEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instances.json")
	var wg sync.WaitGroup
	for _, pid := range []int{101, 202} {
		pid := pid
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_ = mutateRegistry(path, func(reg map[int]domain.RunningInstance) {
					reg[pid] = domain.RunningInstance{PID: pid, ProfileID: "p"}
				})
			}
		}()
	}
	wg.Wait()
	loaded, err := loadRegistry(path)
	if err != nil {
		t.Fatalf("loadRegistry: %v", err)
	}
	seen := map[int]bool{}
	for _, ri := range loaded {
		seen[ri.PID] = true
	}
	if !seen[101] || !seen[202] {
		t.Fatalf("both PIDs must survive concurrent writers; got %+v", loaded)
	}
}

// TestKill_GroupSweepsChildren — audit A3.
func TestKill_GroupSweepsChildren(t *testing.T) {
	mgr, _ := newTestManager(t)
	t.Cleanup(func() { _ = mgr.Close() })
	leader, child, ticks := spawnGroupLeader(t)
	mgr.mu.Lock()
	mgr.tracked[leader] = domain.RunningInstance{PID: leader, ProfileID: "p", StartTicks: ticks, Background: true}
	mgr.mu.Unlock()

	if err := mgr.Kill(leader); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if !waitDead(leader, 2*time.Second) {
		t.Errorf("leader %d alive after Kill", leader)
	}
	if !waitDead(child, 2*time.Second) {
		t.Errorf("child %d alive after Kill (group not swept)", child)
	}
}

// TestKill_RecycledPIDSkipped — audit A11: a recorded StartTicks that no longer
// matches the live PID means the process was recycled; Kill must not signal the
// innocent occupant.
func TestKill_RecycledPIDSkipped(t *testing.T) {
	mgr, _ := newTestManager(t)
	t.Cleanup(func() { _ = mgr.Close() })
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pid := cmd.Process.Pid
	real, _ := procutil.StartTicks(pid)
	mgr.mu.Lock()
	mgr.tracked[pid] = domain.RunningInstance{PID: pid, ProfileID: "p", StartTicks: real + 999}
	mgr.mu.Unlock()

	if err := mgr.Kill(pid); err != nil {
		t.Fatalf("Kill of recycled pid should be nil (idempotent stop): %v", err)
	}
	if !procutil.Alive(pid) {
		t.Fatal("Kill signaled a recycled/innocent PID")
	}
	mgr.mu.Lock()
	_, tracked := mgr.tracked[pid]
	mgr.mu.Unlock()
	if tracked {
		t.Fatal("recycled entry not removed from tracking")
	}
}

// TestKill_NoResurrect — audit A4: an intentional Kill must never trigger the
// restart policy.
func TestKill_NoResurrect(t *testing.T) {
	dir := t.TempDir()
	fb := fakeBinary(t)
	var restarts atomic.Int32
	mgr := New(Config{
		Resolver:     func(_ domain.Profile) (string, domain.BackendKind, error) { return fb, "", nil },
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
		RestartFunc:  func(string) { restarts.Add(1) },
	})
	t.Cleanup(func() { _ = mgr.Close() })
	p := domain.Profile{ID: "always", Model: "/dev/null", Args: map[string]any{}}
	p.Launch.RestartPolicy = domain.RestartPolicyAlways
	inst, err := mgr.Launch(p, LaunchBackground, "")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := mgr.WaitHealthy(inst.PID, inst.Port, 5*time.Second, ""); err != nil {
		t.Fatalf("WaitHealthy: %v", err)
	}
	if err := mgr.Kill(inst.PID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	time.Sleep(700 * time.Millisecond)
	if n := restarts.Load(); n != 0 {
		t.Fatalf("restartFunc fired %d times after intentional Kill; want 0", n)
	}
}

// TestRestart_CountPropagationHonorsMaxRestarts — audit A5: the restart count
// carries across generations so MaxRestarts actually bounds a crash loop.
func TestRestart_CountPropagationHonorsMaxRestarts(t *testing.T) {
	dir := t.TempDir()
	crashBin := filepath.Join(dir, "crash.sh")
	if err := os.WriteFile(crashBin, []byte("#!/usr/bin/env bash\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var launches atomic.Int32
	var mgr *fsManager
	relaunch := func(id string) {
		launches.Add(1)
		p := domain.Profile{ID: id, Model: "/dev/null", Args: map[string]any{}}
		p.Launch.RestartPolicy = domain.RestartPolicyOnFailure
		p.Launch.MaxRestarts = 2
		p.Launch.BackoffSeconds = 0
		_, _ = mgr.Launch(p, LaunchBackground, "")
	}
	mgr = New(Config{
		Resolver:     func(_ domain.Profile) (string, domain.BackendKind, error) { return crashBin, "", nil },
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
		RestartFunc:  relaunch,
	})
	t.Cleanup(func() { _ = mgr.Close() })

	p := domain.Profile{ID: "crashy", Model: "/dev/null", Args: map[string]any{}}
	p.Launch.RestartPolicy = domain.RestartPolicyOnFailure
	p.Launch.MaxRestarts = 2
	p.Launch.BackoffSeconds = 0
	if _, err := mgr.Launch(p, LaunchBackground, ""); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if launches.Load() >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Give any incorrect extra restart time to fire (backoff floor is 1s).
	time.Sleep(2 * time.Second)
	if n := launches.Load(); n != 2 {
		t.Fatalf("restartFunc fired %d times; want exactly 2 (MaxRestarts bound)", n)
	}
}

// TestRestart_AdoptedInstanceRestartedByLiveness — audit A10: instances without
// a reaper (adopted post-Reconcile) still get their restart policy applied, via
// the liveness ticker.
func TestRestart_AdoptedInstanceRestartedByLiveness(t *testing.T) {
	dir := t.TempDir()
	fb := fakeBinary(t)
	var restarts atomic.Int32
	mgr := New(Config{
		Resolver:     func(_ domain.Profile) (string, domain.BackendKind, error) { return fb, "", nil },
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
		RestartFunc:  func(string) { restarts.Add(1) },
	})
	t.Cleanup(func() { _ = mgr.Close() })
	// Stop the default 5s ticker before seeding, then run a fast one.
	mgr.livenessStop()
	mgr.mu.Lock()
	mgr.tracked[1<<22] = domain.RunningInstance{
		PID: 1 << 22, ProfileID: "adopted", StartTicks: 1,
		RestartPolicy: string(domain.RestartPolicyAlways),
	}
	mgr.mu.Unlock()
	stop := mgr.startLivenessWithProbe(30*time.Millisecond, func(ri domain.RunningInstance) bool {
		return procutil.SameProcess(ri.PID, ri.StartTicks)
	})
	defer stop()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if restarts.Load() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := restarts.Load(); n != 1 {
		t.Fatalf("adopted restart fired %d times; want exactly 1", n)
	}
}

// TestReconcile_IdentityKeepsCommMismatch — audit A1/A11: a live wrapper-exec
// backend (comm != binary basename) survives Reconcile when StartTicks match.
func TestReconcile_IdentityKeepsCommMismatch(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "instances.json")
	cmd := exec.Command("sleep", "60") // comm "sleep" != "vllm-serve.sh"
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pid := cmd.Process.Pid
	ticks, _ := procutil.StartTicks(pid)
	if err := saveRegistry(regPath, []domain.RunningInstance{{
		PID: pid, ProfileID: "vl", BinaryPath: "/opt/vllm/vllm-serve.sh",
		Kind: domain.BackendKindVLLM, StartTicks: ticks,
	}}); err != nil {
		t.Fatal(err)
	}
	mgr := New(Config{
		Resolver: func(_ domain.Profile) (string, domain.BackendKind, error) { return "true", "", nil },
		LogDir:   filepath.Join(dir, "logs"), RegistryPath: regPath,
	})
	t.Cleanup(func() { _ = mgr.Close() })
	if err := mgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	loaded, _ := loadRegistry(regPath)
	found := false
	for _, ri := range loaded {
		if ri.PID == pid {
			found = true
		}
	}
	if !found {
		t.Fatal("live wrapper-exec backend dropped despite matching StartTicks (A1)")
	}
}

// TestReconcile_StaleStartTicksDropped — audit A11: a recorded StartTicks that
// no longer matches the live PID (recycled) is dropped.
func TestReconcile_StaleStartTicksDropped(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "instances.json")
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pid := cmd.Process.Pid
	ticks, _ := procutil.StartTicks(pid)
	if err := saveRegistry(regPath, []domain.RunningInstance{{
		PID: pid, ProfileID: "vl", BinaryPath: "/opt/vllm/vllm-serve.sh",
		Kind: domain.BackendKindVLLM, StartTicks: ticks + 5,
	}}); err != nil {
		t.Fatal(err)
	}
	mgr := New(Config{
		Resolver: func(_ domain.Profile) (string, domain.BackendKind, error) { return "true", "", nil },
		LogDir:   filepath.Join(dir, "logs"), RegistryPath: regPath,
	})
	t.Cleanup(func() { _ = mgr.Close() })
	_ = mgr.Reconcile()
	loaded, _ := loadRegistry(regPath)
	for _, ri := range loaded {
		if ri.PID == pid {
			t.Fatal("stale-StartTicks entry survived Reconcile")
		}
	}
}

// TestReconcile_LegacyKindTokenKeepsWrapperExec — audit A1 legacy path: a
// StartTicks==0 (pre-upgrade) vLLM entry whose live cmdline contains "vllm"
// survives via the kind-token fallback.
func TestReconcile_LegacyKindTokenKeepsWrapperExec(t *testing.T) {
	dir := t.TempDir()
	regPath := filepath.Join(dir, "instances.json")
	// argv[0]="vllm" via exec -a; comm stays "sleep".
	cmd := exec.Command("bash", "-c", "exec -a vllm sleep 60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pid := cmd.Process.Pid
	time.Sleep(250 * time.Millisecond) // let exec take effect
	if err := saveRegistry(regPath, []domain.RunningInstance{{
		PID: pid, ProfileID: "vl", BinaryPath: "/opt/vllm/vllm-serve.sh",
		Kind: domain.BackendKindVLLM, // StartTicks 0 = legacy
	}}); err != nil {
		t.Fatal(err)
	}
	mgr := New(Config{
		Resolver: func(_ domain.Profile) (string, domain.BackendKind, error) { return "true", "", nil },
		LogDir:   filepath.Join(dir, "logs"), RegistryPath: regPath,
	})
	t.Cleanup(func() { _ = mgr.Close() })
	_ = mgr.Reconcile()
	loaded, _ := loadRegistry(regPath)
	found := false
	for _, ri := range loaded {
		if ri.PID == pid {
			found = true
		}
	}
	if !found {
		t.Fatal("legacy vLLM wrapper-exec entry dropped (A1 legacy kind-token path)")
	}
}

// TestLaunch_RegistrySaveErrorKillsChild — audit A12: when the registry write
// fails, Launch kills the live-but-unregistered child and returns the zero
// instance so no caller can leak it.
func TestLaunch_RegistrySaveErrorKillsChild(t *testing.T) {
	dir := t.TempDir()
	roDir := filepath.Join(dir, "ro")
	if err := os.MkdirAll(roDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fb := fakeBinary(t)
	mgr := New(Config{
		Resolver:     func(_ domain.Profile) (string, domain.BackendKind, error) { return fb, "", nil },
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(roDir, "instances.json"),
	})
	t.Cleanup(func() { _ = mgr.Close() })
	if err := os.Chmod(roDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })

	p := domain.Profile{ID: "leak", Model: "/dev/null", Args: map[string]any{}}
	inst, err := mgr.Launch(p, LaunchBackground, "")
	if err == nil {
		_ = mgr.Kill(inst.PID)
		t.Fatal("Launch should fail when the registry directory is read-only")
	}
	if inst.PID != 0 {
		t.Fatalf("Launch must return the zero instance on rollback; got pid %d", inst.PID)
	}
	mgr.mu.Lock()
	n := len(mgr.tracked)
	mgr.mu.Unlock()
	if n != 0 {
		t.Fatalf("tracked has %d entries after rollback; want 0", n)
	}
}

// TestLiveness_MarkOperatorStopLabelsAndSkipsRestart — UIUX-030(b): an adopted
// backend whose death the operator pre-declared via MarkOperatorStop is labelled
// ExitReasonOperatorStop (ExitClass "stopped") and never restarted, even with
// RestartPolicyAlways. Without the mark the same death is "crashed" and fires
// restartFunc (negative control in the same file).
func TestLiveness_MarkOperatorStopLabelsAndSkipsRestart(t *testing.T) {
	dir := t.TempDir()
	fb := fakeBinary(t)
	var restarts atomic.Int32
	mgr := New(Config{
		Resolver:     func(_ domain.Profile) (string, domain.BackendKind, error) { return fb, "", nil },
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
		RestartFunc:  func(string) { restarts.Add(1) },
	})
	t.Cleanup(func() { _ = mgr.Close() })
	mgr.livenessStop()

	const pid = 1 << 23
	mgr.mu.Lock()
	mgr.tracked[pid] = domain.RunningInstance{
		PID: pid, ProfileID: "op-stop", StartTicks: 1,
		RestartPolicy: string(domain.RestartPolicyAlways),
	}
	mgr.mu.Unlock()
	// Intentionally absent from hasReaper: adopted observer death path.
	mgr.MarkOperatorStop(pid)

	stop := mgr.startLivenessWithProbe(30*time.Millisecond, func(ri domain.RunningInstance) bool {
		return procutil.SameProcess(ri.PID, ri.StartTicks)
	})
	defer stop()

	// Wait until the death is classified.
	deadline := time.Now().Add(4 * time.Second)
	var entry domain.RunningInstance
	var found bool
	for time.Now().Before(deadline) {
		for _, ri := range mgr.List() {
			if ri.PID == pid && ri.Crashed {
				entry = ri
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Fatal("liveness never marked the adopted pid Crashed")
	}
	if entry.ExitReason != domain.ExitReasonOperatorStop {
		t.Fatalf("ExitReason = %q, want %q", entry.ExitReason, domain.ExitReasonOperatorStop)
	}
	if got := domain.ExitClass(entry); got != "stopped" {
		t.Fatalf("ExitClass = %q, want stopped", got)
	}
	// Give a wrong restart policy time to fire (same window as adopted control).
	time.Sleep(200 * time.Millisecond)
	if n := restarts.Load(); n != 0 {
		t.Fatalf("restartFunc fired %d times after MarkOperatorStop; want 0", n)
	}
}

// TestLiveness_AdoptedDeathWithoutMarkStillCrashesAndRestarts is the negative
// control for UIUX-030(b): without MarkOperatorStop the observer death path
// still classifies as crashed and applies RestartPolicyAlways.
func TestLiveness_AdoptedDeathWithoutMarkStillCrashesAndRestarts(t *testing.T) {
	dir := t.TempDir()
	fb := fakeBinary(t)
	var restarts atomic.Int32
	mgr := New(Config{
		Resolver:     func(_ domain.Profile) (string, domain.BackendKind, error) { return fb, "", nil },
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
		RestartFunc:  func(string) { restarts.Add(1) },
	})
	t.Cleanup(func() { _ = mgr.Close() })
	mgr.livenessStop()

	const pid = 1 << 24
	mgr.mu.Lock()
	mgr.tracked[pid] = domain.RunningInstance{
		PID: pid, ProfileID: "adopted-crash", StartTicks: 1,
		RestartPolicy: string(domain.RestartPolicyAlways),
	}
	mgr.mu.Unlock()

	stop := mgr.startLivenessWithProbe(30*time.Millisecond, func(ri domain.RunningInstance) bool {
		return procutil.SameProcess(ri.PID, ri.StartTicks)
	})
	defer stop()

	deadline := time.Now().Add(4 * time.Second)
	var entry domain.RunningInstance
	var found bool
	for time.Now().Before(deadline) {
		for _, ri := range mgr.List() {
			if ri.PID == pid && ri.Crashed {
				entry = ri
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Fatal("liveness never marked the adopted pid Crashed")
	}
	if got := domain.ExitClass(entry); got != "crashed" {
		t.Fatalf("ExitClass = %q, want crashed (no MarkOperatorStop)", got)
	}
	for time.Now().Before(deadline) {
		if restarts.Load() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := restarts.Load(); n < 1 {
		t.Fatalf("restartFunc fired %d times without MarkOperatorStop; want >= 1", n)
	}
}

// TestLiveness_OwnedKillIntentRetainsFlag — liveness must NOT consume
// killRequested for owned (hasReaper) pids. The reaper's waitEnrichment rewrites
// ExitReason and maybeScheduleRestart's A4 guard both read that flag; deleting
// it here would mislabel SIGTERM kills and resurrect RestartPolicyAlways.
func TestLiveness_OwnedKillIntentRetainsFlag(t *testing.T) {
	dir := t.TempDir()
	fb := fakeBinary(t)
	var restarts atomic.Int32
	mgr := New(Config{
		Resolver:     func(_ domain.Profile) (string, domain.BackendKind, error) { return fb, "", nil },
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
		RestartFunc:  func(string) { restarts.Add(1) },
	})
	t.Cleanup(func() { _ = mgr.Close() })
	mgr.livenessStop()

	const pid = 1 << 25
	mgr.mu.Lock()
	mgr.tracked[pid] = domain.RunningInstance{
		PID: pid, ProfileID: "owned-kill", StartTicks: 1,
		RestartPolicy: string(domain.RestartPolicyAlways),
	}
	mgr.hasReaper[pid] = struct{}{}
	mgr.killRequested[pid] = struct{}{}
	mgr.mu.Unlock()

	stop := mgr.startLivenessWithProbe(30*time.Millisecond, func(ri domain.RunningInstance) bool {
		return procutil.SameProcess(ri.PID, ri.StartTicks)
	})
	defer stop()

	deadline := time.Now().Add(4 * time.Second)
	var entry domain.RunningInstance
	var found bool
	for time.Now().Before(deadline) {
		for _, ri := range mgr.List() {
			if ri.PID == pid && ri.Crashed {
				entry = ri
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !found {
		t.Fatal("liveness never marked the owned pid Crashed")
	}
	if entry.ExitReason != domain.ExitReasonOperatorStop {
		t.Fatalf("ExitReason = %q, want %q (liveness still labels)", entry.ExitReason, domain.ExitReasonOperatorStop)
	}
	mgr.mu.Lock()
	_, still := mgr.killRequested[pid]
	mgr.mu.Unlock()
	if !still {
		t.Fatal("killRequested consumed for owned pid; reaper needs it for A4 + ExitReason rewrite")
	}
	// Owned deaths are never put on the adopted restart path; flag retention
	// is what keeps maybeScheduleRestart (reaper) from resurrecting.
	time.Sleep(200 * time.Millisecond)
	if n := restarts.Load(); n != 0 {
		t.Fatalf("restartFunc fired %d times for owned intentional death; want 0", n)
	}
}
