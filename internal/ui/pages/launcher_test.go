package pages

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/processmgr"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/profilestore"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/validator"
)

func TestLauncherPage_ListsProfiles(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(domain.Profile{
		ID: "qwen", Name: "Qwen Coder", Model: "/m.gguf",
		Args: map[string]any{"port": float64(8080)},
	}); err != nil {
		t.Fatal(err)
	}

	page := NewLauncherPage(store, nil, nil) // no manager / no validator yet
	tm := teatest.NewTestModel(t, page, teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Qwen Coder")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	_ = tm.Quit()
}

type fakeManager struct {
	launched []domain.Profile
	mode     processmgr.LaunchMode
	nextErr  error
}

func (f *fakeManager) Launch(p domain.Profile, mode processmgr.LaunchMode) (domain.RunningInstance, error) {
	if f.nextErr != nil {
		err := f.nextErr
		f.nextErr = nil
		return domain.RunningInstance{}, err
	}
	f.launched = append(f.launched, p)
	f.mode = mode
	return domain.RunningInstance{ProfileID: p.ID, PID: 4242, Port: 8080, Background: mode == processmgr.LaunchBackground}, nil
}
func (f *fakeManager) Kill(pid int) error                          { return nil }
func (f *fakeManager) List() []domain.RunningInstance              { return nil }
func (f *fakeManager) WaitHealthy(_, _ int, _ time.Duration) error { return nil }
func (f *fakeManager) TailLogs(_ int) (io.ReadCloser, error)       { return nil, processmgr.ErrUnknownPID }
func (f *fakeManager) Close() error                                { return nil }

type mockResolver struct{}

func (m *mockResolver) Resolve(p domain.Profile) (backendcatalog.ResolvedBackend, error) {
	return backendcatalog.ResolvedBackend{
		Backend:        domain.Backend{ID: "default", Name: "default", Executable: "llama-server"},
		ExecutablePath: "llama-server",
		Schema:         domain.BackendValidationSchema{Flags: map[string]domain.FlagSpec{}},
	}, nil
}

func TestLauncherPage_EnterLaunchesSelected(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	_ = store.Save(domain.Profile{
		ID: "alpha", Name: "Alpha", Model: "/m.gguf",
		Args: map[string]any{"port": float64(8080)},
	})

	mgr := &fakeManager{}
	page := NewLauncherPage(store, mgr, nil).SetBackendResolver(&mockResolver{})

	model, _ := page.Update(LauncherProfilesLoadedMsg{Profiles: []domain.Profile{{
		ID: "alpha", Name: "Alpha", Model: "/m.gguf",
		Args: map[string]any{"port": float64(8080)},
	}}})
	page = model.(LauncherPage)

	updated, cmd := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = updated.(LauncherPage)

	if cmd == nil {
		t.Fatal("Enter did not produce a tea.Cmd")
	}
	msg := cmd()
	switch m := msg.(type) {
	case launchedMsg:
		if m.inst.ProfileID != "alpha" {
			t.Errorf("launched ProfileID = %s, want alpha", m.inst.ProfileID)
		}
	case launchErrMsg:
		t.Fatalf("got launchErrMsg: %v", m.err)
	default:
		t.Fatalf("unexpected msg type: %T", msg)
	}

	if len(mgr.launched) != 1 {
		t.Errorf("manager.launched len = %d, want 1", len(mgr.launched))
	}
	if mgr.mode != processmgr.LaunchBackground {
		t.Errorf("mode = %v, want LaunchBackground", mgr.mode)
	}
}

func TestLauncherPage_ValidationBlocksLaunch(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)

	bad := domain.Profile{
		ID: "bad", Name: "Bad", Model: "/m.gguf",
		Args: map[string]any{
			"port":        float64(8080),
			"batch-size":  float64(1024),
			"ubatch-size": float64(2048), // > batch-size -> error
		},
	}
	mgr := &fakeManager{}
	page := NewLauncherPage(store, mgr, validator.New())
	model, _ := page.Update(LauncherProfilesLoadedMsg{Profiles: []domain.Profile{bad}})
	page = model.(LauncherPage)

	_, cmd := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a cmd (with launchErrMsg), got nil")
	}
	msg := cmd()
	if _, ok := msg.(launchErrMsg); !ok {
		t.Fatalf("expected launchErrMsg from validation, got %T", msg)
	}
	if len(mgr.launched) != 0 {
		t.Errorf("manager.launched len = %d, want 0 (validation should have blocked)", len(mgr.launched))
	}
}

