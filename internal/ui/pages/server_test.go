package pages

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
	"github.com/quantmind-br/model-loader/internal/service/metricsstore"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// drainCmd executes a tea.Cmd and flattens any BatchMsg recursively,
// returning all leaf messages for assertions.
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

type fakeProcMgr struct {
	insts        []domain.RunningInstance
	history      []domain.ExitedInstance
	refreshCalls int
	markedStops  []int
}

func (f *fakeProcMgr) Kill(pid int) error                      { return nil }
func (f *fakeProcMgr) List() []domain.RunningInstance          { return f.insts }
func (f *fakeProcMgr) TailLogs(pid int) (io.ReadCloser, error) { return nil, nil }
func (f *fakeProcMgr) History() []domain.ExitedInstance        { return f.history }
func (f *fakeProcMgr) RefreshFromDisk() error                  { f.refreshCalls++; return nil }
func (f *fakeProcMgr) MarkOperatorStop(pid int)                { f.markedStops = append(f.markedStops, pid) }

type fakeMonMgr struct{}

func (fakeMonMgr) Subscribe(pid, port int, logPath string) (<-chan monitor.MonitorEvent, func() error, error) {
	ch := make(chan monitor.MonitorEvent)
	return ch, func() error { close(ch); return nil }, nil
}

// fakeProxyForPages implements serverProxyController, recording the order of
// Unload/Load calls so restart tests can assert swap semantics.
type fakeProxyForPages struct {
	status     httpproxy.Status
	statusSeq  []httpproxy.Status // when non-empty, Status() pops from here first
	ops        []string
	loadErr    error
	unloadErr  error
	forceStops int
	stops      int
}

func (f *fakeProxyForPages) Start(context.Context) error { return nil }
func (f *fakeProxyForPages) Stop(context.Context) error {
	f.stops++
	return nil
}
func (f *fakeProxyForPages) Status() httpproxy.Status {
	if len(f.statusSeq) > 0 {
		st := f.statusSeq[0]
		f.statusSeq = f.statusSeq[1:]
		return st
	}
	return f.status
}
func (f *fakeProxyForPages) BaseURL() string                     { return "http://127.0.0.1:9999" }
func (f *fakeProxyForPages) EnsureRunning(context.Context) error { return nil }
func (f *fakeProxyForPages) ForceStop() error {
	f.forceStops++
	f.ops = append(f.ops, "forcestop")
	return nil
}
func (f *fakeProxyForPages) Load(_ context.Context, id string) (httpproxy.Status, error) {
	f.ops = append(f.ops, "load "+id) // record the attempt even when it fails
	if f.loadErr != nil {
		return httpproxy.Status{}, f.loadErr
	}
	f.status.LoadedProfileID = id
	return f.status, nil
}
func (f *fakeProxyForPages) Unload(_ context.Context, force bool) (httpproxy.Status, error) {
	if force {
		f.ops = append(f.ops, "unload force")
	} else {
		f.ops = append(f.ops, "unload")
	}
	if f.unloadErr != nil {
		return httpproxy.Status{}, f.unloadErr
	}
	f.status.LoadedProfileID = ""
	f.status.LoadedPID = 0
	return f.status, nil
}

func TestServerPage_RendersInstanceRows(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 1234, Port: 8080, ProfileID: "p1", LogPath: "/tmp/x.log"},
		{PID: 5678, Port: 8081, ProfileID: "p2", LogPath: "/tmp/y.log"},
	}}
	mm := fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)

	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	view := p.View()
	if !strings.Contains(view, "1234") {
		t.Fatalf("view missing pid 1234:\n%s", view)
	}
	if !strings.Contains(view, "5678") {
		t.Fatalf("view missing pid 5678:\n%s", view)
	}
}

func TestServerPage_TableFits80Columns(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 12345, Port: 8080, ProfileID: "very-long-profile-id-123", LogPath: "/tmp/x.log"},
	}}
	p := NewServerPage(pm, &fakeMonMgr{}, nil)
	p.SetSize(80, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	for _, line := range strings.Split(p.renderTable(), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if w := lipgloss.Width(line); w > 80 {
			t.Errorf("table line width = %d, want <= 80: %q", w, line)
		}
	}
}

func TestServerPage_HistoryFits80Columns(t *testing.T) {
	p := NewServerPage(&fakeProcMgr{}, &fakeMonMgr{}, nil)
	p.SetSize(80, 30)
	p.history = []domain.ExitedInstance{
		{
			ProfileID:       "a-very-long-profile-identifier-that-would-overflow-the-row",
			PID:             1234567,
			StartedAt:       time.Now().Add(-2 * time.Hour),
			ExitedAt:        time.Now().Add(-1 * time.Hour),
			DurationSeconds: 3600,
			ExitReason:      "signal:SIGKILL killed by the oom reaper for a very long reason",
			StderrTail:      []string{"line1", "line2", "line3"},
		},
	}
	for _, line := range strings.Split(p.renderHistory(), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if w := lipgloss.Width(line); w > 80 {
			t.Errorf("history line width = %d, want <= 80: %q", w, line)
		}
	}
}

func TestServerPage_FullViewFits80Columns(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 12345, Port: 8080, ProfileID: "very-long-profile-id-123", LogPath: "/tmp/x.log"},
	}}
	p := NewServerPage(pm, &fakeMonMgr{}, nil)
	p.SetSize(80, 24)
	p.WithProxy(&fakeProxyForPages{
		status: httpproxy.Status{Running: true, Addr: "http://127.0.0.1:8080", LoadedProfileID: "qwen", LastSwapAt: time.Now(), LastSwapDur: 140 * time.Millisecond, InflightRequests: 1, LastError: "backend timeout"},
	})
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	for _, line := range strings.Split(p.View(), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if w := lipgloss.Width(line); w > 80 {
			t.Errorf("full view line width = %d, want <= 80: %q", w, line)
		}
	}
}

func TestServerPage_NoGapBetweenTableAndSubTabs(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 1234, Port: 8080, ProfileID: "p1", LogPath: "/tmp/x.log"},
	}}
	p := NewServerPage(pm, &fakeMonMgr{}, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	view := p.View()
	tablePart := p.renderTable()
	tabsPart := p.renderStatusLine()

	if strings.Contains(view, tablePart+"\n\n"+tabsPart) {
		t.Fatalf("view contains blank line between table and sub-tabs:\n%s", view)
	}
	if !strings.Contains(view, tablePart+"\n"+tabsPart) {
		t.Fatalf("view missing expected table→sub-tabs transition:\n%s", view)
	}
}

func TestServerPage_LogsSubViewShowsLines(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}

	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	mm.ch <- monitor.MonitorEvent{Source: monitor.SourceLogs, PID: 1, Data: monitor.LogLine{Line: "boot complete"}}
	p, _ = updateAs[*ServerPage](p, monitorEventMsg{ev: <-mm.ch})

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

func TestServerPage_TabCyclesToSlots(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	// Inject a slot snapshot.
	p, _ = updateAs[*ServerPage](p, monitorEventMsg{ev: monitor.MonitorEvent{
		Source: monitor.SourceSlots, PID: 1,
		Data: monitor.SlotSnapshot{Slots: []monitor.Slot{{ID: 0, State: "idle", NCtxMax: 4096}}},
	}})

	// Press Tab -> sub-view becomes Slots.
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})

	v := p.View()
	if !strings.Contains(v, "idle") {
		t.Fatalf("after Tab, slots view missing 'idle':\n%s", v)
	}
}

func TestServerPage_MetricsViewRendersSparkline(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	p, _ = updateAs[*ServerPage](p, monitorEventMsg{ev: monitor.MonitorEvent{
		Source: monitor.SourceMetrics, PID: 1,
		Data: monitor.Metrics{
			TokensPerSec:   []float64{10, 20, 30, 40, 50},
			RequestsPerSec: []float64{0, 0, 1, 2, 3},
			WindowSeconds:  60,
		},
	}})
	// Tab twice -> Metrics.
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})

	v := p.View()
	if !strings.Contains(v, "tokens/s") {
		t.Fatalf("metrics view missing 'tokens/s':\n%s", v)
	}
	bars := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	// Sparkline falls back to ASCII bars under NO_COLOR. Accept either so the
	// test passes in either rendering mode.
	if theme.NoColor() {
		bars = []rune{'_', '.', ':', '-', '=', '+', '*', '#'}
	}
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

func TestServerPage_MetricsPlaceholderWhenEmpty(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 42, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	// Switch to metrics sub-view.
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	out := p.View()
	if !strings.Contains(out, "(no metrics yet") {
		t.Fatalf("metrics view missing placeholder; got:\n%s", out)
	}
}

