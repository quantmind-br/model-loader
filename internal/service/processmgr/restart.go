package processmgr

import (
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// restartBackoffCap bounds a single restart's backoff sleep.
const restartBackoffCap = 30 * time.Second

// maybeScheduleRestart applies the restart policy for a dead instance.
// exitCode == nil means the exit status is unknown (signal death or
// liveness-detected) and counts as failure for on-failure policy.
// No-ops when: policy none/empty; a Kill was requested for this pid
// (intentional stop — audit A4); a restart was already scheduled for this
// pid (waitEnrichment + liveness dedupe); MaxRestarts exhausted.
// Blocks for the backoff, then invokes m.restartFunc. Callers decide
// whether to run it inline (reaper) or as a goroutine (liveness).
func (m *fsManager) maybeScheduleRestart(cur domain.RunningInstance, exitCode *int) {
	policy := cur.RestartPolicy
	if policy == "" || policy == "none" {
		return
	}

	m.mu.Lock()
	if _, killed := m.killRequested[cur.PID]; killed {
		m.mu.Unlock()
		return // intentional stop — never resurrect (audit A4)
	}
	if _, scheduled := m.restartScheduled[cur.PID]; scheduled {
		m.mu.Unlock()
		return // reaper/liveness dedupe (audit A10)
	}
	shouldRestart := policy == "always" ||
		(policy == "on-failure" && (exitCode == nil || *exitCode != 0))
	if !shouldRestart {
		m.mu.Unlock()
		return
	}
	if cur.MaxRestarts > 0 && cur.RestartCount >= cur.MaxRestarts {
		m.mu.Unlock()
		m.logger.Info("watchdog_gave_up",
			"pid", cur.PID, "profile_id", cur.ProfileID,
			"restart_count", cur.RestartCount, "max_restarts", cur.MaxRestarts)
		return
	}
	m.restartScheduled[cur.PID] = struct{}{}
	cur.RestartCount++
	now := time.Now().UTC()
	cur.LastRestartAt = &now
	if _, ok := m.tracked[cur.PID]; ok {
		m.tracked[cur.PID] = cur
	}
	// Carry the generation to the next Launch so MaxRestarts bounds the loop
	// across the death→relaunch boundary (audit A5).
	m.pendingRestarts[cur.ProfileID] = cur.RestartCount
	m.mu.Unlock()

	// Backoff: BackoffSeconds × count, floored at 1s (no hot loops when
	// BackoffSeconds is 0) and capped (audit A5).
	backoff := time.Duration(cur.BackoffSeconds) * time.Second
	if cur.RestartCount > 1 {
		backoff = time.Duration(cur.BackoffSeconds*cur.RestartCount) * time.Second
	}
	if backoff <= 0 {
		backoff = time.Second
	}
	if backoff > restartBackoffCap {
		backoff = restartBackoffCap
	}
	m.logger.Info("watchdog_restart",
		"pid", cur.PID, "profile_id", cur.ProfileID,
		"restart_count", cur.RestartCount, "backoff", backoff.String())
	time.Sleep(backoff)
	if m.restartFunc != nil {
		m.restartFunc(cur.ProfileID)
	}
}