func TestLauncherPage_KOpensConfirmDoesNotKillImmediately(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	_ = store.Save(domain.Profile{
		ID: "alpha", Name: "Alpha", Model: "/m.gguf",
		Args: map[string]any{"port": float64(8080)},
	})

	mgr := &fakeManager{}
	page := NewLauncherPage(store, mgr, nil)
	model, _ := page.Update(launchedMsg{inst: domain.RunningInstance{ProfileID: "alpha", PID: 4242, Port: 8080, Background: true}})
	page = model.(LauncherPage)

	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	page = updated.(LauncherPage)
	if !page.killConfirm.Active() {
		t.Fatal("expected confirm form after k")
	}
	if !page.IsCapturingInput() {
		t.Fatal("page should capture input while confirm is open")
	}
	if len(page.running) != 1 {
		t.Errorf("k should not kill immediately; running len=%d, want 1", len(page.running))
	}
}

func TestLauncherPage_FinalizeAffirmativeRemovesInstance(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	_ = store.Save(domain.Profile{
		ID: "alpha", Name: "Alpha", Model: "/m.gguf",
		Args: map[string]any{"port": float64(8080)},
	})

	mgr := &fakeManager{}
	page := NewLauncherPage(store, mgr, nil)
	model, _ := page.Update(launchedMsg{inst: domain.RunningInstance{ProfileID: "alpha", PID: 4242, Port: 8080, Background: true}})
	page = model.(LauncherPage)

	// Open the confirm form.
	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	page = updated.(LauncherPage)

	// Drive affirmative path via the msg killConfirm.onYes would emit.
	updated, _ = page.Update(launcherKillConfirmedMsg{pid: 4242})
	page = updated.(LauncherPage)
	if len(page.running) != 0 {
		t.Errorf("running len after kill = %d, want 0", len(page.running))
	}
	if !strings.Contains(page.status, "killed pid=4242") {
		t.Errorf("status = %q, want killed pid=4242", page.status)
	}
}

func TestLauncherPage_FinalizeNegativeKeepsInstance(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	mgr := &fakeManager{}
	page := NewLauncherPage(store, mgr, nil)
	model, _ := page.Update(launchedMsg{inst: domain.RunningInstance{ProfileID: "alpha", PID: 99, Port: 8080, Background: true}})
	page = model.(LauncherPage)

	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	page = updated.(LauncherPage)

	// Esc cancels the confirm without invoking onYes — instance stays.
	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = updated.(LauncherPage)
	if page.killConfirm.Active() {
		t.Error("esc should clear killConfirm")
	}
	if len(page.running) != 1 {
		t.Errorf("negative finalize should keep instance; running len=%d", len(page.running))
	}
}

// TestLauncherPage_KillCompletesViaAsyncMsgs is the regression test for the
// stuck-form bug: huh's confirm reaches StateCompleted only after async
// nextFieldMsg/nextGroupMsg msgs flow back through the non-key path. If
// that path doesn't check for completion + finalize, the form stays
// referenced (View() blanks, IsCapturingInput stays true) until the user
// presses another key. This drives the full teatest event loop end-to-end
// to make sure the kill actually goes through on a single confirm submit.
func TestLauncherPage_KillCompletesViaAsyncMsgs(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	_ = store.Save(domain.Profile{
		ID: "alpha", Name: "Alpha", Model: "/m.gguf",
		Args: map[string]any{"port": float64(8080)},
	})

	mgr := &fakeManager{}
	page := NewLauncherPage(store, mgr, nil)
	tm := teatest.NewTestModel(t, page, teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})
	tm.Send(launchedMsg{inst: domain.RunningInstance{
		ProfileID: "alpha", PID: 4242, Port: 8080, Background: true,
	}})

	// Open kill confirm.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Kill pid=4242?")
	}, teatest.WithDuration(2*time.Second))

	// Submit affirmative via 'y' (huh confirm Accept keymap).
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	// The kill must finalize without further input — confirm form gone,
	// status shows the killed pid, running list empty.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "killed pid=4242")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	final := tm.FinalModel(t).(LauncherPage)
	if final.killConfirm.Active() {
		t.Error("killConfirm should be inactive after async-driven completion")
	}
	if len(final.running) != 0 {
		t.Errorf("running len = %d, want 0", len(final.running))
	}
}

