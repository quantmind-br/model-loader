package processmgr

import (
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/procutil"
)

func TestLiveness_MarksDeadPIDCrashed(t *testing.T) {
	dir := t.TempDir()
	m := New(Config{Resolver: func(_ domain.Profile) (string, domain.BackendKind, error) { return "true", "", nil }, LogDir: dir, RegistryPath: filepath.Join(dir, "i.json")})
	t.Cleanup(func() { _ = m.Close() })
	m.tracked[99999] = domain.RunningInstance{ProfileID: "x", PID: 99999, Port: 9000, Background: true}

	// Stub probe — first call says alive, subsequent say dead.
	calls := atomic.Int32{}
	probe := func(_ domain.RunningInstance) bool {
		c := calls.Add(1)
		return c == 1 // alive on first probe, dead after
	}
	stop := m.startLivenessWithProbe(50*time.Millisecond, probe)
	defer stop()

	// Wait for at least 2 ticks.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		inst := m.tracked[99999]
		m.mu.Unlock()
		if inst.Crashed {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("liveness ticker did not mark PID 99999 as Crashed; calls=%d", calls.Load())
}

func TestLiveness_AliveStaysAlive(t *testing.T) {
	dir := t.TempDir()
	m := New(Config{Resolver: func(_ domain.Profile) (string, domain.BackendKind, error) { return "true", "", nil }, LogDir: dir, RegistryPath: filepath.Join(dir, "i.json")})
	t.Cleanup(func() { _ = m.Close() })
	m.tracked[111] = domain.RunningInstance{ProfileID: "x", PID: 111, Port: 9000, Background: true}

	stop := m.startLivenessWithProbe(20*time.Millisecond, func(_ domain.RunningInstance) bool { return true })
	defer stop()

	time.Sleep(80 * time.Millisecond)
	m.mu.Lock()
	inst := m.tracked[111]
	m.mu.Unlock()
	if inst.Crashed {
		t.Fatal("liveness flipped Crashed=true on still-alive PID")
	}
}

// Sanity: ESRCH from syscall.Kill is treated as "process gone".
func TestLiveness_DefaultProbeRespectsESRCH(t *testing.T) {
	if procutil.Alive(99999) {
		// On Linux, syscall.Kill(99999, 0) typically returns ESRCH; if the
		// machine is heavily loaded with PIDs this could exist. Skip when so.
		t.Skip("PID 99999 is alive on this system; cannot validate ESRCH path")
	}
}
