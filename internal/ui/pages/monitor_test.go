package pages

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

type fakeProcMgr struct {
	insts []domain.RunningInstance
}

func (f *fakeProcMgr) Launch(p domain.Profile, m processmgr.LaunchMode) (domain.RunningInstance, error) {
	return domain.RunningInstance{}, nil
}
func (f *fakeProcMgr) Kill(pid int) error                               { return nil }
func (f *fakeProcMgr) List() []domain.RunningInstance                   { return f.insts }
func (f *fakeProcMgr) WaitHealthy(pid, port int, t time.Duration) error { return nil }
func (f *fakeProcMgr) TailLogs(pid int) (io.ReadCloser, error)          { return nil, nil }

type fakeMonMgr struct{}

func (fakeMonMgr) Subscribe(pid, port int, logPath string) (<-chan monitor.MonitorEvent, func() error, error) {
	ch := make(chan monitor.MonitorEvent)
	return ch, func() error { close(ch); return nil }, nil
}

func TestMonitorPage_RendersInstanceRows(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 1234, Port: 8080, ProfileID: "p1", LogPath: "/tmp/x.log"},
		{PID: 5678, Port: 8081, ProfileID: "p2", LogPath: "/tmp/y.log"},
	}}
	mm := fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)

	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	view := p.View()
	if !strings.Contains(view, "1234") {
		t.Fatalf("view missing pid 1234:\n%s", view)
	}
	if !strings.Contains(view, "5678") {
		t.Fatalf("view missing pid 5678:\n%s", view)
	}
}

func TestMonitorPage_LogsSubViewShowsLines(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}

	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	mm.ch <- monitor.MonitorEvent{Source: monitor.SourceLogs, PID: 1, Data: monitor.LogLine{Line: "boot complete"}}
	p, _ = updateAs[*MonitorPage](p, monitorEventMsg{ev: <-mm.ch})

	v := p.View()
	if !strings.Contains(v, "boot complete") {
		t.Fatalf("logs view missing 'boot complete':\n%s", v)
	}
}

type chanMonMgr struct{ ch chan monitor.MonitorEvent }

func (m *chanMonMgr) Subscribe(pid, port int, logPath string) (<-chan monitor.MonitorEvent, func() error, error) {
	return m.ch, func() error { return nil }, nil
}

type slowCancelMonMgr struct {
	cancel func() error
}

func (s *slowCancelMonMgr) Subscribe(_, _ int, _ string) (<-chan monitor.MonitorEvent, func() error, error) {
	ch := make(chan monitor.MonitorEvent)
	return ch, s.cancel, nil
}

func updateAs[T tea.Model](p tea.Model, msg tea.Msg) (T, tea.Cmd) {
	out, cmd := p.Update(msg)
	return out.(T), cmd
}

func TestMonitorPage_TabCyclesToSlots(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	// Inject a slot snapshot.
	p, _ = updateAs[*MonitorPage](p, monitorEventMsg{ev: monitor.MonitorEvent{
		Source: monitor.SourceSlots, PID: 1,
		Data: monitor.SlotSnapshot{Slots: []monitor.Slot{{ID: 0, State: "idle", NCtxMax: 4096}}},
	}})

	// Press Tab -> sub-view becomes Slots.
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})

	v := p.View()
	if !strings.Contains(v, "idle") {
		t.Fatalf("after Tab, slots view missing 'idle':\n%s", v)
	}
}

func TestMonitorPage_MetricsViewRendersSparkline(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	p, _ = updateAs[*MonitorPage](p, monitorEventMsg{ev: monitor.MonitorEvent{
		Source: monitor.SourceMetrics, PID: 1,
		Data: monitor.Metrics{
			TokensPerSec:   []float64{10, 20, 30, 40, 50},
			RequestsPerSec: []float64{0, 0, 1, 2, 3},
			WindowSeconds:  60,
		},
	}})
	// Tab twice -> Metrics.
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})

	v := p.View()
	if !strings.Contains(v, "tokens/s") {
		t.Fatalf("metrics view missing 'tokens/s':\n%s", v)
	}
	bars := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	found := false
	for _, b := range bars {
		if strings.ContainsRune(v, b) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("metrics view has no sparkline bar")
	}
}

func TestMonitorPage_MetricsPlaceholderWhenEmpty(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 42, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	// Switch to metrics sub-view.
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	out := p.View()
	if !strings.Contains(out, "(no metrics yet") {
		t.Fatalf("metrics view missing placeholder; got:\n%s", out)
	}
}

func TestMonitorPage_KOpensConfirmDoesNotKillImmediately(t *testing.T) {
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{insts: []domain.RunningInstance{{PID: 7, Port: 8080, LogPath: "/tmp/x.log"}}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})

	if pm.killed != 0 {
		t.Fatalf("k should not kill immediately; killed=%d", pm.killed)
	}
	if !p.killConfirm.Active() {
		t.Fatal("expected confirm form after k")
	}
	if !p.IsCapturingInput() {
		t.Fatal("page should capture input while confirm is open")
	}

	// Esc cancels.
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyEsc})
	if p.killConfirm.Active() {
		t.Fatal("esc should clear confirm form")
	}
	if pm.killed != 0 {
		t.Fatalf("esc should not kill; killed=%d", pm.killed)
	}
}