// TestServerPage_MetricsPersistedOffUpdateLoop guards audit N-P2: metrics disk
// persistence must run in a returned tea.Cmd, not inline in handleMonitorEvent.
func TestServerPage_MetricsPersistedOffUpdateLoop(t *testing.T) {
	dir := t.TempDir()
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 1, Port: 8080, ProfileID: "p1", LogPath: "/tmp/x.log"},
	}}
	p := NewServerPage(pm, &fakeMonMgr{}, nil).WithMetricsDir(dir)

	ev := monitorEventMsg{ev: monitor.MonitorEvent{
		Source: monitor.SourceMetrics, PID: 1, Timestamp: time.Now(),
		Data: monitor.Metrics{TokensPerSec: []float64{42}, RequestsPerSec: []float64{2}},
	}}
	_, cmd := p.Update(ev)

	// The update loop must NOT have written the file synchronously.
	if recs, _ := metricsstore.Read(dir, "p1", time.Time{}); len(recs) != 0 {
		t.Fatalf("metrics persisted inline in update loop; got %d records", len(recs))
	}

	drainCmd(cmd)

	recs, err := metricsstore.Read(dir, "p1", time.Time{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("after draining cmds: %d records, want 1", len(recs))
	}
	if recs[0].TokensPerSec != 42 {
		t.Errorf("tokens/s = %v, want 42", recs[0].TokensPerSec)
	}
}

// TestServerPage_HistoryWithoutInstanceFlashesSelect is a regression for
// TUI_AUDIT F-03: pressing 'h' with no instance selected used to flash a
// misleading message pointing at a non-existent `logging.metrics_dir` config
// key. It must now report that an instance needs selecting.
func TestServerPage_HistoryWithoutInstanceFlashesSelect(t *testing.T) {
	pm := &fakeProcMgr{}
	p := NewServerPage(pm, &fakeMonMgr{}, nil)

	p2, _ := updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})

	if p2.historyChart != nil {
		t.Fatal("history chart must not open without a selected instance")
	}
	msg := p2.flash.Message()
	if !strings.Contains(msg, "select a running instance") {
		t.Errorf("flash = %q, want it to mention selecting a running instance", msg)
	}
	if strings.Contains(msg, "metrics_dir") {
		t.Errorf("flash %q must not reference the non-existent logging.metrics_dir key", msg)
	}
}

func TestServerPage_HistoryChartCapturesInput(t *testing.T) {
	pm := &fakeProcMgr{}
	p := NewServerPage(pm, &fakeMonMgr{}, nil)
	if p.IsCapturingInput() {
		t.Fatal("page should not capture input with no overlay/chart")
	}
	// While the history chart is open the page must claim global keys so the
	// 1/2/3/4 time-window keys reach it instead of switching tabs (ROUTE-01).
	p.historyChart = &components.HistoryChart{}
	if !p.IsCapturingInput() {
		t.Fatal("page should capture input while the history chart is open")
	}
	// '1' must not panic and must keep the chart open (switchHistoryWindow).
	p2, _ := updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	if p2.historyChart == nil {
		t.Fatal("history-window key should not close the chart")
	}
	// esc closes it.
	p2, _ = updateAs[*ServerPage](p2, tea.KeyMsg{Type: tea.KeyEsc})
	if p2.historyChart != nil {
		t.Fatal("esc should close the history chart")
	}
}

func TestServerPage_KillConfirmedDropsRowOptimistically(t *testing.T) {
	// killTrackingMgr.Kill records the pid but keeps it in List(), mimicking a
	// real manager whose liveness reconcile lags. The row must vanish from the
	// table immediately on confirm, not on the next monitor tick (UX-02).
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{insts: []domain.RunningInstance{{PID: 7, Port: 8080, LogPath: "/tmp/x.log"}}}}
	p := NewServerPage(pm, &fakeMonMgr{}, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	if len(p.tbl.Rows()) != 1 {
		t.Fatalf("setup: want 1 row, got %d", len(p.tbl.Rows()))
	}

	p, _ = updateAs[*ServerPage](p, monitorKillConfirmedMsg{pid: 7})

	if len(p.tbl.Rows()) != 0 {
		t.Fatalf("killed row should be dropped immediately; rows=%d", len(p.tbl.Rows()))
	}
	if pm.killed != 7 {
		t.Fatalf("Kill(7) expected; killed=%d", pm.killed)
	}
}

func TestServerPage_KOpensConfirmDoesNotKillImmediately(t *testing.T) {
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{insts: []domain.RunningInstance{{PID: 7, Port: 8080, LogPath: "/tmp/x.log"}}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})

	if pm.killed != 0 {
		t.Fatalf("k should not kill immediately; killed=%d", pm.killed)
	}
	if !p.killConfirm.Active() {
		t.Fatal("expected confirm form after K")
	}
	if !p.IsCapturingInput() {
		t.Fatal("page should capture input while confirm is open")
	}

	// Esc cancels.
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyEsc})
	if p.killConfirm.Active() {
		t.Fatal("esc should clear confirm form")
	}
	if pm.killed != 0 {
		t.Fatalf("esc should not kill; killed=%d", pm.killed)
	}
}

func TestServerPage_FinalizeConfirmAffirmativeCallsKill(t *testing.T) {
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{insts: []domain.RunningInstance{{PID: 7, Port: 8080, LogPath: "/tmp/x.log"}}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	// Open the confirm form via the normal key path.
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	if !p.killConfirm.Active() {
		t.Fatal("expected confirm form after K")
	}
	// Simulate the affirmative completion path. In production huh drives
	// the form to StateCompleted, Confirm.Update emits the onYes msg and
	// clears the form. Here we replicate both halves: clear the confirm
	// (mirroring Confirm.Update's clear) and inject the msg the onYes
	// callback would emit.
	p.killConfirm = components.Confirm{}
	p, cmd := updateAs[*ServerPage](p, monitorKillConfirmedMsg{pid: 7})
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

func TestServerPage_FinalizeConfirmNegativeNoKill(t *testing.T) {
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{insts: []domain.RunningInstance{{PID: 7, Port: 8080, LogPath: "/tmp/x.log"}}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})

	// Esc clears the confirm without invoking onYes.
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyEsc})
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

func TestServerPage_OrphanSubCleanedOnRefresh(t *testing.T) {
	var cancelCalled int32
	mm := &countingMonMgr{
		ch:       make(chan monitor.MonitorEvent, 8),
		onCancel: func() { atomic.AddInt32(&cancelCalled, 1) },
	}
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{insts: []domain.RunningInstance{{PID: 7, Port: 8080, LogPath: "/tmp/x.log"}}}}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	// Initial refresh creates a subscription for pid 7.
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	if _, ok := p.subs[7]; !ok {
		t.Fatal("expected sub for pid 7 after initial refresh")
	}

	// Manager-side the pid is gone (e.g., killed externally or crashed).
	pm.fakeProcMgr.insts = nil
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

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

func TestServerPage_SelectsRowByPID(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 100, Port: 8080, LogPath: "/tmp/a.log"},
		{PID: 200, Port: 8081, LogPath: "/tmp/b.log"},
		{PID: 300, Port: 8082, LogPath: "/tmp/c.log"},
	}}
	mm := &fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	// Public handler queues a refresh; pendingSelectPID is consumed by the
	// refresh handler when it lands. We drain the single refresh cmd here.
	p, cmd := updateAs[*ServerPage](p, ServerSelectPIDMsg{PID: 200})
	if cmd == nil {
		t.Fatalf("ServerSelectPIDMsg should issue refreshInstancesCmd")
	}
	p, _ = updateAs[*ServerPage](p, cmd())

	if got := p.selectedPID(); got != 200 {
		t.Fatalf("selectedPID = %d, want 200", got)
	}
}

func TestServerPage_PendingSelectAppliesAfterFirstRefresh(t *testing.T) {
	// Simulates: SwitchToServerMsg fires before ServerPage has any rows
	// (e.g., very first instance ever launched). pendingSelectPID must
	// wait for the refresh to land, then position the cursor.
	pm := &fakeProcMgr{insts: nil} // initially empty
	mm := &fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)

	// Public msg arrives while rows are still empty.
	p, cmd := updateAs[*ServerPage](p, ServerSelectPIDMsg{PID: 200})
	if cmd == nil {
		t.Fatalf("ServerSelectPIDMsg should issue refreshInstancesCmd")
	}
	// selectedPID is 0 right now (no rows), even though ServerSelectPIDMsg arrived.
	if got := p.selectedPID(); got != 0 {
		t.Fatalf("selectedPID before refresh = %d, want 0", got)
	}
	// Now the registry gets the instance and the refresh cmd lands.
	pm.insts = []domain.RunningInstance{
		{PID: 100, Port: 8080, LogPath: "/tmp/a.log"},
		{PID: 200, Port: 8081, LogPath: "/tmp/b.log"},
	}
	p, _ = updateAs[*ServerPage](p, cmd())
	if got := p.selectedPID(); got != 200 {
		t.Fatalf("selectedPID after refresh = %d, want 200 (pendingSelectPID should have been consumed)", got)
	}
}

