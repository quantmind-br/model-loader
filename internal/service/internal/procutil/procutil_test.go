package procutil

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// TestStartTicksStableAndNonZero verifies StartTicks(self) is non-zero and
// stable across two calls (audit A1/A11: identity anchor).
func TestStartTicksStableAndNonZero(t *testing.T) {
	pid := os.Getpid()
	a, err := StartTicks(pid)
	if err != nil {
		t.Fatalf("StartTicks(%d): %v", pid, err)
	}
	if a == 0 {
		t.Fatalf("StartTicks(%d) = 0, want non-zero", pid)
	}
	b, err := StartTicks(pid)
	if err != nil {
		t.Fatalf("StartTicks(%d) second call: %v", pid, err)
	}
	if a != b {
		t.Fatalf("StartTicks not stable: %d != %d", a, b)
	}
}

// TestSameProcessDetectsRecycling verifies SameProcess with wrong ticks is
// false (would-be recycled PID) and with 0 ticks degrades to Alive (audit A11).
func TestSameProcessDetectsRecycling(t *testing.T) {
	pid := os.Getpid()
	real, err := StartTicks(pid)
	if err != nil {
		t.Fatalf("StartTicks: %v", err)
	}
	if SameProcess(pid, real+1) {
		t.Fatalf("SameProcess(self, wrongTicks) = true, want false")
	}
	if !SameProcess(pid, real) {
		t.Fatalf("SameProcess(self, realTicks) = false, want true")
	}
	if !SameProcess(pid, 0) {
		t.Fatalf("SameProcess(self, 0) = false, want true (degrade to Alive)")
	}
	if SameProcess(1<<22, 1) {
		t.Fatalf("SameProcess(dead, 1) = true, want false")
	}
}

// TestTerminateTreeKillsSetsidChild verifies TerminateTree sweeps a Setsid
// group leader AND its backgrounded child (audit A3: vLLM EngineCore/Worker_TP
// survive a leader-only SIGKILL).
func TestTerminateTreeKillsSetsidChild(t *testing.T) {
	// Leader forks a child sleep, prints its PID, then execs another sleep so
	// the leader's own PID stays alive as group leader.
	script := `sleep 60 & echo $! > "$CHILD_PID_FILE"; exec sleep 60`
	childFile := t.TempDir() + "/child.pid"
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "CHILD_PID_FILE="+childFile)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	leader := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()

	// Wait for the child PID file to appear.
	var childPID int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(childFile)
		if err == nil {
			if n, perr := strconv.Atoi(string(trimSpace(data))); perr == nil && n > 0 {
				childPID = n
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if childPID == 0 {
		_ = TerminateTree(leader, 0)
		t.Fatal("child PID never appeared")
	}
	if !Alive(leader) || !Alive(childPID) {
		_ = TerminateTree(leader, 0)
		t.Fatalf("setup: leader alive=%v child alive=%v", Alive(leader), Alive(childPID))
	}

	if err := TerminateTree(leader, time.Second); err != nil {
		t.Fatalf("TerminateTree: %v", err)
	}
	if Alive(childPID) || groupAlive(leader) {
		t.Fatal("TerminateTree returned before confirming the child/group exited")
	}

	// Both leader and child must be gone (group sweep).
	waitDead := func(pid int) bool {
		d := time.Now().Add(2 * time.Second)
		for time.Now().Before(d) {
			if !Alive(pid) {
				return true
			}
			time.Sleep(20 * time.Millisecond)
		}
		return false
	}
	if !waitDead(leader) {
		t.Errorf("leader %d still alive after TerminateTree", leader)
	}
	if !waitDead(childPID) {
		t.Errorf("child %d still alive after TerminateTree (group not swept)", childPID)
	}
}

func TestTerminateTreeConfirmsChildWhenLeaderExitsFirst(t *testing.T) {
	childFile := t.TempDir() + "/child.pid"
	cmd := exec.Command("sh", "-c", `sh -c 'trap "" TERM; echo $$ > "$CHILD_PID_FILE"; exec sleep 60' & wait`)
	cmd.Env = append(os.Environ(), "CHILD_PID_FILE="+childFile)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	leader := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() { _ = syscall.Kill(-leader, syscall.SIGKILL) })
	childPID := 0
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(childFile); err == nil {
			childPID, _ = strconv.Atoi(string(trimSpace(data)))
			if childPID > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID == 0 {
		t.Fatal("child did not become ready")
	}
	if err := TerminateTree(leader, time.Second); err != nil {
		t.Fatal(err)
	}
	if Alive(childPID) || groupAlive(leader) {
		t.Fatal("surviving child after group termination")
	}
}

// TestTerminateTreeConfirmsDeathBeforeReturn verifies TerminateTree does not
// return until the target is CONFIRMED dead after SIGKILL. The leader traps
// SIGTERM to SIG_IGN (survives the exec), so the SIGTERM grace elapses and the
// SIGKILL+confirm path runs. The fix (P4/DF11): returning before confirmed
// death lets Kill purge the registry while the backend still holds VRAM.
func TestTerminateTreeConfirmsDeathBeforeReturn(t *testing.T) {
	// trap "" TERM sets SIGTERM to SIG_IGN, which survives the exec into
	// sleep, so the leader ignores SIGTERM and only dies on SIGKILL.
	cmd := exec.Command("sh", "-c", `trap "" TERM; exec sleep 60`)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	leader := cmd.Process.Pid
	go func() { _ = cmd.Wait() }()
	defer func() { _ = TerminateTree(leader, 0) }()

	if !Alive(leader) {
		t.Fatalf("setup: leader %d not alive before kill", leader)
	}

	// Small SIGTERM grace: the leader ignores SIGTERM so the grace elapses,
	// then SIGKILL fires and the confirm loop waits for actual death (well
	// under the 15s killConfirmGrace).
	err := TerminateTree(leader, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("TerminateTree: %v", err)
	}

	// The whole point: no polling loop here. TerminateTree must not return
	// before the target is confirmed dead. If it is still Alive immediately
	// after return, the confirmed-death fix regressed.
	if Alive(leader) {
		t.Fatalf("leader %d still alive immediately after TerminateTree returned (returned before confirmed death)", leader)
	}
}

func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == ' ' || b[len(b)-1] == '\t' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