func TestMonitorPage_FinalizeConfirmAffirmativeCallsKill(t *testing.T) {
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{insts: []domain.RunningInstance{{PID: 7, Port: 8080, LogPath: "/tmp/x.log"}}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	// Open the confirm form via the normal key path.
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if !p.killConfirm.Active() {
		t.Fatal("expected confirm form after k")
	}
	// Simulate the affirmative completion path. In production huh drives
	// the form to StateCompleted, Confirm.Update emits the onYes msg and
	// clears the form. Here we replicate both halves: clear the confirm
	// (mirroring Confirm.Update's clear) and inject the msg the onYes
	// callback would emit.
	p.killConfirm = components.Confirm{}
	p, cmd := updateAs[*MonitorPage](p, monitorKillConfirmedMsg{pid: 7})
	if cmd == nil {
		t.Fatal("affirmative completion should return a refresh Cmd")
	}

	if pm.killed != 7 {
		t.Fatalf("expected Kill(7) after affirmative, got Kill(%d)", pm.killed)
	}
	if p.killConfirm.Active() {
		t.Fatal("confirm form should be cleared after completion")
	}
}

func TestMonitorPage_FinalizeConfirmNegativeNoKill(t *testing.T) {
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{insts: []domain.RunningInstance{{PID: 7, Port: 8080, LogPath: "/tmp/x.log"}}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})

	// Esc clears the confirm without invoking onYes.
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyEsc})
	if p.killConfirm.Active() {
		t.Error("esc should clear killConfirm")
	}
	if pm.killed != 0 {
		t.Errorf("negative finalize should not kill; killed=%d", pm.killed)
	}
}

type killTrackingMgr struct {
	fakeProcMgr
	killed int
}

func (m *killTrackingMgr) Kill(pid int) error { m.killed = pid; return nil }

func TestMonitorPage_OrphanSubCleanedOnRefresh(t *testing.T) {
	var cancelCalled int32
	mm := &countingMonMgr{
		ch:       make(chan monitor.MonitorEvent, 8),
		onCancel: func() { atomic.AddInt32(&cancelCalled, 1) },
	}
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{insts: []domain.RunningInstance{{PID: 7, Port: 8080, LogPath: "/tmp/x.log"}}}}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	// Initial refresh creates a subscription for pid 7.
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	if _, ok := p.subs[7]; !ok {
		t.Fatal("expected sub for pid 7 after initial refresh")
	}

	// Manager-side the pid is gone (e.g., killed externally or crashed).
	pm.fakeProcMgr.insts = nil
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	// Cancel runs asynchronously off the UI thread; poll briefly.
	for i := 0; i < 100 && atomic.LoadInt32(&cancelCalled) == 0; i++ {
		time.Sleep(2 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&cancelCalled); got != 1 {
		t.Fatalf("cancel called %d times, want 1 (orphan sub not cancelled)", got)
	}
	if _, ok := p.subs[7]; ok {
		t.Fatal("orphan sub for pid 7 not deleted")
	}
}

type countingMonMgr struct {
	ch       chan monitor.MonitorEvent
	onCancel func()
}

func (m *countingMonMgr) Subscribe(pid, port int, logPath string) (<-chan monitor.MonitorEvent, func() error, error) {
	return m.ch, func() error {
		if m.onCancel != nil {
			m.onCancel()
		}
		return nil
	}, nil
}

func TestMonitorPage_SelectsRowByPID(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 100, Port: 8080, LogPath: "/tmp/a.log"},
		{PID: 200, Port: 8081, LogPath: "/tmp/b.log"},
		{PID: 300, Port: 8082, LogPath: "/tmp/c.log"},
	}}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	// Public handler queues a refresh; pendingSelectPID is consumed by the
	// refresh handler when it lands. We drain the single refresh cmd here.
	p, cmd := updateAs[*MonitorPage](p, MonitorSelectPIDMsg{PID: 200})
	if cmd == nil {
		t.Fatalf("MonitorSelectPIDMsg should issue refreshInstancesCmd")
	}
	p, _ = updateAs[*MonitorPage](p, cmd())

	if got := p.selectedPID(); got != 200 {
		t.Fatalf("selectedPID = %d, want 200", got)
	}
}