func TestServerPage_HandlesWindowSize(t *testing.T) {
	pm := &fakeProcMgr{}
	mm := fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if p.width != 80 || p.height != 24 {
		t.Fatalf("after WindowSizeMsg: width=%d height=%d, want 80x24", p.width, p.height)
	}
}

func TestServerPage_ROpensRestartConfirm(t *testing.T) {
	prof := domain.Profile{ID: "qwen", Name: "Qwen", Model: "/tmp/x.gguf", Args: map[string]any{"port": 8080.0}}
	psk := &fakeProfileStore{p: prof}
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{
		insts: []domain.RunningInstance{{ProfileID: "qwen", PID: 100, Port: 8080, LogPath: "/tmp/a.log", Background: true}},
	}}
	mm := &fakeMonMgr{}
	p := NewServerPage(pm, mm, psk).WithProxy(&fakeProxyForPages{})
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})

	if !p.restartConfirm.Active() {
		t.Fatal("expected restart confirm form after R")
	}
	if !p.IsCapturingInput() {
		t.Fatal("page should capture input while restart confirm is open")
	}

	// Esc cancels.
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyEsc})
	if p.restartConfirm.Active() {
		t.Fatal("esc should clear restart confirm form")
	}
	if pm.killed != 0 {
		t.Fatalf("esc should not kill; killed=%d", pm.killed)
	}
}

func TestServerPage_RestartConfirmAffirmative_UnloadsThenLoads(t *testing.T) {
	prof := domain.Profile{ID: "qwen", Name: "Qwen", Model: "/tmp/x.gguf", Args: map[string]any{"port": 8080.0}}
	psk := &fakeProfileStore{p: prof}
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{
		insts: []domain.RunningInstance{{ProfileID: "qwen", PID: 100, Port: 8080, LogPath: "/tmp/a.log", Background: true}},
	}}
	proxy := &fakeProxyForPages{status: httpproxy.Status{Running: true, LoadedProfileID: "qwen", LoadedPID: 100}}
	mm := &fakeMonMgr{}
	p := NewServerPage(pm, mm, psk).WithProxy(proxy)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if !p.restartConfirm.Active() {
		t.Fatal("expected restart confirm form")
	}

	// Affirmative completion: emit the msg restartConfirm.onYes would build.
	p.restartConfirm = components.Confirm{}
	_, cmd := updateAs[*ServerPage](p, monitorRestartConfirmedMsg{
		pid:     100,
		profile: prof,
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

	// The restart must drive the proxy: unload first, then load — never the
	// process manager directly.
	if len(proxy.ops) != 2 || proxy.ops[0] != "unload" || proxy.ops[1] != "load qwen" {
		t.Errorf("proxy ops = %v, want [unload, load qwen]", proxy.ops)
	}
	if pm.killed != 0 {
		t.Errorf("restart must not call pm.Kill; killed=%d", pm.killed)
	}
}

func TestServerPage_RestartConfirmNegativeNoAction(t *testing.T) {
	prof := domain.Profile{ID: "qwen", Name: "Qwen", Model: "/tmp/x.gguf", Args: map[string]any{"port": 8080.0}}
	psk := &fakeProfileStore{p: prof}
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{
		insts: []domain.RunningInstance{{ProfileID: "qwen", PID: 100, Port: 8080, LogPath: "/tmp/a.log", Background: true}},
	}}
	proxy := &fakeProxyForPages{}
	mm := &fakeMonMgr{}
	p := NewServerPage(pm, mm, psk).WithProxy(proxy)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})

	// Esc clears the confirm without invoking onYes.
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyEsc})
	if p.restartConfirm.Active() {
		t.Error("esc should clear restartConfirm")
	}
	if len(proxy.ops) != 0 {
		t.Errorf("negative finalize should not touch the proxy; ops=%v", proxy.ops)
	}
}

func TestServerPage_RestartFlashWhenProxyMissing(t *testing.T) {
	prof := domain.Profile{ID: "qwen", Name: "Qwen", Model: "/tmp/x.gguf"}
	pm := &fakeProcMgr{
		insts: []domain.RunningInstance{{ProfileID: "qwen", PID: 100, Port: 8080, LogPath: "/tmp/a.log"}},
	}
	p := NewServerPage(pm, &fakeMonMgr{}, &fakeProfileStore{p: prof}) // no proxy wired
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, _ = updateAs[*ServerPage](p, monitorRestartConfirmedMsg{pid: 100, profile: prof})
	if !strings.Contains(p.flash.Message(), "proxy not available") {
		t.Errorf("expected flash about missing proxy; got %q", p.flash.Message())
	}
}

func TestServerPage_RFlashWhenStoreNil(t *testing.T) {
	pm := &fakeProcMgr{
		insts: []domain.RunningInstance{{ProfileID: "qwen", PID: 100, Port: 8080, LogPath: "/tmp/a.log", Background: true}},
	}
	mm := &fakeMonMgr{}
	p := NewServerPage(pm, mm, nil) // store nil
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if p.restartConfirm.Active() {
		t.Fatal("nil store should not open confirm form")
	}
	if !strings.Contains(p.flash.Message(), "store not available") {
		t.Errorf("expected flash about missing store; got %q", p.flash.Message())
	}
}

func TestServerPage_RFlashWhenProfileMissing(t *testing.T) {
	psk := &fakeProfileStore{err: errors.New("profile vanished")}
	pm := &fakeProcMgr{
		insts: []domain.RunningInstance{{ProfileID: "qwen", PID: 100, Port: 8080, LogPath: "/tmp/a.log", Background: true}},
	}
	mm := &fakeMonMgr{}
	p := NewServerPage(pm, mm, psk)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if p.restartConfirm.Active() {
		t.Fatal("missing profile should not open confirm form")
	}
	if !strings.Contains(p.flash.Message(), "not found") {
		t.Errorf("expected flash about missing profile; got %q", p.flash.Message())
	}
}

func TestServerPage_KillLoadedPIDUnloadsViaProxy(t *testing.T) {
	// Killing the instance the proxy currently serves must go through
	// /_admin/unload (force) so the proxy state stays consistent.
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{
		insts: []domain.RunningInstance{{ProfileID: "qwen", PID: 7, Port: 8080, LogPath: "/tmp/x.log"}},
	}}
	proxy := &fakeProxyForPages{status: httpproxy.Status{Running: true, LoadedProfileID: "qwen", LoadedPID: 7}}
	p := NewServerPage(pm, &fakeMonMgr{}, nil).WithProxy(proxy)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, cmd := updateAs[*ServerPage](p, monitorKillConfirmedMsg{pid: 7})
	if len(p.tbl.Rows()) != 0 {
		t.Fatalf("killed row should be dropped immediately; rows=%d", len(p.tbl.Rows()))
	}
	got := drainCmd(cmd)
	var ur *unloadResultMsg
	for _, m := range got {
		if v, ok := m.(unloadResultMsg); ok {
			ur = &v
			break
		}
	}
	if ur == nil {
		t.Fatalf("no unloadResultMsg in cmd batch; got %v", got)
	}
	if ur.err != nil {
		t.Fatalf("unloadResultMsg.err = %v", ur.err)
	}
	if len(proxy.ops) != 1 || proxy.ops[0] != "unload force" {
		t.Errorf("proxy ops = %v, want [unload force]", proxy.ops)
	}
	if pm.killed != 0 {
		t.Errorf("proxy-owned pid must not be pm.Kill'ed; killed=%d", pm.killed)
	}
}

func TestServerPage_KillOrphanPIDUsesProcessManager(t *testing.T) {
	// A pid the proxy does NOT own (orphan from a previous run) still goes
	// through pm.Kill. The decision (Status probe + Kill) runs inside the
	// async cmd, so we drain it before asserting.
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{
		insts: []domain.RunningInstance{{ProfileID: "old", PID: 9, Port: 8081, LogPath: "/tmp/y.log"}},
	}}
	proxy := &fakeProxyForPages{status: httpproxy.Status{Running: true, LoadedProfileID: "qwen", LoadedPID: 7}}
	p := NewServerPage(pm, &fakeMonMgr{}, nil).WithProxy(proxy)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, cmd := updateAs[*ServerPage](p, monitorKillConfirmedMsg{pid: 9})
	msgs := drainCmd(cmd)
	var kr *killResultMsg
	for _, m := range msgs {
		if v, ok := m.(killResultMsg); ok {
			kr = &v
			break
		}
	}
	if kr == nil {
		t.Fatalf("no killResultMsg in cmd batch; got %v", msgs)
	}
	if kr.err != nil {
		t.Fatalf("killResultMsg.err = %v", kr.err)
	}
	if pm.killed != 9 {
		t.Errorf("orphan pid should be pm.Kill'ed; killed=%d", pm.killed)
	}
	if len(proxy.ops) != 0 {
		t.Errorf("orphan kill must not touch the proxy; ops=%v", proxy.ops)
	}
}