func TestLauncherPage_RefreshReloadsProfiles(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewLauncherPage(store, nil, nil)

	// 0 profiles initially.
	model, _ := page.Update(LauncherProfilesLoadedMsg{Profiles: nil})
	page = model.(LauncherPage)
	if len(page.profiles) != 0 {
		t.Fatalf("initial profiles = %d, want 0", len(page.profiles))
	}

	// Add one to disk.
	if err := store.Save(domain.Profile{ID: "x", Name: "X", Model: "/m.gguf", Args: map[string]any{"port": float64(8080)}}); err != nil {
		t.Fatal(err)
	}

	// Press 'r' -> cmd that yields a fresh launcherProfilesLoadedMsg.
	updated, cmd := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	page = updated.(LauncherPage)
	if cmd == nil {
		t.Fatal("'r' did not produce reload cmd")
	}
	msg := cmd()
	loaded, ok := msg.(LauncherProfilesLoadedMsg)
	if !ok {
		t.Fatalf("got %T, want LauncherProfilesLoadedMsg", msg)
	}
	if len(loaded.Profiles) != 1 || loaded.Profiles[0].ID != "x" {
		t.Errorf("reloaded profiles = %v", loaded.Profiles)
	}
}

func TestLauncherPage_PortBusyHint(t *testing.T) {
	page := NewLauncherPage(nil, nil, nil)
	wrapped := fmt.Errorf("port 8080: %w", processmgr.ErrPortBusy)
	updated, _ := page.Update(launchErrMsg{err: wrapped})
	out := updated.(LauncherPage).View()
	if !strings.Contains(out, "port") || !strings.Contains(out, "in use") {
		t.Fatalf("view missing port-busy hint; got:\n%s", out)
	}
}

func TestLauncherPage_ModelMissingHint(t *testing.T) {
	page := NewLauncherPage(nil, nil, nil)
	wrapped := fmt.Errorf("foo: %w", processmgr.ErrModelNotFound)
	updated, _ := page.Update(launchErrMsg{err: wrapped})
	out := updated.(LauncherPage).View()
	if !strings.Contains(out, "model file not found") {
		t.Fatalf("view missing model-not-found hint; got:\n%s", out)
	}
}

func TestLauncherPage_ForegroundBusyHint(t *testing.T) {
	page := NewLauncherPage(nil, nil, nil)
	updated, _ := page.Update(launchErrMsg{err: processmgr.ErrForegroundBusy})
	out := updated.(LauncherPage).View()
	if !strings.Contains(out, "foreground") || !strings.Contains(out, "[b]") {
		t.Fatalf("view missing foreground-busy hint; got:\n%s", out)
	}
}

func TestLauncherPage_HealthCheckTimeoutHint(t *testing.T) {
	page := NewLauncherPage(nil, nil, nil)
	wrapped := fmt.Errorf("pid 1: %w", processmgr.ErrHealthCheckTimeout)
	updated, _ := page.Update(launchErrMsg{err: wrapped})
	out := updated.(LauncherPage).View()
	if !strings.Contains(out, "healthy") || !strings.Contains(out, "check logs") {
		t.Fatalf("view missing health-timeout hint; got:\n%s", out)
	}
}