func TestMonitorPage_PendingSelectAppliesAfterFirstRefresh(t *testing.T) {
	// Simulates: SwitchToMonitorMsg fires before MonitorPage has any rows
	// (e.g., very first instance ever launched). pendingSelectPID must
	// wait for the refresh to land, then position the cursor.
	pm := &fakeProcMgr{insts: nil} // initially empty
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)

	// Public msg arrives while rows are still empty.
	p, cmd := updateAs[*MonitorPage](p, MonitorSelectPIDMsg{PID: 200})
	if cmd == nil {
		t.Fatalf("MonitorSelectPIDMsg should issue refreshInstancesCmd")
	}
	// selectedPID is 0 right now (no rows), even though MonitorSelectPIDMsg arrived.
	if got := p.selectedPID(); got != 0 {
		t.Fatalf("selectedPID before refresh = %d, want 0", got)
	}
	// Now the registry gets the instance and the refresh cmd lands.
	pm.insts = []domain.RunningInstance{
		{PID: 100, Port: 8080, LogPath: "/tmp/a.log"},
		{PID: 200, Port: 8081, LogPath: "/tmp/b.log"},
	}
	p, _ = updateAs[*MonitorPage](p, cmd())
	if got := p.selectedPID(); got != 200 {
		t.Fatalf("selectedPID after refresh = %d, want 200 (pendingSelectPID should have been consumed)", got)
	}
}

func TestMonitorPage_HandlesWindowSize(t *testing.T) {
	pm := &fakeProcMgr{}
	mm := fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if p.width != 80 || p.height != 24 {
		t.Fatalf("after WindowSizeMsg: width=%d height=%d, want 80x24", p.width, p.height)
	}
}

func TestMonitorPage_ROpensRestartConfirm(t *testing.T) {
	prof := domain.Profile{ID: "qwen", Name: "Qwen", Model: "/tmp/x.gguf", Args: map[string]any{"port": 8080.0}}
	psk := &fakeProfileStore{p: prof}
	pm := &restartTrackingMgr{
		insts:   []domain.RunningInstance{{ProfileID: "qwen", PID: 100, Port: 8080, LogPath: "/tmp/a.log", Background: true}},
		newPID:  200,
		newPort: 8080,
	}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, psk)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	if !p.restartConfirm.Active() {
		t.Fatal("expected restart confirm form after r")
	}
	if !p.IsCapturingInput() {
		t.Fatal("page should capture input while restart confirm is open")
	}

	// Esc cancels.
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyEsc})
	if p.restartConfirm.Active() {
		t.Fatal("esc should clear restart confirm form")
	}
	if pm.killedPID != 0 {
		t.Fatalf("esc should not kill; killed=%d", pm.killedPID)
	}
}

func TestMonitorPage_RestartConfirmAffirmativeKillsAndLaunches(t *testing.T) {
	prof := domain.Profile{ID: "qwen", Name: "Qwen", Model: "/tmp/x.gguf", Args: map[string]any{"port": 8080.0}}
	psk := &fakeProfileStore{p: prof}
	pm := &restartTrackingMgr{
		insts:   []domain.RunningInstance{{ProfileID: "qwen", PID: 100, Port: 8080, LogPath: "/tmp/a.log", Background: true}},
		newPID:  200,
		newPort: 8080,
	}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, psk)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !p.restartConfirm.Active() {
		t.Fatal("expected restart confirm form")
	}

	// Affirmative completion: emit the msg restartConfirm.onYes would build.
	p.restartConfirm = components.Confirm{}
	_, cmd := updateAs[*MonitorPage](p, monitorRestartConfirmedMsg{
		pid:        100,
		profile:    prof,
		background: true,
	})
	if cmd == nil {
		t.Fatal("affirmative completion should return a Cmd")
	}
	// Drain to find the restartResultMsg.
	got := drainCmd(cmd)
	var rr *restartResultMsg
	for _, m := range got {
		if v, ok := m.(restartResultMsg); ok {
			rr = &v
			break
		}
	}
	if rr == nil {
		t.Fatalf("no restartResultMsg in cmd batch; got %v", got)
	}
	if rr.err != nil {
		t.Fatalf("restartResultMsg.err = %v", rr.err)
	}

	if pm.killedPID != 100 {
		t.Errorf("killedPID = %d, want 100", pm.killedPID)
	}
	if pm.launchedID != "qwen" {
		t.Errorf("launchedID = %q, want qwen", pm.launchedID)
	}
	if pm.launchMode != processmgr.LaunchBackground {
		t.Errorf("launchMode = %v, want LaunchBackground", pm.launchMode)
	}
}

func TestMonitorPage_RestartConfirmNegativeNoAction(t *testing.T) {
	prof := domain.Profile{ID: "qwen", Name: "Qwen", Model: "/tmp/x.gguf", Args: map[string]any{"port": 8080.0}}
	psk := &fakeProfileStore{p: prof}
	pm := &restartTrackingMgr{
		insts:   []domain.RunningInstance{{ProfileID: "qwen", PID: 100, Port: 8080, LogPath: "/tmp/a.log", Background: true}},
		newPID:  200,
		newPort: 8080,
	}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, psk)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	// Esc clears the confirm without invoking onYes.
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyEsc})
	if p.restartConfirm.Active() {
		t.Error("esc should clear restartConfirm")
	}
	if pm.killedPID != 0 {
		t.Errorf("negative finalize should not kill; killed=%d", pm.killedPID)
	}
}