// TestServerPage_KillDegradedProxyRefuses mirrors the CLI's
// refuseKillOnDegradedProxy: while the proxy is Running but its /_status
// probe fails (LastError "status_probe_failed…", LoadedPID 0), the loaded
// backend is indistinguishable from an orphan. Killing it directly could
// shoot the proxy's backend behind its back, so the kill must be refused
// with an error flash and no pm.Kill.
func TestServerPage_KillDegradedProxyRefuses(t *testing.T) {
	degraded := httpproxy.Status{Running: true, LastError: "status_probe_failed: context deadline exceeded"}
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{
		insts: []domain.RunningInstance{{ProfileID: "old", PID: 9, Port: 8081, LogPath: "/tmp/y.log"}},
	}}
	proxy := &fakeProxyForPages{}
	p := NewServerPage(pm, &fakeMonMgr{}, nil).WithProxy(proxy)
	// Degraded twice: the guard re-fetches Status once before refusing.
	// (Set after WithProxy — NewProxyPanel consumes one Status() call.)
	proxy.statusSeq = []httpproxy.Status{degraded, degraded}
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, cmd := updateAs[*ServerPage](p, monitorKillConfirmedMsg{pid: 9})
	msgs := drainCmd(cmd)
	var kr *killResultMsg
	for _, m := range msgs {
		if v, ok := m.(killResultMsg); ok {
			kr = &v
			break
		}
	}
	if kr == nil {
		t.Fatalf("no killResultMsg in cmd batch; got %v", msgs)
	}
	if kr.err == nil {
		t.Fatal("degraded proxy: kill must be refused with an error")
	}
	if pm.killed != 0 {
		t.Errorf("degraded proxy: pm.Kill must not be called; killed=%d", pm.killed)
	}
	if len(proxy.ops) != 0 {
		t.Errorf("degraded proxy: no proxy ops expected; ops=%v", proxy.ops)
	}
	p, _ = updateAs[*ServerPage](p, *kr)
	if !p.forceKillConfirm.Active() {
		t.Error("degraded proxy: kill refusal should arm the force-stop confirm (A13)")
	}
}

// TestServerPage_ForceKillConfirmedForceStopsProxyAndKills — audit A13: after
// the operator accepts the force-stop confirm, the proxy is force-stopped and
// the stranded backend is killed directly.
func TestServerPage_ForceKillConfirmedForceStopsProxyAndKills(t *testing.T) {
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{
		insts: []domain.RunningInstance{{ProfileID: "old", PID: 9, Port: 8081, LogPath: "/tmp/y.log"}},
	}}
	proxy := &fakeProxyForPages{}
	p := NewServerPage(pm, &fakeMonMgr{}, nil).WithProxy(proxy)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, cmd := updateAs[*ServerPage](p, monitorForceKillConfirmedMsg{pid: 9})
	msgs := drainCmd(cmd)
	var kr *killResultMsg
	for _, m := range msgs {
		if v, ok := m.(killResultMsg); ok {
			kr = &v
			break
		}
	}
	if kr == nil {
		t.Fatalf("no killResultMsg in cmd batch; got %v", msgs)
	}
	if kr.err != nil {
		t.Fatalf("force kill should succeed; err=%v", kr.err)
	}
	if proxy.forceStops != 1 {
		t.Errorf("expected 1 ForceStop, got %d", proxy.forceStops)
	}
	if pm.killed != 9 {
		t.Errorf("expected pid 9 killed after force-stop, got %d", pm.killed)
	}
}

// TestServerPage_KillDegradedThenHealthyOrphanProceeds: the probe failure
// was transient — the guard's single re-fetch shows a healthy proxy whose
// loaded pid differs from the target, so the orphan kill proceeds.
func TestServerPage_KillDegradedThenHealthyOrphanProceeds(t *testing.T) {
	degraded := httpproxy.Status{Running: true, LastError: "status_probe_failed: context deadline exceeded"}
	healthy := httpproxy.Status{Running: true, LoadedProfileID: "qwen", LoadedPID: 7}
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{
		insts: []domain.RunningInstance{{ProfileID: "old", PID: 9, Port: 8081, LogPath: "/tmp/y.log"}},
	}}
	proxy := &fakeProxyForPages{}
	p := NewServerPage(pm, &fakeMonMgr{}, nil).WithProxy(proxy)
	// Set after WithProxy — NewProxyPanel consumes one Status() call.
	proxy.statusSeq = []httpproxy.Status{degraded, healthy}
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	_, cmd := updateAs[*ServerPage](p, monitorKillConfirmedMsg{pid: 9})
	msgs := drainCmd(cmd)
	var kr *killResultMsg
	for _, m := range msgs {
		if v, ok := m.(killResultMsg); ok {
			kr = &v
			break
		}
	}
	if kr == nil {
		t.Fatalf("no killResultMsg in cmd batch; got %v", msgs)
	}
	if kr.err != nil {
		t.Fatalf("recovered orphan kill should proceed; err = %v", kr.err)
	}
	if pm.killed != 9 {
		t.Errorf("recovered orphan should be pm.Kill'ed; killed=%d", pm.killed)
	}
	if len(proxy.ops) != 0 {
		t.Errorf("orphan kill must not touch the proxy; ops=%v", proxy.ops)
	}
}

// TestServerPage_RestartOrphanKillsThenLoads_NoUnload: restarting an
// instance the proxy does NOT own must pm.Kill the orphan first (free its
// VRAM) and then Load via the proxy — never Unload, which would kill
// whatever OTHER model the proxy currently serves.
func TestServerPage_RestartOrphanKillsThenLoads_NoUnload(t *testing.T) {
	prof := domain.Profile{ID: "old", Name: "Old", Model: "/tmp/x.gguf"}
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{
		insts: []domain.RunningInstance{{ProfileID: "old", PID: 9, Port: 8081, LogPath: "/tmp/y.log"}},
	}}
	// The proxy serves a different model (pid 7) — it must survive.
	proxy := &fakeProxyForPages{status: httpproxy.Status{Running: true, LoadedProfileID: "qwen", LoadedPID: 7}}
	p := NewServerPage(pm, &fakeMonMgr{}, &fakeProfileStore{p: prof}).WithProxy(proxy)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	_, cmd := updateAs[*ServerPage](p, monitorRestartConfirmedMsg{pid: 9, profile: prof})
	msgs := drainCmd(cmd)
	var rr *restartResultMsg
	for _, m := range msgs {
		if v, ok := m.(restartResultMsg); ok {
			rr = &v
			break
		}
	}
	if rr == nil {
		t.Fatalf("no restartResultMsg in cmd batch; got %v", msgs)
	}
	if rr.err != nil {
		t.Fatalf("restartResultMsg.err = %v", rr.err)
	}
	if pm.killed != 9 {
		t.Errorf("orphan restart must pm.Kill the old pid; killed=%d", pm.killed)
	}
	if len(proxy.ops) != 1 || proxy.ops[0] != "load old" {
		t.Errorf("proxy ops = %v, want [load old] (no unload)", proxy.ops)
	}
}

// TestServerPage_RestartOrphanDegradedProxyRefuses: the degraded-proxy
// guard applies to the orphan restart branch too — a pid that looks like
// an orphan only because the status probe failed must not be killed.
func TestServerPage_RestartOrphanDegradedProxyRefuses(t *testing.T) {
	degraded := httpproxy.Status{Running: true, LastError: "status_probe_failed: context deadline exceeded"}
	prof := domain.Profile{ID: "old", Name: "Old", Model: "/tmp/x.gguf"}
	pm := &killTrackingMgr{fakeProcMgr: fakeProcMgr{
		insts: []domain.RunningInstance{{ProfileID: "old", PID: 9, Port: 8081, LogPath: "/tmp/y.log"}},
	}}
	proxy := &fakeProxyForPages{}
	p := NewServerPage(pm, &fakeMonMgr{}, &fakeProfileStore{p: prof}).WithProxy(proxy)
	// Set after WithProxy — NewProxyPanel consumes one Status() call.
	proxy.statusSeq = []httpproxy.Status{degraded, degraded}
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	_, cmd := updateAs[*ServerPage](p, monitorRestartConfirmedMsg{pid: 9, profile: prof})
	msgs := drainCmd(cmd)
	var rr *restartResultMsg
	for _, m := range msgs {
		if v, ok := m.(restartResultMsg); ok {
			rr = &v
			break
		}
	}
	if rr == nil {
		t.Fatalf("no restartResultMsg in cmd batch; got %v", msgs)
	}
	if rr.err == nil {
		t.Fatal("degraded proxy: orphan restart must be refused")
	}
	if pm.killed != 0 {
		t.Errorf("degraded proxy: pm.Kill must not be called; killed=%d", pm.killed)
	}
	if len(proxy.ops) != 0 {
		t.Errorf("degraded proxy: no proxy ops expected; ops=%v", proxy.ops)
	}
}

