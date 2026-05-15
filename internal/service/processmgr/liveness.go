package processmgr

import (
	"errors"
	"runtime/debug"
	"sync"
	"syscall"
	"time"
)

// probePIDAlive retorna true quando syscall.Kill(pid, 0) sucede, o que
// significa que o processo existe (independente da permissão de signaling).
// Em Linux, ESRCH indica que o PID já não existe.
func probePIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.EPERM) {
		// existe mas não temos permissão — ainda assim, vivo.
		return true
	}
	return false
}

// crashEvent batches a structured log emission to fire AFTER m.mu is
// released. Liveness must not hold the lock across logger writes — see
// AGENTS.md "What NOT to do". Slice is built under lock, drained outside.
type crashEvent struct {
	pid       int
	profileID string
}

// startLivenessWithProbe inicia uma goroutine que polla cada `interval` os
// PIDs trackeados e marca como Crashed os que `probe(pid)` retornar false.
// Retorna função stop() idempotente that ALSO waits for the goroutine to
// fully drain (including any pending registry save), eliminating the race
// between `stop()` returning and `t.TempDir()` cleanup.
func (m *fsManager) startLivenessWithProbe(interval time.Duration, probe func(int) bool) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				m.logger.Error("liveness_goroutine_panic",
					"panic", r, "stack", string(debug.Stack()))
			}
		}()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-t.C:
				m.mu.Lock()
				dirty := false
				var crashes []crashEvent
				nowUTC := now.UTC()
				for pid, inst := range m.tracked {
					if inst.Crashed {
						continue
					}
					if probe(pid) {
						continue
					}
					ts := nowUTC
					inst.Crashed = true
					inst.ExitedAt = &ts
					m.tracked[pid] = inst
					dirty = true
					crashes = append(crashes, crashEvent{pid: pid, profileID: inst.ProfileID})
				}
				snapshot := snapshotLocked(m.tracked)
				m.mu.Unlock()
				for _, c := range crashes {
					m.logger.Info("liveness_crash_detected",
						"pid", c.pid, "profile_id", c.profileID)
				}
				if dirty {
					_ = saveRegistry(m.registryPath, snapshot)
				}
			}
		}
	}()
	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			close(stop)
			<-done // wait for goroutine to finish; prevents TempDir-cleanup race
		})
	}
}

// startLiveness usa a probe default (syscall.Kill) e tick de 5 segundos.
func (m *fsManager) startLiveness() func() {
	return m.startLivenessWithProbe(5*time.Second, probePIDAlive)
}