func TestMonitorPage_RestartConfirmForegroundPreservesMode(t *testing.T) {
	prof := domain.Profile{ID: "qwen", Name: "Qwen", Model: "/tmp/x.gguf", Args: map[string]any{"port": 8080.0}}
	psk := &fakeProfileStore{p: prof}
	pm := &restartTrackingMgr{
		insts:   []domain.RunningInstance{{ProfileID: "qwen", PID: 100, Port: 8080, LogPath: "/tmp/a.log"}}, // Background: false
		newPID:  200,
		newPort: 8080,
	}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, psk)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})

	// Foreground instance — confirm should preserve mode through to the
	// monitorRestartConfirmedMsg's background flag.
	p.restartConfirm = components.Confirm{}
	_, cmd := updateAs[*MonitorPage](p, monitorRestartConfirmedMsg{
		pid:        100,
		profile:    prof,
		background: false,
	})
	if cmd == nil {
		t.Fatal("affirmative completion should return a Cmd")
	}
	got := drainCmd(cmd)
	var rr *restartResultMsg
	for _, m := range got {
		if v, ok := m.(restartResultMsg); ok {
			rr = &v
			break
		}
	}
	if rr == nil {
		t.Fatalf("no restartResultMsg in cmd batch; got %v", got)
	}
	if rr.err != nil {
		t.Fatalf("restartResultMsg.err = %v", rr.err)
	}
	if pm.launchMode != processmgr.LaunchForeground {
		t.Errorf("launchMode = %v, want LaunchForeground", pm.launchMode)
	}
}

func TestMonitorPage_RFlashWhenStoreNil(t *testing.T) {
	pm := &restartTrackingMgr{
		insts: []domain.RunningInstance{{ProfileID: "qwen", PID: 100, Port: 8080, LogPath: "/tmp/a.log", Background: true}},
	}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil) // store nil
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if p.restartConfirm.Active() {
		t.Fatal("nil store should not open confirm form")
	}
	if !strings.Contains(p.flash, "store not available") {
		t.Errorf("expected flash about missing store; got %q", p.flash)
	}
}

func TestMonitorPage_RFlashWhenProfileMissing(t *testing.T) {
	psk := &fakeProfileStore{err: errors.New("profile vanished")}
	pm := &restartTrackingMgr{
		insts: []domain.RunningInstance{{ProfileID: "qwen", PID: 100, Port: 8080, LogPath: "/tmp/a.log", Background: true}},
	}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, psk)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if p.restartConfirm.Active() {
		t.Fatal("missing profile should not open confirm form")
	}
	if !strings.Contains(p.flash, "not found") {
		t.Errorf("expected flash about missing profile; got %q", p.flash)
	}
}

func TestMonitorPage_RestartResultMsgSetsFlashOnError(t *testing.T) {
	pm := &fakeProcMgr{}
	mm := fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)

	p, _ = updateAs[*MonitorPage](p, restartResultMsg{pid: 42, err: errors.New("kill refused")})
	if !strings.Contains(p.flash, "kill refused") {
		t.Errorf("expected flash to contain error; got %q", p.flash)
	}
}

type fakeProfileStore struct {
	p   domain.Profile
	err error
}

func (f *fakeProfileStore) Get(id string) (domain.Profile, error) {
	if f.err != nil {
		return domain.Profile{}, f.err
	}
	return f.p, nil
}

type restartTrackingMgr struct {
	insts      []domain.RunningInstance
	killedPID  int
	launchedID string
	launchMode processmgr.LaunchMode
	newPID     int
	newPort    int
}

func (r *restartTrackingMgr) List() []domain.RunningInstance { return r.insts }
func (r *restartTrackingMgr) Kill(pid int) error             { r.killedPID = pid; return nil }
func (r *restartTrackingMgr) TailLogs(_ int) (io.ReadCloser, error) {
	return nil, processmgr.ErrUnknownPID
}
func (r *restartTrackingMgr) Launch(p domain.Profile, mode processmgr.LaunchMode) (domain.RunningInstance, error) {
	r.launchedID = p.ID
	r.launchMode = mode
	return domain.RunningInstance{ProfileID: p.ID, PID: r.newPID, Port: r.newPort, Background: true}, nil
}

func TestMonitorPage_CrashedRowShowsMarker(t *testing.T) {
	exit := time.Now().UTC()
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{
		PID:       777,
		Port:      8080,
		ProfileID: "qwen",
		LogPath:   "/tmp/x.log",
		Crashed:   true,
		ExitedAt:  &exit,
	}}}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	out := p.View()
	if !strings.Contains(out, "✗") && !strings.Contains(out, "crashed") {
		t.Fatalf("crashed row missing badge; got:\n%s", out)
	}
}