func TestServerPage_RefreshReadsRegistryFromDisk(t *testing.T) {
	// The TUI process never launches instances — the detached proxy process
	// does. refreshInstancesCmd must re-read instances.json (read-only, via
	// RefreshFromDisk) so proxy-launched instances appear without the TUI
	// ever writing the registry back.
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	p := NewServerPage(pm, &fakeMonMgr{}, nil)

	msgs := drainCmd(p.refreshInstancesCmd())
	if len(msgs) != 1 {
		t.Fatalf("refreshInstancesCmd produced %d msgs, want 1", len(msgs))
	}
	if _, ok := msgs[0].(monitorInstancesRefreshedMsg); !ok {
		t.Fatalf("refresh produced %T, want monitorInstancesRefreshedMsg", msgs[0])
	}
	if pm.refreshCalls != 1 {
		t.Errorf("RefreshFromDisk called %d times, want 1", pm.refreshCalls)
	}
}

func TestServerPage_RestartResultMsgSetsFlashOnError(t *testing.T) {
	pm := &fakeProcMgr{}
	mm := fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)

	p, _ = updateAs[*ServerPage](p, restartResultMsg{pid: 42, err: errors.New("kill refused")})
	if !strings.Contains(p.flash.Message(), "kill refused") {
		t.Errorf("expected flash to contain error; got %q", p.flash.Message())
	}
}

// TestServerPage_RestartLoadFailureFlashesError covers the half-failed
// restart: Unload succeeds but the subsequent Load fails. The page must
// surface an error flash and the proxy must have seen exactly
// [unload, load] — proving the swap was attempted in order and not retried.
func TestServerPage_RestartLoadFailureFlashesError(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 7, Port: 8080, ProfileID: "demo", LogPath: "/tmp/x.log"},
	}}
	proxy := &fakeProxyForPages{
		status:  httpproxy.Status{Running: true, LoadedProfileID: "demo", LoadedPID: 7},
		loadErr: errors.New("model gone"),
	}
	p := NewServerPage(pm, &fakeMonMgr{}, nil).WithProxy(proxy)
	p.SetSize(120, 30)

	_, cmd := p.Update(monitorRestartConfirmedMsg{pid: 7, profile: domain.Profile{ID: "demo"}})
	msgs := drainCmd(cmd)
	var res *restartResultMsg
	for _, m := range msgs {
		if v, ok := m.(restartResultMsg); ok {
			res = &v
			break
		}
	}
	if res == nil {
		t.Fatalf("no restartResultMsg from restart cmd; got %v", msgs)
	}
	if res.err == nil || !strings.Contains(res.err.Error(), "load") {
		t.Fatalf("restart err = %v, want a load failure", res.err)
	}
	if got := strings.Join(proxy.ops, ", "); got != "unload, load demo" {
		t.Errorf("proxy ops = %q, want \"unload, load demo\"", got)
	}

	p, _ = updateAs[*ServerPage](p, *res)
	if !strings.Contains(p.flash.Message(), "model gone") {
		t.Errorf("flash = %q, want the load error surfaced", p.flash.Message())
	}
}

func TestServerPage_FlashAutoClear(t *testing.T) {
	pm := &fakeProcMgr{}
	mm := fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)

	p, _ = updateAs[*ServerPage](p, restartResultMsg{pid: 42, err: errors.New("boom")})
	if p.flash.Message() == "" {
		t.Fatal("flash should be set after error")
	}

	items := p.flash.Items()
	p, _ = updateAs[*ServerPage](p, components.FlashClearMsg{Tag: "monitor", Seq: items[len(items)-1].Seq})
	if p.flash.Message() != "" {
		t.Errorf("flash should be cleared after FlashClearMsg; got %q", p.flash.Message())
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

func TestServerPage_CrashedRowShowsMarker(t *testing.T) {
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
	p := NewServerPage(pm, mm, nil)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	out := p.View()
	if !strings.Contains(out, "✗") && !strings.Contains(out, "crashed") {
		t.Fatalf("crashed row missing badge; got:\n%s", out)
	}
}

func TestServerPage_StoppedRowLabel(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{
		PID:        777,
		Port:       8080,
		ProfileID:  "qwen",
		LogPath:    "/tmp/x.log",
		Crashed:    true,
		ExitReason: domain.ExitReasonOperatorStop,
	}}}
	p := NewServerPage(pm, fakeMonMgr{}, nil)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	profileCell := p.tbl.Rows()[0][3]
	if !strings.Contains(profileCell, "(stopped)") {
		t.Errorf("profile cell = %q, want stopped label", profileCell)
	}
	if strings.Contains(profileCell, "(crashed)") {
		t.Errorf("profile cell = %q, must not be labelled crashed", profileCell)
	}
}

func TestServerPage_PeriodicRefreshTickEmitsRefreshCmd(t *testing.T) {
	pm := &fakeProcMgr{}
	mm := &fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	cmd := p.Init()
	if cmd == nil {
		t.Fatal("Init returned nil")
	}
	if !p.periodicTickActive {
		t.Errorf("periodicTickActive = false; want true")
	}
}

func TestServerPage_CancelOrphanIsAsync(t *testing.T) {
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
	p := NewServerPage(pm, mm, nil)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	pm.insts = nil // PID 1 vanished -> cancel should be triggered

	done := make(chan struct{})
	go func() {
		p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
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

func TestServerPage_RowShowsUptime(t *testing.T) {
	started := time.Now().Add(-3*time.Minute - 12*time.Second)
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 9, Port: 8080, StartedAt: started, LogPath: "/tmp/z.log"}}}
	mm := fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	rows := p.tbl.Rows()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0][4] != "3m12s" {
		t.Errorf("uptime cell = %q, want %q", rows[0][4], "3m12s")
	}
}

func TestServerPage_RowShowsVRAMAndTokens(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 7, Port: 8080, StartedAt: time.Now(), LogPath: "/tmp/x.log"}}}
	mm := fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	// Inject a synthetic subState so the next refresh picks up the metrics.
	st := p.subs[7]
	if st == nil {
		t.Fatalf("expected subState for pid 7 after refresh")
	}
	st.gpu = monitor.GPUStats{VRAMUsedMB: 1024, VRAMTotalMB: 24576}
	st.mets = monitor.Metrics{TokensPerSec: []float64{10.5, 12.3}}

	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	rows := p.tbl.Rows()
	if rows[0][5] != "1024/24576MB" {
		t.Errorf("vram cell = %q, want %q", rows[0][5], "1024/24576MB")
	}
	if rows[0][6] != "12.3" {
		t.Errorf("tokens cell = %q, want %q", rows[0][6], "12.3")
	}
}

func TestServerPage_RowFallbackDashesWhenNoState(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, StartedAt: time.Time{}, LogPath: "/tmp/x.log"}}}
	mm := fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	rows := p.tbl.Rows()
	if rows[0][4] != "--" || rows[0][5] != "--" || rows[0][6] != "--" {
		t.Errorf("fallback cells = %q/%q/%q, want all '--'", rows[0][4], rows[0][5], rows[0][6])
	}
}

// TestServerPage_CrashedRowPlainCells asserts that crashed instance cells
// are NEVER wrapped in ANSI styles. bubbles/table.renderRow truncates each
// cell with runewidth.Truncate, which is not ANSI-aware — wrapping a cell
// in theme.Error.Render produced sliced escape sequences that the terminal
// rendered as replacement glyphs (U+FFFD) and left SGR state open, causing
// color bleed and residual-text artifacts on Kitty/Ghostty. Crashed status
// is conveyed via plain-ASCII markers: "✗ " prefix on PID, " (crashed)"
// suffix on profile.
func TestServerPage_CrashedRowPlainCells(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 13, Port: 8080, ProfileID: "p1", Crashed: true, LogPath: "/tmp/x.log"}}}
	mm := fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	rows := p.tbl.Rows()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0][1] != "✗ 13" {
		t.Errorf("crashed pid cell = %q, want %q (plain, no ANSI)", rows[0][1], "✗ 13")
	}
	if !strings.Contains(rows[0][3], "crashed") {
		t.Errorf("profile cell should mention crashed; got %q", rows[0][3])
	}
	// Regression: no cell may contain an ANSI escape (\x1b). Otherwise
	// bubbles/table.renderRow truncates mid-sequence and corrupts output.
	for i, cell := range rows[0] {
		if strings.ContainsRune(cell, '\x1b') {
			t.Errorf("cell[%d] contains ANSI escape: %q", i, cell)
		}
	}
}