func TestLauncherPage_HealthyEmitsSwitchToMonitor(t *testing.T) {
	page := LauncherPage{}
	model, cmd := page.Update(healthyMsg{pid: 4242})
	if cmd == nil {
		t.Fatal("expected Cmd batch with SwitchToMonitorMsg")
	}
	// healthyMsg now returns a tea.Batch (status auto-clear + switch). Drain
	// the batch and look for SwitchToMonitorMsg.
	got := drainCmd(cmd)
	var found *SwitchToMonitorMsg
	for _, m := range got {
		if sw, ok := m.(SwitchToMonitorMsg); ok {
			found = &sw
			break
		}
	}
	if found == nil {
		t.Fatalf("no SwitchToMonitorMsg in cmd batch; got %v", got)
	}
	if found.PID != 4242 {
		t.Fatalf("SwitchToMonitorMsg.PID = %d, want 4242", found.PID)
	}
	_ = model
}

// drainCmd recursively executes a tea.Cmd, returning every concrete tea.Msg
// produced immediately. tea.BatchMsg is a slice of tea.Cmd, each of which may
// itself emit messages or further batches. Each leaf cmd runs with a 50ms
// timeout so delayed ticks (e.g. the 15s scheduleFlashClear) don't block the
// test — those simply contribute no message.
func drainCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(50 * time.Millisecond):
		return nil
	}
	if msg == nil {
		return nil
	}
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, drainCmd(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestLauncherPage_SpinnerVisibleDuringWait(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewLauncherPage(store, &fakeManager{}, nil)
	if page.waitingPID != 0 {
		t.Fatal("waitingPID non-zero on fresh page")
	}

	model, _ := page.Update(launchedMsg{inst: domain.RunningInstance{ProfileID: "alpha", PID: 4242, Port: 8080}})
	page = model.(LauncherPage)
	if page.waitingPID != 4242 {
		t.Errorf("waitingPID = %d after launchedMsg, want 4242", page.waitingPID)
	}
	if !strings.Contains(page.status, "waiting for /health") {
		t.Errorf("status = %q, want waiting-for-health phrasing", page.status)
	}

	model, _ = page.Update(healthyMsg{pid: 4242})
	page = model.(LauncherPage)
	if page.waitingPID != 0 {
		t.Errorf("waitingPID after healthy = %d, want 0", page.waitingPID)
	}
}

func TestLauncherPage_SpinnerClearedOnLaunchErr(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewLauncherPage(store, &fakeManager{}, nil)
	model, _ := page.Update(launchedMsg{inst: domain.RunningInstance{ProfileID: "alpha", PID: 1, Port: 8080}})
	page = model.(LauncherPage)
	model, _ = page.Update(launchErrMsg{err: errors.New("boom")})
	page = model.(LauncherPage)
	if page.waitingPID != 0 {
		t.Errorf("waitingPID after launchErr = %d, want 0", page.waitingPID)
	}
}

func TestLauncherPage_PaneBorderInSplit(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	_ = store.Save(domain.Profile{ID: "alpha", Name: "Alpha", Model: "/m.gguf", Args: map[string]any{"port": float64(8080)}})

	page := NewLauncherPage(store, &fakeManager{}, nil)
	model, _ := page.Update(LauncherProfilesLoadedMsg{Profiles: []domain.Profile{{ID: "alpha", Name: "Alpha", Model: "/m.gguf", Args: map[string]any{"port": float64(8080)}}}})
	page = model.(LauncherPage)
	page.width = 120
	page.height = 30

	// Rounded border characters used by theme.Pane (theme.Border = RoundedBorder).
	out := page.View()
	for _, ch := range []string{"╭", "╮", "╰", "╯"} {
		if !strings.Contains(out, ch) {
			t.Errorf("Launcher view missing pane border char %q; got:\n%s", ch, out)
		}
	}
}

func TestLauncherPage_EmptyStateHint(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewLauncherPage(store, nil, nil)
	if !strings.Contains(page.View(), "no profiles yet") {
		t.Errorf("empty Launcher view missing hint; got:\n%s", page.View())
	}
}

func TestLauncherPage_HintsListPageKeys(t *testing.T) {
	page := NewLauncherPage(nil, nil, nil)
	hints := page.Hints()
	for _, want := range []string{"[b]", "[enter]", "[k]", "[r]"} {
		if !strings.Contains(hints, want) {
			t.Errorf("Hints missing %q; got %q", want, hints)
		}
	}
}