func TestMonitorPage_PeriodicRefreshTickEmitsRefreshCmd(t *testing.T) {
	pm := &fakeProcMgr{}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	cmd := p.Init()
	if cmd == nil {
		t.Fatal("Init returned nil")
	}
	if !p.periodicTickActive {
		t.Errorf("periodicTickActive = false; want true")
	}
}

func TestMonitorPage_CancelOrphanIsAsync(t *testing.T) {
	// Cancel func will block until release is signaled — simulating a
	// stuck nvidia-smi that takes time to reap. The test asserts that
	// applyInstances returns promptly without blocking on cancel.
	release := make(chan struct{})
	var cancelStarted, cancelFinished int32
	mm := &slowCancelMonMgr{
		cancel: func() error {
			atomic.AddInt32(&cancelStarted, 1)
			<-release
			atomic.AddInt32(&cancelFinished, 1)
			return nil
		},
	}
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	p := NewMonitorPage(pm, mm, nil)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	pm.insts = nil // PID 1 vanished -> cancel should be triggered

	done := make(chan struct{})
	go func() {
		p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
		close(done)
	}()
	select {
	case <-done:
		// applyInstances returned promptly even though cancel still hasn't completed.
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("applyInstances blocked on cancel; cancelStarted=%d", atomic.LoadInt32(&cancelStarted))
	}
	// Now release the cancel.
	close(release)
	for i := 0; i < 100 && atomic.LoadInt32(&cancelFinished) == 0; i++ {
		time.Sleep(2 * time.Millisecond)
	}
	if atomic.LoadInt32(&cancelFinished) != 1 {
		t.Fatalf("cancel never completed after release; finished=%d", atomic.LoadInt32(&cancelFinished))
	}
}

func TestMonitorPage_RowShowsUptime(t *testing.T) {
	started := time.Now().Add(-3*time.Minute - 12*time.Second)
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 9, Port: 8080, StartedAt: started, LogPath: "/tmp/z.log"}}}
	mm := fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	rows := p.tbl.Rows()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0][3] != "3m12s" {
		t.Errorf("uptime cell = %q, want %q", rows[0][3], "3m12s")
	}
}

func TestMonitorPage_RowShowsVRAMAndTokens(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 7, Port: 8080, StartedAt: time.Now(), LogPath: "/tmp/x.log"}}}
	mm := fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	// Inject a synthetic subState so the next refresh picks up the metrics.
	st := p.subs[7]
	if st == nil {
		t.Fatalf("expected subState for pid 7 after refresh")
	}
	st.gpu = monitor.GPUStats{VRAMUsedMB: 1024, VRAMTotalMB: 24576}
	st.mets = monitor.Metrics{TokensPerSec: []float64{10.5, 12.3}}

	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	rows := p.tbl.Rows()
	if rows[0][4] != "1024/24576MB" {
		t.Errorf("vram cell = %q, want %q", rows[0][4], "1024/24576MB")
	}
	if rows[0][5] != "12.3" {
		t.Errorf("tokens cell = %q, want %q", rows[0][5], "12.3")
	}
}

func TestMonitorPage_RowFallbackDashesWhenNoState(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, StartedAt: time.Time{}, LogPath: "/tmp/x.log"}}}
	mm := fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	rows := p.tbl.Rows()
	if rows[0][3] != "--" || rows[0][4] != "--" || rows[0][5] != "--" {
		t.Errorf("fallback cells = %q/%q/%q, want all '--'", rows[0][3], rows[0][4], rows[0][5])
	}
}

func TestMonitorPage_CrashedRowStyledViaThemeError(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 13, Port: 8080, ProfileID: "p1", Crashed: true, LogPath: "/tmp/x.log"}}}
	mm := fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	rows := p.tbl.Rows()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0][0] != theme.Error.Render("✗ 13") {
		t.Errorf("crashed pid cell not styled via theme.Error; got %q want %q", rows[0][0], theme.Error.Render("✗ 13"))
	}
	if !strings.Contains(rows[0][2], "crashed") {
		t.Errorf("profile cell should mention crashed; got %q", rows[0][2])
	}
}

func TestMonitorPage_HealthyRowUnstyled(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 13, Port: 8080, ProfileID: "p1", LogPath: "/tmp/x.log"}}}
	mm := fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	rows := p.tbl.Rows()
	if rows[0][0] != "13" {
		t.Errorf("healthy pid cell should be plain %q; got %q", "13", rows[0][0])
	}
}

func TestMonitorPage_SelectedPIDParsesAcrossANSI(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 13, Port: 8080, ProfileID: "p1", Crashed: true, LogPath: "/tmp/x.log"}}}
	mm := fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	if got := p.selectedPID(); got != 13 {
		t.Errorf("selectedPID = %d, want 13 (despite ANSI)", got)
	}
}