// TestServerPage_CrashedView_NoBrokenANSI is a render-pipeline regression
// for the reported crash where Server tab cells appeared as U+FFFD glyphs
// after a proxy swap. The original cause was per-cell theme.Error.Render
// being truncated by bubbles/table → broken SGR sequences. This test
// drives a real View() with a crashed instance under a narrow column
// width and asserts the output contains no replacement chars and no
// unterminated CSI sequences.
func TestServerPage_CrashedView_NoBrokenANSI(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{
		PID:       9999999,
		Port:      4331,
		ProfileID: "qwen3-vl-32b-16k",
		LogPath:   "/tmp/x.log",
		Crashed:   true,
		StartedAt: time.Now().Add(-7 * time.Second),
	}}}
	mm := &fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(95, 30) // matches the screenshot terminal width
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	out := p.View()

	if strings.ContainsRune(out, '�') {
		t.Fatalf("View output contains U+FFFD replacement char (broken UTF-8 / sliced ANSI):\n%q", out)
	}
	// Every \x1b[ (CSI introducer) must be followed by a valid SGR terminator
	// ('m') before another \x1b or EOF — otherwise the sequence is sliced.
	for i := 0; i < len(out); i++ {
		if out[i] != '\x1b' {
			continue
		}
		// CSI form: ESC '[' params 'm'. Find the next final byte.
		if i+1 >= len(out) || out[i+1] != '[' {
			t.Fatalf("ESC at byte %d not followed by '[': %q", i, out[i:min(i+8, len(out))])
		}
		// Walk params until a final byte (range 0x40..0x7E) or another ESC.
		j := i + 2
		for j < len(out) {
			c := out[j]
			if c == '\x1b' {
				t.Fatalf("CSI started at byte %d truncated at next ESC (byte %d): %q",
					i, j, out[i:j])
			}
			if c >= 0x40 && c <= 0x7E {
				break
			}
			j++
		}
		if j >= len(out) {
			t.Fatalf("CSI started at byte %d never terminated", i)
		}
		i = j
	}
}

func TestServerPage_HealthyRowUnstyled(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 13, Port: 8080, ProfileID: "p1", LogPath: "/tmp/x.log"}}}
	mm := fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	rows := p.tbl.Rows()
	if rows[0][1] != "13" {
		t.Errorf("healthy pid cell should be plain %q; got %q", "13", rows[0][1])
	}
}

func TestServerPage_SelectedPIDParsesAcrossANSI(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 13, Port: 8080, ProfileID: "p1", Crashed: true, LogPath: "/tmp/x.log"}}}
	mm := fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	if got := p.selectedPID(); got != 13 {
		t.Errorf("selectedPID = %d, want 13 (despite ANSI)", got)
	}
}

func TestServerPage_EmptyStateHint(t *testing.T) {
	pm := &fakeProcMgr{insts: nil}
	mm := fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	out := p.View()
	if !strings.Contains(out, "No instances running") {
		t.Errorf("empty Monitor view missing hint; got:\n%s", out)
	}
	if !strings.Contains(out, "Profiles [1]") {
		t.Errorf("empty Monitor view should reference Profiles tab as [1]; got:\n%s", out)
	}
}

func TestServerPage_EmptyStateKeepsSubViews(t *testing.T) {
	pm := &fakeProcMgr{
		insts:   nil,
		history: []domain.ExitedInstance{{ProfileID: "stopped-profile", PID: 7}},
	}
	p := NewServerPage(pm, fakeMonMgr{}, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	out := p.View()
	for _, want := range []string{"No instances running", "Logs", "Slots", "Metrics", "History"} {
		if !strings.Contains(out, want) {
			t.Errorf("empty server view missing %q:\n%s", want, out)
		}
	}
	p.subView = SubViewHistory
	if out := p.View(); !strings.Contains(out, "stopped-profile") {
		t.Errorf("history must remain reachable without running instances:\n%s", out)
	}
}

func TestServerPage_SubViewTabsHighlightActive(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	wantTabs := components.ActiveLabel("Logs", true) + theme.Subtitle.Render(" │ ") + components.ActiveLabel("Slots", false) + theme.Subtitle.Render(" │ ") + components.ActiveLabel("Metrics", false) + theme.Subtitle.Render(" │ ") + components.ActiveLabel("History", false)
	if got := renderSubViewTabs(SubViewLogs); got != wantTabs {
		t.Errorf("renderSubViewTabs(Logs) shape mismatch; got %q", got)
	}

	out := p.View()
	if !strings.Contains(out, components.ActiveLabel("Logs", true)) {
		t.Errorf("view missing active Logs tab; got:\n%s", out)
	}

	// Cycle.
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	out = p.View()
	if !strings.Contains(out, components.ActiveLabel("Slots", true)) {
		t.Errorf("view missing active Slots tab after [v]; got:\n%s", out)
	}
}

func TestServerPage_SubViewTabsHaveColorlessActiveMarker(t *testing.T) {
	t.Cleanup(theme.RebuildStyles)
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()

	out := renderSubViewTabs(SubViewSlots)
	if !strings.Contains(out, "[Slots]") || strings.Count(out, "[") != 1 {
		t.Fatalf("Slots is not the sole active marker under NO_COLOR: %q", out)
	}

	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	if out := p.View(); !strings.Contains(out, "[Logs]") {
		t.Fatalf("Logs not bracketed under NO_COLOR:\n%s", out)
	}
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	pageOut := p.View()
	if !strings.Contains(pageOut, "[Slots]") || strings.Contains(pageOut, "[Logs]") {
		t.Fatalf("[v] did not move the NO_COLOR brackets from Logs to Slots:\n%s", pageOut)
	}
}

func TestServerPage_LogsShowingNofMFooter(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	// 5 lines — footer absent.
	st := p.subs[1]
	st.logs = []string{"a", "b", "c", "d", "e"}
	if strings.Contains(p.View(), "showing last 10") {
		t.Errorf("5-log view should not show count footer; got:\n%s", p.View())
	}

	// More than visible log lines — footer present.
	visible := 30 - (tableHeightForRows(30, 1) + 8)
	st.logs = make([]string, visible+5)
	for i := range st.logs {
		st.logs[i] = fmt.Sprintf("line %d", i)
	}
	if !strings.Contains(p.View(), fmt.Sprintf("showing last %d of %d", visible, visible+5)) {
		t.Errorf("overflow log view should show count footer (visible=%d); got:\n%s", visible, p.View())
	}
}

func TestServerPage_PausedIndicator(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 8)}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	if strings.Contains(p.View(), "PAUSED") {
		t.Fatalf("idle view should not contain PAUSED; got:\n%s", p.View())
	}

	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeySpace})
	if !strings.Contains(p.View(), "PAUSED") {
		t.Errorf("paused view missing PAUSED; got:\n%s", p.View())
	}

	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeySpace})
	if strings.Contains(p.View(), "PAUSED") {
		t.Errorf("resumed view still contains PAUSED; got:\n%s", p.View())
	}
}

