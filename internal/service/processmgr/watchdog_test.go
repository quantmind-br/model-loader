package processmgr

import (
	"io"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

type watchdogTestManager struct {
	launches []launchCall
	nextPID  int
}

type launchCall struct {
	profile   domain.Profile
	mode      LaunchMode
	attemptID string
}

func (m *watchdogTestManager) Launch(p domain.Profile, mode LaunchMode, attemptID string) (domain.RunningInstance, error) {
	m.launches = append(m.launches, launchCall{profile: p, mode: mode, attemptID: attemptID})
	m.nextPID++
	return domain.RunningInstance{PID: m.nextPID, ProfileID: p.ID, Background: mode == LaunchBackground}, nil
}

func (m *watchdogTestManager) Kill(int) error                                    { return nil }
func (m *watchdogTestManager) List() []domain.RunningInstance                    { return nil }
func (m *watchdogTestManager) WaitHealthy(int, int, time.Duration, string) error { return nil }
func (m *watchdogTestManager) WaitReady(domain.RunningInstance, time.Duration, string) (string, error) {
	return "", nil
}
func (m *watchdogTestManager) TailLogs(int) (io.ReadCloser, error)               { return io.NopCloser(nil), nil }
func (m *watchdogTestManager) Close() error                                      { return nil }
func (m *watchdogTestManager) GetExitInfo(int) (ExitInfo, bool)                  { return ExitInfo{}, false }
func (m *watchdogTestManager) History() []domain.ExitedInstance                  { return nil }
func (m *watchdogTestManager) RefreshFromDisk() error                            { return nil }

func TestWatchdogPolicies(t *testing.T) {
	tests := []struct {
		name      string
		policy    domain.RestartPolicy
		exitCode  int
		wantStart bool
	}{
		{name: "none never restarts", policy: domain.RestartPolicyNone, exitCode: 1},
		{name: "on failure restarts non-zero", policy: domain.RestartPolicyOnFailure, exitCode: 1, wantStart: true},
		{name: "on failure skips clean exit", policy: domain.RestartPolicyOnFailure, exitCode: 0},
		{name: "always restarts clean exit", policy: domain.RestartPolicyAlways, exitCode: 0, wantStart: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgrImpl := &watchdogTestManager{}
			var mgr Manager = mgrImpl
			w := NewWatchdog(&mgr)
			w.baseBackoff = 0
			profile := watchdogProfile("p", tt.policy, 3, 0)
			w.Track(domain.RunningInstance{PID: 10, Background: true}, profile)

			cmd := w.HandleExit(exitInfo(tt.exitCode))
			if !tt.wantStart {
				if cmd != nil {
					t.Fatalf("HandleExit returned restart cmd")
				}
				return
			}

			if cmd == nil {
				t.Fatal("HandleExit returned nil cmd")
			}
			msg := cmd()
			if _, ok := msg.(WatchdogRestartedMsg); !ok {
				t.Fatalf("msg = %T, want WatchdogRestartedMsg", msg)
			}
			if len(mgrImpl.launches) != 1 {
				t.Fatalf("launches = %d, want 1", len(mgrImpl.launches))
			}
			if mgrImpl.launches[0].mode != LaunchBackground {
				t.Fatalf("mode = %v, want LaunchBackground", mgrImpl.launches[0].mode)
			}
		})
	}
}

func TestWatchdogBackoffDoublingAndCap(t *testing.T) {
	w := &Watchdog{baseBackoff: 2 * time.Second, maxBackoff: 5 * time.Second}
	profile := watchdogProfile("p", domain.RestartPolicyAlways, 10, 0)

	if got := w.backoff(profile, 1); got != 2*time.Second {
		t.Fatalf("backoff attempt 1 = %v, want 2s", got)
	}
	if got := w.backoff(profile, 2); got != 4*time.Second {
		t.Fatalf("backoff attempt 2 = %v, want 4s", got)
	}
	if got := w.backoff(profile, 3); got != 5*time.Second {
		t.Fatalf("backoff attempt 3 = %v, want capped 5s", got)
	}
}

func TestWatchdogBackoffUsesProfileSeconds(t *testing.T) {
	w := &Watchdog{baseBackoff: time.Second, maxBackoff: time.Minute}
	profile := watchdogProfile("p", domain.RestartPolicyAlways, 10, 3)

	if got := w.backoff(profile, 2); got != 6*time.Second {
		t.Fatalf("backoff = %v, want 6s", got)
	}
}

func TestWatchdogGaveUpAfterMaxRestarts(t *testing.T) {
	mgrImpl := &watchdogTestManager{}
	var mgr Manager = mgrImpl
	w := NewWatchdog(&mgr)
	profile := watchdogProfile("p", domain.RestartPolicyAlways, 1, 0)
	w.states[42] = restartState{profile: profile, background: true, restartCount: 1}

	cmd := w.HandleExit(exitInfo(1))
	if cmd == nil {
		t.Fatal("HandleExit returned nil cmd")
	}
	msg := cmd()
	gaveUp, ok := msg.(WatchdogGaveUpMsg)
	if !ok {
		t.Fatalf("msg = %T, want WatchdogGaveUpMsg", msg)
	}
	if gaveUp.Attempts != 1 || gaveUp.Profile.ID != "p" || gaveUp.PID != 42 {
		t.Fatalf("gaveUp = %+v", gaveUp)
	}
	if len(mgrImpl.launches) != 0 {
		t.Fatalf("launches = %d, want 0", len(mgrImpl.launches))
	}
}

func watchdogProfile(id string, policy domain.RestartPolicy, maxRestarts, backoffSeconds int) domain.Profile {
	return domain.Profile{
		ID: id,
		Launch: domain.LaunchConfig{
			RestartPolicy:  policy,
			MaxRestarts:    maxRestarts,
			BackoffSeconds: backoffSeconds,
		},
	}
}

func exitInfo(code int) ExitInfo {
	return ExitInfo{ExitCode: &code}
}