func TestMonitorPage_EmptyStateHint(t *testing.T) {
	pm := &fakeProcMgr{insts: nil}
	mm := fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	out := p.View()
	if !strings.Contains(out, "no instances running") {
		t.Errorf("empty Monitor view missing hint; got:\n%s", out)
	}
	if !strings.Contains(out, "Launcher [1]") {
		t.Errorf("empty Monitor view should reference Launcher tab as [1]; got:\n%s", out)
	}
}

func TestMonitorPage_SubViewTabsHighlightActive(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	if got := renderSubViewTabs(SubViewLogs); got != theme.TabActive.Render("Logs")+theme.Subtitle.Render(" │ ")+theme.TabInactive.Render("Slots")+theme.Subtitle.Render(" │ ")+theme.TabInactive.Render("Metrics") {
		t.Errorf("renderSubViewTabs(Logs) shape mismatch; got %q", got)
	}

	out := p.View()
	if !strings.Contains(out, theme.TabActive.Render("Logs")) {
		t.Errorf("view missing active Logs tab; got:\n%s", out)
	}

	// Cycle.
	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	out = p.View()
	if !strings.Contains(out, theme.TabActive.Render("Slots")) {
		t.Errorf("view missing active Slots tab after [v]; got:\n%s", out)
	}
}

func TestMonitorPage_LogsShowingNofMFooter(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	// 5 lines — footer absent.
	st := p.subs[1]
	st.logs = []string{"a", "b", "c", "d", "e"}
	if strings.Contains(p.View(), "showing last 10") {
		t.Errorf("5-log view should not show count footer; got:\n%s", p.View())
	}

	// 11 lines — footer present.
	st.logs = make([]string, 11)
	for i := range st.logs {
		st.logs[i] = fmt.Sprintf("line %d", i)
	}
	if !strings.Contains(p.View(), "showing last 10 of 11") {
		t.Errorf("11-log view should show count footer; got:\n%s", p.View())
	}
}

func TestMonitorPage_PausedIndicator(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	if strings.Contains(p.View(), "PAUSED") {
		t.Fatalf("idle view should not contain PAUSED; got:\n%s", p.View())
	}

	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeySpace})
	if !strings.Contains(p.View(), "PAUSED") {
		t.Errorf("paused view missing PAUSED; got:\n%s", p.View())
	}

	p, _ = updateAs[*MonitorPage](p, tea.KeyMsg{Type: tea.KeySpace})
	if strings.Contains(p.View(), "PAUSED") {
		t.Errorf("resumed view still contains PAUSED; got:\n%s", p.View())
	}
}