func TestServerPage_HintsListPageKeys(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	hints := p.Hints()
	for _, want := range []string{"[v]", "[K]", "[R]", "[h]", "[Space]"} {
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

// TestServerPage_ApplyInstances_RendersRows feeds N instances and asserts the
// table has N rows whose key columns reflect the input.
func TestServerPage_ApplyInstances_RendersRows(t *testing.T) {
	pm := &fakeProcMgr{}
	mm := fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
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
		if !strings.Contains(rows[i][1], fmt.Sprintf("%d", want.PID)) {
			t.Fatalf("row %d pid col %q missing pid %d", i, rows[i][1], want.PID)
		}
		if !strings.Contains(rows[i][2], fmt.Sprintf("%d", want.Port)) {
			t.Fatalf("row %d port col %q missing port %d", i, rows[i][2], want.Port)
		}
		if !strings.Contains(rows[i][3], want.ProfileID) {
			t.Fatalf("row %d profile col %q missing %q", i, rows[i][3], want.ProfileID)
		}
	}
}

// TestServerPage_ApplyInstances_EnsuresSubscription verifies that a fresh
// non-crashed instance gets registered in p.subs and p.chans.
func TestServerPage_ApplyInstances_EnsuresSubscription(t *testing.T) {
	pm := &fakeProcMgr{}
	mm := &chanMonMgr{ch: make(chan monitor.MonitorEvent, 1)}
	p := NewServerPage(pm, mm, nil)
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

// TestServerPage_ApplyInstances_ReapsCrashed pre-populates a subscription
// then feeds the same PID with Crashed=true. The collapsed reap loop must
// drop both the subs and chans entry for that PID.
func TestServerPage_ApplyInstances_ReapsCrashed(t *testing.T) {
	var cancelCalled int32
	pm := &fakeProcMgr{}
	mm := &countingMonMgr{
		ch:       make(chan monitor.MonitorEvent, 1),
		onCancel: func() { atomic.AddInt32(&cancelCalled, 1) },
	}
	p := NewServerPage(pm, mm, nil)
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

// TestServerPage_ApplyInstances_ReapsOrphan pre-populates two PIDs (A, B)
// then feeds the input list with only A. B's subscription must be dropped.
func TestServerPage_ApplyInstances_ReapsOrphan(t *testing.T) {
	var cancelCalled int32
	pm := &fakeProcMgr{}
	mm := &countingMonMgr{
		ch:       make(chan monitor.MonitorEvent, 4),
		onCancel: func() { atomic.AddInt32(&cancelCalled, 1) },
	}
	p := NewServerPage(pm, mm, nil)
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

// TestServerPage_ApplyInstances_NoLeakOnAllCrashed launches N instances,
// then crashes all of them in a single applyInstances call. Both maps must
// be empty afterwards — proving the collapsed reap loop covers the
// "everything died at once" case.
func TestServerPage_ApplyInstances_NoLeakOnAllCrashed(t *testing.T) {
	var cancelCalled int32
	pm := &fakeProcMgr{}
	mm := &countingMonMgr{
		ch:       make(chan monitor.MonitorEvent, 8),
		onCancel: func() { atomic.AddInt32(&cancelCalled, 1) },
	}
	p := NewServerPage(pm, mm, nil)
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

func TestServerPage_Reload(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 4242, Port: 9090, ProfileID: "p1", LogPath: "/tmp/x.log"},
	}}
	p := NewServerPage(pm, fakeMonMgr{}, nil)

	cmd := p.Reload()
	if cmd == nil {
		t.Fatal("Reload returned nil cmd")
	}
	msg := cmd()
	refreshed, ok := msg.(monitorInstancesRefreshedMsg)
	if !ok {
		t.Fatalf("Reload cmd produced %T, want monitorInstancesRefreshedMsg", msg)
	}
	if len(refreshed.insts) != 1 || refreshed.insts[0].PID != 4242 {
		t.Fatalf("refreshed.insts = %+v, want one instance pid=4242", refreshed.insts)
	}
}

func TestServerPage_HistorySubViewRendersRows(t *testing.T) {
	pm := &fakeProcMgr{
		insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}},
		history: []domain.ExitedInstance{
			{ProfileID: "qwen", PID: 99, Port: 8080, StartedAt: time.Now().UTC().Add(-time.Hour), ExitedAt: time.Now().UTC(), DurationSeconds: 3600, ExitReason: "exit:0", StderrTail: []string{"err"}},
		},
	}
	mm := &fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	// Cycle to History (3 presses: Logs→Slots→Metrics→History).
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})

	v := p.View()
	if !strings.Contains(v, "qwen") {
		t.Fatalf("history view missing profile 'qwen':\n%s", v)
	}
	if !strings.Contains(v, "99") {
		t.Fatalf("history view missing PID 99:\n%s", v)
	}
	if !strings.Contains(v, "exit:0") {
		t.Fatalf("history view missing exit reason 'exit:0':\n%s", v)
	}
	if !strings.Contains(v, "1 lines") {
		t.Fatalf("history view missing stderr-tail count '1 lines':\n%s", v)
	}
}

func TestServerPage_HistorySubViewCyclesViaV(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}}}
	mm := &fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	// Initial: Logs.
	if p.subView != SubViewLogs {
		t.Fatalf("initial subView = %d, want SubViewLogs", p.subView)
	}
	// 1st v -> Slots.
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if p.subView != SubViewSlots {
		t.Fatalf("after 1st v subView = %d, want SubViewSlots", p.subView)
	}
	// 2nd v -> Metrics.
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if p.subView != SubViewMetrics {
		t.Fatalf("after 2nd v subView = %d, want SubViewMetrics", p.subView)
	}
	// 3rd v -> History.
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if p.subView != SubViewHistory {
		t.Fatalf("after 3rd v subView = %d, want SubViewHistory", p.subView)
	}
	// 4th v -> back to Logs.
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if p.subView != SubViewLogs {
		t.Fatalf("after 4th v subView = %d, want SubViewLogs", p.subView)
	}
}

func TestServerPage_HistoryRefreshedOnInstanceRefresh(t *testing.T) {
	pm := &fakeProcMgr{
		insts:   []domain.RunningInstance{{PID: 1, Port: 8080, LogPath: "/tmp/x.log"}},
		history: []domain.ExitedInstance{{ProfileID: "a", PID: 10}},
	}
	mm := &fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	if len(p.history) != 1 || p.history[0].PID != 10 {
		t.Fatalf("history not refreshed; got %+v", p.history)
	}
	pm.history = append(pm.history, domain.ExitedInstance{ProfileID: "b", PID: 20})
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	if len(p.history) != 2 || p.history[1].PID != 20 {
		t.Fatalf("history not updated on second refresh; got %+v", p.history)
	}
}

// TUI_AUDIT F-03: pressing 'h' with an instance selected but no metrics
// directory wired must report the directory is unavailable — NOT point at
// `logging.metrics_dir`, a config key that does not exist (the metrics dir is
// always derived from state_dir; see cmd/model-loader/main.go).
func TestServerPage_HMetricsUnavailableMessage(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 1234, Port: 8080, ProfileID: "p1"},
	}}
	p := NewServerPage(pm, &fakeMonMgr{}, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})

	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})

	msg := p.flash.Message()
	if !strings.Contains(msg, "metrics directory unavailable") {
		t.Errorf("flash=%q; want it to report the metrics directory is unavailable", msg)
	}
	if strings.Contains(msg, "metrics_dir") {
		t.Errorf("flash=%q must not reference the non-existent logging.metrics_dir config key", msg)
	}
}

// UIUX-010: when the monitor subscription for an instance failed, the VRAM
// and Tokens/s cells must read "ERR" (plain ASCII — see the ANSI-truncation
// pitfall on renderRows) instead of the "no data yet" placeholder "--".
func TestServerPage_RowShowsERRWhenSubscriptionFailed(t *testing.T) {
	pm := &fakeProcMgr{}
	mm := fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)
	p.subs[99] = &subState{subErr: "nvidia-smi not found"}

	rows := p.renderRows([]domain.RunningInstance{
		{PID: 99, Port: 7000, ProfileID: "alpha", LogPath: "/tmp/a.log"},
	})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0][5] != "ERR" || rows[0][6] != "ERR" {
		t.Errorf("VRAM/Tokens cells = %q/%q, want ERR/ERR", rows[0][5], rows[0][6])
	}
}

// UIUX-024: pausing refresh must be visible from the monitor table header,
// not only inside the logs sub-view.
func TestServerPage_RenderTableShowsPausedMarker(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 7, Port: 7000, ProfileID: "alpha", LogPath: "/tmp/a.log"},
	}}
	mm := fakeMonMgr{}
	p := NewServerPage(pm, mm, nil)
	p.SetSize(120, 30)

	if out := p.renderTable(); strings.Contains(out, "[PAUSED") {
		t.Errorf("unpaused table header must not show PAUSED marker; got:\n%s", out)
	}
	p.paused = true
	if out := p.renderTable(); !strings.Contains(out, "[PAUSED - Space to resume]") {
		t.Errorf("paused table header missing PAUSED marker; got:\n%s", out)
	}
}

// UIUX-021: a newly-crashed instance raises exactly one TabAttentionMsg; the
// 2s refresh re-reporting the same crash stays silent, and crashSeen entries
// are reaped once the PID leaves the instance list.
func TestServerPage_CrashEmitsTabAttentionOnce(t *testing.T) {
	pm := &fakeProcMgr{}
	p := NewServerPage(pm, fakeMonMgr{}, nil)
	insts := []domain.RunningInstance{{PID: 42, Port: 8080, ProfileID: "p1", Crashed: true}}

	countAttention := func(msgs []tea.Msg) int {
		n := 0
		for _, m := range msgs {
			if a, ok := m.(TabAttentionMsg); ok {
				if a.Page != AttentionServer {
					t.Fatalf("attention page = %q, want %q", a.Page, AttentionServer)
				}
				n++
			}
		}
		return n
	}

	if got := countAttention(drainCmd(p.applyInstances(insts))); got != 1 {
		t.Fatalf("first apply: attention msgs = %d, want 1", got)
	}
	if got := countAttention(drainCmd(p.applyInstances(insts))); got != 0 {
		t.Fatalf("second apply (same crash): attention msgs = %d, want 0", got)
	}

	// PID gone: crashSeen entry must be reaped so the map cannot grow forever.
	_ = p.applyInstances(nil)
	if len(p.crashSeen) != 0 {
		t.Fatalf("crashSeen = %v after PID left the list, want empty", p.crashSeen)
	}
}

