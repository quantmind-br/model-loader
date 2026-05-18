package processmgr

import (
	"fmt"
	"reflect"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/quantmind-br/model-loader/internal/domain"
)

type Watchdog struct {
	mgr         *Manager
	baseBackoff time.Duration
	maxBackoff  time.Duration
	states      map[int]restartState
}

type restartState struct {
	profile      domain.Profile
	background   bool
	restartCount int
}

type WatchdogGaveUpMsg struct {
	PID      int
	Profile  domain.Profile
	Attempts int
}

type WatchdogRestartedMsg struct {
	OldPID   int
	Instance domain.RunningInstance
}

type WatchdogRestartFailedMsg struct {
	PID     int
	Profile domain.Profile
	Err     error
}

func NewWatchdog(mgr *Manager) *Watchdog {
	return &Watchdog{
		mgr:         mgr,
		baseBackoff: time.Second,
		maxBackoff:  time.Minute,
		states:      map[int]restartState{},
	}
}

func (w *Watchdog) Track(inst domain.RunningInstance, profile domain.Profile) {
	if w == nil || inst.PID == 0 {
		return
	}
	if w.states == nil {
		w.states = map[int]restartState{}
	}
	w.states[inst.PID] = restartState{profile: profile, background: inst.Background}
}

func (w *Watchdog) HandleExit(ev ExitInfo) tea.Cmd {
	if w == nil {
		return nil
	}
	pid := exitPID(ev)
	state, matchedPID, ok := w.stateForPID(pid)
	if !ok {
		return nil
	}
	pid = matchedPID
	delete(w.states, pid)

	if !shouldRestart(state.profile.Launch.RestartPolicy, exitCode(ev), exitKilled(ev)) {
		return nil
	}

	if state.profile.Launch.MaxRestarts > 0 && state.restartCount >= state.profile.Launch.MaxRestarts {
		return func() tea.Msg {
			return WatchdogGaveUpMsg{PID: pid, Profile: state.profile, Attempts: state.restartCount}
		}
	}

	state.restartCount++
	delay := w.backoff(state.profile, state.restartCount)
	return func() tea.Msg {
		time.Sleep(delay)
		if w.mgr == nil || *w.mgr == nil {
			return WatchdogRestartFailedMsg{PID: pid, Profile: state.profile, Err: fmt.Errorf("process manager is nil")}
		}
		mode := LaunchForeground
		if state.background {
			mode = LaunchBackground
		}
		inst, err := (*w.mgr).Launch(state.profile, mode, "watchdog")
		if err != nil {
			w.states[pid] = state
			return WatchdogRestartFailedMsg{PID: pid, Profile: state.profile, Err: err}
		}
		w.states[inst.PID] = state
		return WatchdogRestartedMsg{OldPID: pid, Instance: inst}
	}
}

func (w *Watchdog) stateForPID(pid int) (restartState, int, bool) {
	if len(w.states) == 0 {
		return restartState{}, 0, false
	}
	if state, ok := w.states[pid]; ok {
		return state, pid, true
	}
	if pid != 0 || len(w.states) != 1 {
		return restartState{}, 0, false
	}
	for trackedPID, state := range w.states {
		return state, trackedPID, true
	}
	return restartState{}, 0, false
}

func shouldRestart(policy domain.RestartPolicy, code int, killed bool) bool {
	if killed {
		return false
	}
	switch policy {
	case domain.RestartPolicyAlways:
		return true
	case domain.RestartPolicyOnFailure:
		return code != 0
	default:
		return false
	}
}

func (w *Watchdog) backoff(profile domain.Profile, restartCount int) time.Duration {
	base := w.baseBackoff
	if profile.Launch.BackoffSeconds > 0 {
		base = time.Duration(profile.Launch.BackoffSeconds) * time.Second
	}
	if base <= 0 {
		base = time.Second
	}
	delay := base
	for range max(0, restartCount-1) {
		delay *= 2
		if w.maxBackoff > 0 && delay >= w.maxBackoff {
			return w.maxBackoff
		}
	}
	if w.maxBackoff > 0 && delay > w.maxBackoff {
		return w.maxBackoff
	}
	return delay
}

func exitCode(ev ExitInfo) int {
	if ev.ExitCode == nil {
		return 0
	}
	return *ev.ExitCode
}

func exitPID(ev ExitInfo) int {
	return intField(ev, "PID")
}

func exitKilled(ev ExitInfo) bool {
	return boolField(ev, "Killed")
}

func intField(v any, name string) int {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return 0
	}
	f := rv.FieldByName(name)
	if !f.IsValid() {
		return 0
	}
	switch f.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(f.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return int(f.Uint())
	default:
		return 0
	}
}

func boolField(v any, name string) bool {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return false
	}
	f := rv.FieldByName(name)
	return f.IsValid() && f.Kind() == reflect.Bool && f.Bool()
}