func TestMonitorPage_HintsListPageKeys(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p, _ = updateAs[*MonitorPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	hints := p.Hints()
	for _, want := range []string{"[v]", "[k]", "[r]", "[Space]"} {
		if !strings.Contains(hints, want) {
			t.Errorf("Hints missing %q; got %q", want, hints)
		}
	}
}

// ---- subState.Apply (CQ-008) ----

func TestSubState_Apply_LogLine(t *testing.T) {
	s := &subState{}
	ev := monitor.MonitorEvent{
		Source: monitor.SourceLogs,
		PID:    42,
		Data:   monitor.LogLine{Line: "hello"},
	}
	s.Apply(ev, false)
	if len(s.logs) != 1 || s.logs[0] != "hello" {
		t.Fatalf("expected logs=[hello]; got %#v", s.logs)
	}
}

func TestSubState_Apply_LogLine_Paused(t *testing.T) {
	s := &subState{logs: []string{"prev"}}
	ev := monitor.MonitorEvent{
		Source: monitor.SourceLogs,
		PID:    42,
		Data:   monitor.LogLine{Line: "dropped"},
	}
	s.Apply(ev, true)
	if len(s.logs) != 1 || s.logs[0] != "prev" {
		t.Fatalf("paused: expected logs unchanged ([prev]); got %#v", s.logs)
	}
}

func TestSubState_Apply_LogLine_CapAt2000(t *testing.T) {
	s := &subState{}
	for i := 0; i < 2000; i++ {
		s.logs = append(s.logs, fmt.Sprintf("line-%d", i))
	}
	ev := monitor.MonitorEvent{
		Source: monitor.SourceLogs,
		PID:    1,
		Data:   monitor.LogLine{Line: "overflow"},
	}
	s.Apply(ev, false)
	if len(s.logs) != 2000 {
		t.Fatalf("expected log buffer capped at 2000; got %d", len(s.logs))
	}
	if s.logs[0] != "line-1" {
		t.Fatalf("expected oldest line evicted (head=line-1); got %q", s.logs[0])
	}
	if s.logs[1999] != "overflow" {
		t.Fatalf("expected newest line at tail (=overflow); got %q", s.logs[1999])
	}
}

func TestSubState_Apply_SlotSnapshot(t *testing.T) {
	s := &subState{}
	snap := monitor.SlotSnapshot{Slots: []monitor.Slot{{ID: 0, State: "processing"}}}
	ev := monitor.MonitorEvent{
		Source: monitor.SourceSlots,
		PID:    7,
		Data:   snap,
	}
	s.Apply(ev, false)
	if len(s.slots.Slots) != 1 || s.slots.Slots[0].State != "processing" {
		t.Fatalf("expected slots replaced with snap; got %#v", s.slots)
	}
}

func TestSubState_Apply_GPUStats(t *testing.T) {
	s := &subState{}
	stats := monitor.GPUStats{VRAMUsedMB: 1234, VRAMTotalMB: 24000, Utilization: 55.5, Source: "nvidia-smi"}
	ev := monitor.MonitorEvent{
		Source: monitor.SourceGPU,
		PID:    9,
		Data:   stats,
	}
	s.Apply(ev, false)
	if s.gpu != stats {
		t.Fatalf("expected gpu=%#v; got %#v", stats, s.gpu)
	}
}

func TestSubState_Apply_HealthStatus(t *testing.T) {
	s := &subState{}
	hs := monitor.HealthStatus{OK: true, Status: "ok"}
	ev := monitor.MonitorEvent{
		Source: monitor.SourceHealth,
		PID:    11,
		Data:   hs,
	}
	s.Apply(ev, false)
	if s.health != hs {
		t.Fatalf("expected health=%#v; got %#v", hs, s.health)
	}
}

func TestSubState_Apply_Metrics(t *testing.T) {
	s := &subState{}
	mets := monitor.Metrics{
		TokensPerSec:   []float64{1, 2, 3},
		RequestsPerSec: []float64{0.1, 0.2},
		WindowSeconds:  60,
	}
	ev := monitor.MonitorEvent{
		Source: monitor.SourceMetrics,
		PID:    13,
		Data:   mets,
	}
	s.Apply(ev, false)
	if s.mets.WindowSeconds != 60 || len(s.mets.TokensPerSec) != 3 || len(s.mets.RequestsPerSec) != 2 {
		t.Fatalf("expected mets replaced with rolling window; got %#v", s.mets)
	}
}

// --- CQ-003: per-helper tests for the decomposed applyInstances pipeline ---

// TestMonitorPage_ApplyInstances_RendersRows feeds N instances and asserts the
// table has N rows whose key columns reflect the input.
func TestMonitorPage_ApplyInstances_RendersRows(t *testing.T) {
	pm := &fakeProcMgr{}
	mm := fakeMonMgr{}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)

	insts := []domain.RunningInstance{
		{PID: 11, Port: 7000, ProfileID: "alpha", LogPath: "/tmp/a.log"},
		{PID: 22, Port: 7001, ProfileID: "beta", LogPath: "/tmp/b.log"},
		{PID: 33, Port: 7002, ProfileID: "gamma", LogPath: "/tmp/c.log"},
	}
	p.applyInstances(insts)

	rows := p.tbl.Rows()
	if len(rows) != 3 {
		t.Fatalf("rows: got %d, want 3", len(rows))
	}
	for i, want := range insts {
		if !strings.Contains(rows[i][0], fmt.Sprintf("%d", want.PID)) {
			t.Fatalf("row %d pid col %q missing pid %d", i, rows[i][0], want.PID)
		}
		if !strings.Contains(rows[i][1], fmt.Sprintf("%d", want.Port)) {
			t.Fatalf("row %d port col %q missing port %d", i, rows[i][1], want.Port)
		}
		if !strings.Contains(rows[i][2], want.ProfileID) {
			t.Fatalf("row %d profile col %q missing %q", i, rows[i][2], want.ProfileID)
		}
	}
}

// TestMonitorPage_ApplyInstances_EnsuresSubscription verifies that a fresh
// non-crashed instance gets registered in p.subs and p.chans.
func TestMonitorPage_ApplyInstances_EnsuresSubscription(t *testing.T) {
	pm := &fakeProcMgr{}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 1)}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)

	p.applyInstances([]domain.RunningInstance{
		{PID: 42, Port: 8080, ProfileID: "p1", LogPath: "/tmp/x.log"},
	})

	if st, ok := p.subs[42]; !ok || st == nil {
		t.Fatalf("expected p.subs[42] to be non-nil, got ok=%v st=%v", ok, st)
	}
	if ch, ok := p.chans[42]; !ok || ch == nil {
		t.Fatalf("expected p.chans[42] to be non-nil, got ok=%v ch=%v", ok, ch)
	}
}