// TUI-RESP: centeredDivider guard and SetSize column budget. The table's
// internal viewport height excludes the bubbles header row, so we assert the
// budget is honored by adding 1 for the header line. The formula values
// themselves stay in sync with serverTableHeight.
func TestServerPage_ResponsiveLayout(t *testing.T) {
	if centeredDivider("PAUSED", -4) != "" {
		t.Fatal("centeredDivider negative width should return empty")
	}
	p := NewServerPage(&fakeProcMgr{}, fakeMonMgr{}, nil)
	p.SetSize(40, 12)
	// bubbles table.SetHeight subtracts 1 row for the column header.
	if got, want := p.tbl.Height()+1, serverTableHeight(12); got != want {
		t.Fatalf("table budget = %d want %d (serverTableHeight)", got, want)
	}
	p.SetSize(120, 30)
	p.SetSize(80, 24)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: []domain.RunningInstance{{PID: 1, Port: 8080}}})
	if got := p.tbl.Height() + 1; got != 2 {
		t.Errorf("one-row table height = %d, want 2", got)
	}
	insts := make([]domain.RunningInstance, 9)
	for i := range insts {
		insts[i] = domain.RunningInstance{PID: i + 1, Port: 8080 + i}
	}
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: insts})
	if got, want := p.tbl.Height()+1, serverTableHeight(24); got != want {
		t.Errorf("nine-row table height = %d, want %d", got, want)
	}
	cols := p.tbl.Columns()
	if len(cols) < 7 {
		t.Fatalf("wide columns missing: %+v", cols)
	}
	if cols[0].Width != markerColumnWidth {
		t.Errorf("marker column width = %d, want %d", cols[0].Width, markerColumnWidth)
	}
	if cols[3].Width != colProfile || cols[5].Width != colVRAM {
		t.Fatalf("wide columns = %+v (want Profile=%d, VRAM=%d)", cols, colProfile, colVRAM)
	}
}

// UIUX-047: the kill confirmation must name the profile and endpoint, not just
// the pid, and must stay inside the modal frame at narrow widths.
func TestServerPage_KillConfirmNamesTarget(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 4242, Port: 42105, ProfileID: "agents-a1-4b-q8", LogPath: "/tmp/x.log"},
	}}
	p := NewServerPage(pm, fakeMonMgr{}, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})

	if !p.killConfirm.Active() {
		t.Fatal("expected kill confirm after K")
	}
	content := p.OverlayView().Content
	for _, want := range []string{"agents-a1-4b-q8", "4242", "42105"} {
		if !strings.Contains(content, want) {
			t.Errorf("kill confirm overlay missing %q:\n%s", want, content)
		}
	}
}

func TestServerPage_KillConfirmClipsLongProfileID(t *testing.T) {
	long := strings.Repeat("q", 60)
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 4242, Port: 42105, ProfileID: long, LogPath: "/tmp/x.log"},
	}}
	p := NewServerPage(pm, fakeMonMgr{}, nil)
	p.SetSize(80, 24)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})

	content := p.OverlayView().Content
	if !strings.Contains(content, "…") {
		t.Errorf("long profile id should be clipped with an ellipsis:\n%s", content)
	}
	lines := strings.Split(content, "\n")
	want := lipgloss.Width(lines[0])
	for i, line := range lines {
		if got := lipgloss.Width(line); got != want {
			t.Fatalf("overlay line %d width = %d, want %d (border broken)", i, got, want)
		}
	}
}

func TestServerPage_XWithLoadedBackendAsksConfirm(t *testing.T) {
	proxy := &fakeProxyForPages{status: httpproxy.Status{
		Running: true, LoadedProfileID: "qwen", LoadedPID: 4242, LoadedPort: 42105,
	}}
	p := NewServerPage(&fakeProcMgr{}, fakeMonMgr{}, nil).WithProxy(proxy)
	p.SetSize(120, 30)
	p, cmd := updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd == nil {
		t.Fatal("x should emit a proxy stop request")
	}
	p, _ = updateAs[*ServerPage](p, cmd())
	if !p.stopProxyConfirm.Active() {
		t.Fatal("loaded backend should require stop confirmation")
	}
	content := p.OverlayView().Content
	for _, want := range []string{"qwen", "4242", "42105"} {
		if !strings.Contains(content, want) {
			t.Errorf("stop proxy confirmation missing %q:\n%s", want, content)
		}
	}
	if proxy.stops != 0 {
		t.Fatalf("Stop called %d times before confirmation", proxy.stops)
	}
}

func TestServerPage_XWithNoBackendStopsImmediately(t *testing.T) {
	proxy := &fakeProxyForPages{status: httpproxy.Status{Running: true}}
	p := NewServerPage(&fakeProcMgr{}, fakeMonMgr{}, nil).WithProxy(proxy)
	p, cmd := updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if p.stopProxyConfirm.Active() {
		t.Fatal("proxy without a loaded backend must not open a confirmation")
	}
	if cmd == nil {
		t.Fatal("x should issue an immediate stop command")
	}
	msg, ok := cmd().(components.ProxyActionResultMsg)
	if !ok {
		t.Fatalf("stop command produced %T, want ProxyActionResultMsg", msg)
	}
	if msgAction := fmt.Sprintf("%v", msg); !strings.Contains(msgAction, "stop") {
		t.Errorf("stop command result = %v, want stop action", msg)
	}
	if proxy.stops != 1 {
		t.Fatalf("Stop called %d times, want 1", proxy.stops)
	}
}

func TestServerPage_StopProxyConfirmedStops(t *testing.T) {
	proxy := &fakeProxyForPages{status: httpproxy.Status{Running: true}}
	p := NewServerPage(&fakeProcMgr{}, fakeMonMgr{}, nil).WithProxy(proxy)
	_, cmd := updateAs[*ServerPage](p, serverProxyStopConfirmedMsg{})
	if cmd == nil {
		t.Fatal("confirmed proxy stop should return a command")
	}
	_ = cmd()
	if proxy.stops != 1 {
		t.Fatalf("Stop called %d times, want 1", proxy.stops)
	}
}

func TestServerPage_CursorMarkerTracksSelection(t *testing.T) {
	pm := &fakeProcMgr{insts: []domain.RunningInstance{
		{PID: 1, Port: 8001}, {PID: 2, Port: 8002}, {PID: 3, Port: 8003},
	}}
	p := NewServerPage(pm, fakeMonMgr{}, nil)
	p.SetSize(120, 30)
	p, _ = updateAs[*ServerPage](p, monitorInstancesRefreshedMsg{insts: pm.List()})
	if got := p.tbl.Rows()[0][0]; got != "> " {
		t.Fatalf("first row marker = %q, want %q", got, "> ")
	}
	if got := p.tbl.Rows()[1][0]; got != "  " {
		t.Fatalf("second row marker = %q, want two spaces", got)
	}
	p, _ = updateAs[*ServerPage](p, tea.KeyMsg{Type: tea.KeyDown})
	if got := p.tbl.Rows()[0][0]; got != "  " {
		t.Errorf("first row marker after down = %q, want two spaces", got)
	}
	if got := p.tbl.Rows()[1][0]; got != "> " {
		t.Errorf("second row marker after down = %q, want %q", got, "> ")
	}
}

func TestServerPage_MarkerColumnKeepsTableWidth(t *testing.T) {
	p := NewServerPage(&fakeProcMgr{}, fakeMonMgr{}, nil)
	p.SetSize(80, 24)
	width := 0
	for _, col := range p.tbl.Columns() {
		width += col.Width + tableCellPadding
	}
	if width > 80 {
		t.Errorf("rendered column budget = %d, exceeds terminal width 80", width)
	}
}

// TestServerPage_StopProxyConfirmedMarksOperatorStop — UIUX-030(b): confirmed
// proxy stop records the loaded backend pid as operator-initiated before the
// supervisor tears the tree down.
func TestServerPage_StopProxyConfirmedMarksOperatorStop(t *testing.T) {
	proxy := &fakeProxyForPages{status: httpproxy.Status{Running: true}}
	pm := &fakeProcMgr{}
	p := NewServerPage(pm, fakeMonMgr{}, nil).WithProxy(proxy)
	_, cmd := updateAs[*ServerPage](p, serverProxyStopConfirmedMsg{pid: 4242})
	if cmd == nil {
		t.Fatal("confirmed proxy stop should return a command")
	}
	if len(pm.markedStops) != 1 || pm.markedStops[0] != 4242 {
		t.Fatalf("markedStops = %v, want [4242]", pm.markedStops)
	}
	_ = cmd()
	if proxy.stops != 1 {
		t.Fatalf("Stop called %d times, want 1", proxy.stops)
	}
}

// TestServerPage_StopProxyConfirmedZeroPIDSkipsMark — pid 0 (no loaded backend /
// zero value from older tests) must not call MarkOperatorStop.
func TestServerPage_StopProxyConfirmedZeroPIDSkipsMark(t *testing.T) {
	proxy := &fakeProxyForPages{status: httpproxy.Status{Running: true}}
	pm := &fakeProcMgr{}
	p := NewServerPage(pm, fakeMonMgr{}, nil).WithProxy(proxy)
	_, cmd := updateAs[*ServerPage](p, serverProxyStopConfirmedMsg{})
	if cmd == nil {
		t.Fatal("confirmed proxy stop should return a command")
	}
	if len(pm.markedStops) != 0 {
		t.Fatalf("markedStops = %v, want empty for pid 0", pm.markedStops)
	}
}
