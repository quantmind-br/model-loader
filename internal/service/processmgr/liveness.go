package processmgr

import (
	"runtime/debug"
	"sync"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/procutil"
)

// startLivenessWithProbe starts a goroutine that polls every `interval` the
// tracked PIDs and marks Crashed those for which probe(inst) returns false.
// The probe is identity-aware (audit A11): a recycled PID no longer masks a
// dead backend as "running". Adopted instances (no reaper) that die get their
// restart policy applied here (audit A10) — no waitEnrichment exists to do it.
//
// Returns an idempotent stop() that ALSO waits for the goroutine to fully
// drain (including any pending registry save), eliminating the race between
// stop() returning and t.TempDir() cleanup.
func (m *fsManager) startLivenessWithProbe(interval time.Duration, probe func(domain.RunningInstance) bool) func() {
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
				var crashed []domain.RunningInstance
				var adopted []domain.RunningInstance
				nowUTC := now.UTC()
				for pid, inst := range m.tracked {
					if inst.Crashed {
						continue
					}
					if probe(inst) {
						continue
					}
					ts := nowUTC
					inst.Crashed = true
					inst.ExitedAt = &ts
					m.tracked[pid] = inst
					crashed = append(crashed, inst)
					// Adopted instances have no reaper to apply restart policy.
					if _, owned := m.hasReaper[pid]; !owned {
						adopted = append(adopted, inst)
					}
				}
				m.mu.Unlock()
				for _, inst := range crashed {
					m.logger.Info("liveness_crash_detected",
						"pid", inst.PID, "profile_id", inst.ProfileID)
				}
				// Flock-guarded delta: touch only the newly-crashed PIDs so a
				// concurrent writer's fresh launch is never erased (audit A7).
				if len(crashed) > 0 {
					_ = mutateRegistry(m.registryPath, func(reg map[int]domain.RunningInstance) {
						for _, inst := range crashed {
							reg[inst.PID] = inst
						}
					})
				}
				// Restart adopted deaths off the ticker goroutine so backoff
				// never blocks liveness (audit A10). exitCode nil = unknown.
				for _, inst := range adopted {
					go m.maybeScheduleRestart(inst, nil)
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

// startLiveness uses the default identity-aware probe and a 5-second tick.
func (m *fsManager) startLiveness() func() {
	return m.startLivenessWithProbe(5*time.Second, func(ri domain.RunningInstance) bool {
		return procutil.SameProcess(ri.PID, ri.StartTicks)
	})
}