// TestMonitorPage_ApplyInstances_ReapsCrashed pre-populates a subscription
// then feeds the same PID with Crashed=true. The collapsed reap loop must
// drop both the subs and chans entry for that PID.
func TestMonitorPage_ApplyInstances_ReapsCrashed(t *testing.T) {
	var cancelCalled int32
	pm := &fakeProcMgr{}
	mm := &countingMonMgr{
		ch:       make(chan monitor.MonitorEvent, 1),
		onCancel: func() { atomic.AddInt32(&cancelCalled, 1) },
	}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)

	// Bring up the sub via a normal (non-crashed) refresh.
	p.applyInstances([]domain.RunningInstance{
		{PID: 99, Port: 8080, ProfileID: "p1", LogPath: "/tmp/x.log"},
	})
	if _, ok := p.subs[99]; !ok {
		t.Fatal("setup: expected sub for pid 99")
	}

	// Now the same PID is reported as crashed.
	p.applyInstances([]domain.RunningInstance{
		{PID: 99, Port: 8080, ProfileID: "p1", LogPath: "/tmp/x.log", Crashed: true},
	})

	if _, ok := p.subs[99]; ok {
		t.Fatal("expected p.subs[99] to be deleted after crash")
	}
	if _, ok := p.chans[99]; ok {
		t.Fatal("expected p.chans[99] to be deleted after crash")
	}
	for i := 0; i < 100 && atomic.LoadInt32(&cancelCalled) == 0; i++ {
		time.Sleep(2 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&cancelCalled); got != 1 {
		t.Fatalf("cancel called %d times, want 1", got)
	}
}

// TestMonitorPage_ApplyInstances_ReapsOrphan pre-populates two PIDs (A, B)
// then feeds the input list with only A. B's subscription must be dropped.
func TestMonitorPage_ApplyInstances_ReapsOrphan(t *testing.T) {
	var cancelCalled int32
	pm := &fakeProcMgr{}
	mm := &countingMonMgr{
		ch:       make(chan monitor.MonitorEvent, 4),
		onCancel: func() { atomic.AddInt32(&cancelCalled, 1) },
	}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)

	// Bring up subs for A=10 and B=20.
	p.applyInstances([]domain.RunningInstance{
		{PID: 10, Port: 8080, ProfileID: "p1", LogPath: "/tmp/a.log"},
		{PID: 20, Port: 8081, ProfileID: "p2", LogPath: "/tmp/b.log"},
	})
	if _, ok := p.subs[10]; !ok {
		t.Fatal("setup: expected sub for pid 10")
	}
	if _, ok := p.subs[20]; !ok {
		t.Fatal("setup: expected sub for pid 20")
	}

	// Input now omits PID 20 entirely (orphan).
	p.applyInstances([]domain.RunningInstance{
		{PID: 10, Port: 8080, ProfileID: "p1", LogPath: "/tmp/a.log"},
	})

	if _, ok := p.subs[10]; !ok {
		t.Fatal("expected p.subs[10] to be retained")
	}
	if _, ok := p.subs[20]; ok {
		t.Fatal("expected p.subs[20] to be deleted (orphan)")
	}
	if _, ok := p.chans[20]; ok {
		t.Fatal("expected p.chans[20] to be deleted (orphan)")
	}
	for i := 0; i < 100 && atomic.LoadInt32(&cancelCalled) == 0; i++ {
		time.Sleep(2 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&cancelCalled); got != 1 {
		t.Fatalf("cancel called %d times, want 1 (only orphan B)", got)
	}
}

// TestMonitorPage_ApplyInstances_NoLeakOnAllCrashed launches N instances,
// then crashes all of them in a single applyInstances call. Both maps must
// be empty afterwards — proving the collapsed reap loop covers the
// "everything died at once" case.
func TestMonitorPage_ApplyInstances_NoLeakOnAllCrashed(t *testing.T) {
	var cancelCalled int32
	pm := &fakeProcMgr{}
	mm := &countingMonMgr{
		ch:       make(chan monitor.MonitorEvent, 8),
		onCancel: func() { atomic.AddInt32(&cancelCalled, 1) },
	}
	p := NewMonitorPage(pm, mm, nil)
	p.SetSize(120, 30)

	live := []domain.RunningInstance{
		{PID: 1, Port: 8080, ProfileID: "p1", LogPath: "/tmp/1.log"},
		{PID: 2, Port: 8081, ProfileID: "p2", LogPath: "/tmp/2.log"},
		{PID: 3, Port: 8082, ProfileID: "p3", LogPath: "/tmp/3.log"},
	}
	p.applyInstances(live)
	if len(p.subs) != 3 || len(p.chans) != 3 {
		t.Fatalf("setup: got subs=%d chans=%d, want 3/3", len(p.subs), len(p.chans))
	}

	crashed := make([]domain.RunningInstance, len(live))
	for i, ri := range live {
		ri.Crashed = true
		crashed[i] = ri
	}
	p.applyInstances(crashed)

	if len(p.subs) != 0 {
		t.Fatalf("expected p.subs empty after all-crashed, got %d entries", len(p.subs))
	}
	if len(p.chans) != 0 {
		t.Fatalf("expected p.chans empty after all-crashed, got %d entries", len(p.chans))
	}
	for i := 0; i < 100 && atomic.LoadInt32(&cancelCalled) < 3; i++ {
		time.Sleep(2 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&cancelCalled); got != 3 {
		t.Fatalf("cancel called %d times, want 3 (one per crashed instance)", got)
	}
}
