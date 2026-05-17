package pages

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// SubViewKind selects which bottom region the MonitorPage renders.
type SubViewKind int

const (
	SubViewLogs SubViewKind = iota
	SubViewSlots
	SubViewMetrics
	SubViewHistory
)

// monitorEventMsg wraps a monitor.MonitorEvent received from a per-instance
// subscription channel. Re-armed via listenCmd after each delivery.
type monitorEventMsg struct {
	ev monitor.MonitorEvent
}

// listenCmd reads one event from ch and re-arms itself when handled.
func listenCmd(ch <-chan monitor.MonitorEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return monitorEventMsg{ev: ev}
	}
}

// procMgrIface is the slice of processmgr.Manager that MonitorPage needs.
type procMgrIface interface {
	List() []domain.RunningInstance
	Kill(pid int) error
	Launch(domain.Profile, processmgr.LaunchMode, string) (domain.RunningInstance, error)
	TailLogs(pid int) (io.ReadCloser, error)
	History() []domain.ExitedInstance
}

// backendResolverIface is the subset of backendcatalog.Resolver that
// MonitorPage needs to resolve backend kind on restart.
type backendResolverIface interface {
	Resolve(profile domain.Profile) (backendcatalog.ResolvedBackend, error)
}

// profileStoreIface é o subset de profilestore.Store usado pela MonitorPage
// para implementar `r` (restart real). nil -> `r` cai em modo kill-only.
type profileStoreIface interface {
	Get(id string) (domain.Profile, error)
}

// subState holds per-instance subscription state. Fields populated by later
// tasks (T12-T14); skeleton tracks just the cancel func.
type subState struct {
	cancel func() error
	logs   []string
	slots  monitor.SlotSnapshot
	gpu    monitor.GPUStats
	health monitor.HealthStatus
	mets   monitor.Metrics
	subErr string
}

// Apply mutates the subState according to the concrete type carried by ev.Data.
// When paused is true, log lines are dropped (other event kinds still update).
// The log buffer is capped at 2000 lines.
func (s *subState) Apply(ev monitor.MonitorEvent, paused bool) {
	switch d := ev.Data.(type) {
	case monitor.LogLine:
		if !paused {
			s.logs = append(s.logs, d.Line)
			if len(s.logs) > 2000 {
				s.logs = s.logs[len(s.logs)-2000:]
			}
		}
	case monitor.SlotSnapshot:
		s.slots = d
	case monitor.GPUStats:
		s.gpu = d
	case monitor.HealthStatus:
		s.health = d
	case monitor.Metrics:
		s.mets = d
	}
}

type MonitorPage struct {
	pm                 procMgrIface
	mm                 monitor.Manager
	ps                 profileStoreIface    // injected for `r` real restart (slice 6 / Task 4)
	resolver           backendResolverIface // injected to resolve backend kind on restart
	pendingSelectPID   int                  // set by MonitorSelectPIDMsg, consumed after the next refresh
	tbl                table.Model
	subs               map[int]*subState
	chans              map[int]<-chan monitor.MonitorEvent
	subView            SubViewKind
	history            []domain.ExitedInstance
	paused             bool
	periodicTickActive bool
	width              int
	height             int
	flash              components.Flash
	pauseFlash         string
	restartConfirm     components.Confirm
	killConfirm        components.Confirm
}

// monitorKillConfirmedMsg is emitted by killConfirm.onYes when the user
// confirms a kill. The page handles it in Update so manager.Kill + refresh
// stay on the UI thread.
type monitorKillConfirmedMsg struct{ pid int }

// monitorRestartConfirmedMsg is emitted by restartConfirm.onYes when the user
// confirms a restart. Carries the captured profile + foreground/background flag
// so the async kill+launch dispatch has everything it needs without re-reading
// stale page state.
type monitorRestartConfirmedMsg struct {
	pid        int
	profile    domain.Profile
	background bool
}

// monitorPeriodicTickMsg is delivered every 2s by Init/periodicTickCmd to drive
// background refreshes of the instance list, so crashes detected by the
// processmgr liveness goroutine surface in the UI without user interaction.
type monitorPeriodicTickMsg struct{}

// SetBackendResolver injects the backend resolver used on restart.
func (p *MonitorPage) SetBackendResolver(r backendResolverIface) *MonitorPage {
	p.resolver = r
	return p
}

func NewMonitorPage(pm procMgrIface, mm monitor.Manager, ps profileStoreIface) *MonitorPage {
	cols := []table.Column{
		{Title: "PID", Width: 8},
		{Title: "Port", Width: 6},
		{Title: "Profile", Width: 18},
		{Title: "Uptime", Width: 10},
		{Title: "VRAM", Width: 12},
		{Title: "Tokens/s", Width: 10},
	}
	t := table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(8))
	return &MonitorPage{
		pm:    pm,
		mm:    mm,
		ps:    ps,
		tbl:   t,
		subs:  map[int]*subState{},
		chans: map[int]<-chan monitor.MonitorEvent{},
		flash: components.NewFlash("monitor"),
	}
}

func (p *MonitorPage) SetSize(w, h int) {
	p.width, p.height = w, h
	p.tbl.SetWidth(w)
}

func (p *MonitorPage) Init() tea.Cmd {
	p.periodicTickActive = true
	return tea.Batch(p.refreshInstancesCmd(), p.periodicTickCmd())
}

func (p *MonitorPage) refreshInstancesCmd() tea.Cmd {
	return func() tea.Msg { return monitorInstancesRefreshedMsg{insts: p.pm.List()} }
}

// Reload implements the ui.Reloader contract so RootModel re-polls the
// process manager when the user switches to the Monitor tab. Without
// this, instances spawned/killed externally only surface on the next
// 2s periodic tick.
func (p *MonitorPage) Reload() tea.Cmd {
	return p.refreshInstancesCmd()
}

func (p *MonitorPage) periodicTickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return monitorPeriodicTickMsg{} })
}

type monitorInstancesRefreshedMsg struct {
	insts []domain.RunningInstance
}

// restartResultMsg carries the outcome of an async kill+launch restart.
type restartResultMsg struct {
	pid int
	err error
}

// Update is a thin dispatcher: each typed-message arm delegates to a
// private handle<MsgType> method. Most handlers run the shared tail
// (forwardToConfirms) themselves so non-key messages still reach active
// huh forms (Init handshake, async validation) and so the table sees
// every key for navigation.
func (p *MonitorPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		return p.handleResize(m)
	case tea.KeyMsg:
		return p.handleKey(m)
	case monitorInstancesRefreshedMsg:
		return p.handleInstancesRefreshed(m)
	case restartResultMsg:
		return p.handleRestartResult(m)
	case MonitorSelectPIDMsg:
		return p.handleSelectPID(m)
	case monitorPeriodicTickMsg:
		return p.handlePeriodicTick()
	case monitorKillConfirmedMsg:
		return p.handleKillConfirmed(m)
	case monitorRestartConfirmedMsg:
		return p.handleRestartConfirmed(m)
	case monitorEventMsg:
		return p.handleMonitorEvent(m)
	case components.FlashClearMsg:
		p.flash, _ = p.flash.Update(m)
		return p, nil
	}
	return p, p.forwardToConfirms(msg)
}

func (p *MonitorPage) handleResize(m tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	p.SetSize(m.Width, m.Height)
	return p, p.forwardToConfirms(m)
}

// handleKey routes key input. Confirm overlays consume the key and short-
// circuit the table update so the form keeps focus. The 'k' / 'r' keys
// also short-circuit so the table's default keymap doesn't interpret
// them as line-up / refresh and move the selection off the acted-on row.
func (p *MonitorPage) handleKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	if p.killConfirm.Active() {
		return p, p.handleConfirmKillKey(m)
	}
	if p.restartConfirm.Active() {
		return p, p.handleConfirmRestartKey(m)
	}
	switch {
	case m.Type == tea.KeyRunes && len(m.Runes) == 1 && m.Runes[0] == 'v':
		p.subView = (p.subView + 1) % 4
	case m.Type == tea.KeyRunes && len(m.Runes) == 1 && m.Runes[0] == 'k':
		if pid := p.selectedPID(); pid > 0 {
			return p, p.askConfirmKill(pid)
		}
	case m.Type == tea.KeyRunes && len(m.Runes) == 1 && m.Runes[0] == 'r':
		if pid := p.selectedPID(); pid > 0 {
			return p, p.askConfirmRestart(pid)
		}
	case m.Type == tea.KeySpace:
		p.paused = !p.paused
	}
	return p, p.forwardToConfirms(m)
}

func (p *MonitorPage) handleInstancesRefreshed(m monitorInstancesRefreshedMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if c := p.applyInstances(m.insts); c != nil {
		cmds = append(cmds, c)
	}
	p.history = p.pm.History()
	if p.pendingSelectPID != 0 {
		p.selectRow(p.pendingSelectPID)
		p.pendingSelectPID = 0
	}
	cmds = append(cmds, p.forwardToConfirms(m))
	return p, tea.Batch(cmds...)
}

func (p *MonitorPage) handleRestartResult(m restartResultMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.err != nil {
		p.flash, cmd = p.flash.Set(fmt.Sprintf("restart: pid %d failed: %v", m.pid, m.err))
	}
	return p, tea.Batch(p.refreshInstancesCmd(), p.forwardToConfirms(m), cmd)
}

func (p *MonitorPage) handleSelectPID(m MonitorSelectPIDMsg) (tea.Model, tea.Cmd) {
	// Defer the cursor move until the next refresh has applied fresh rows,
	// so we never select against a stale row list.
	p.pendingSelectPID = m.PID
	return p, tea.Batch(p.refreshInstancesCmd(), p.forwardToConfirms(m))
}

func (p *MonitorPage) handlePeriodicTick() (tea.Model, tea.Cmd) {
	return p, tea.Batch(p.refreshInstancesCmd(), p.periodicTickCmd(), p.forwardToConfirms(monitorPeriodicTickMsg{}))
}

func (p *MonitorPage) handleKillConfirmed(m monitorKillConfirmedMsg) (tea.Model, tea.Cmd) {
	_ = p.pm.Kill(m.pid)
	return p, tea.Batch(p.refreshInstancesCmd(), p.forwardToConfirms(m))
}

func (p *MonitorPage) handleRestartConfirmed(m monitorRestartConfirmedMsg) (tea.Model, tea.Cmd) {
	prof := m.profile
	if p.resolver != nil {
		rb, err := p.resolver.Resolve(prof)
		if err == nil {
			prof.Launch.ResolvedExecutable = rb.ExecutablePath
			prof.Launch.ResolvedBackendKind = rb.Backend.Kind
		}
	}
	return p, tea.Batch(restartCmd(p.pm, m.pid, prof, m.background), p.forwardToConfirms(m))
}

func (p *MonitorPage) handleMonitorEvent(m monitorEventMsg) (tea.Model, tea.Cmd) {
	if st, ok := p.subs[m.ev.PID]; ok {
		st.Apply(m.ev, p.paused)
	}
	var cmds []tea.Cmd
	// Re-arm listener for this PID.
	if ch, ok := p.chans[m.ev.PID]; ok {
		cmds = append(cmds, listenCmd(ch))
	}
	cmds = append(cmds, p.forwardToConfirms(m))
	return p, tea.Batch(cmds...)
}

// forwardToConfirms forwards msg to active confirm forms (so huh's Init /
// validation Cmds land) and to the underlying table (so navigation keys
// reach it). Returned by every handler that does NOT short-circuit.
func (p *MonitorPage) forwardToConfirms(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	if p.killConfirm.Active() {
		var cmd tea.Cmd
		p.killConfirm, cmd = p.killConfirm.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if p.restartConfirm.Active() {
		var cmd tea.Cmd
		p.restartConfirm, cmd = p.restartConfirm.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	t, tc := p.tbl.Update(msg)
	p.tbl = t
	if tc != nil {
		cmds = append(cmds, tc)
	}
	return tea.Batch(cmds...)
}

// askConfirmKill builds and arms the kill-confirmation overlay. The Confirm's
// onYes emits monitorKillConfirmedMsg; the actual Kill happens in Update so
// manager I/O stays on the page.
func (p *MonitorPage) askConfirmKill(pid int) tea.Cmd {
	p.killConfirm = components.NewConfirm(
		fmt.Sprintf("Kill pid=%d?", pid),
		pid,
		func(payload any) tea.Cmd {
			id, _ := payload.(int)
			return func() tea.Msg { return monitorKillConfirmedMsg{pid: id} }
		},
		"Kill",
		"Cancel",
	)
	return p.killConfirm.Init()
}

func (p *MonitorPage) handleConfirmKillKey(msg tea.KeyMsg) tea.Cmd {
	if msg.String() == "esc" {
		p.killConfirm = components.Confirm{}
		return nil
	}
	var cmd tea.Cmd
	p.killConfirm, cmd = p.killConfirm.Update(msg)
	return cmd
}

// restartPayload bundles the data captured at the moment the user opens the
// restart confirm. It is the Confirm.payload so onYes can emit a
// monitorRestartConfirmedMsg with everything needed for the async kill+launch
// dispatch — no need to re-read p.pm.List() at completion time, which would
// race with the periodic refresh.
type restartPayload struct {
	pid        int
	profile    domain.Profile
	background bool
}

// askConfirmRestart preloads the profile for the selected PID and arms a
// confirmation form. If the instance or profile is missing, it surfaces the
// error via p.flash instead of opening the form.
func (p *MonitorPage) askConfirmRestart(pid int) tea.Cmd {
	insts := p.pm.List()
	var inst *domain.RunningInstance
	for i := range insts {
		if insts[i].PID == pid {
			inst = &insts[i]
			break
		}
	}
	var cmd tea.Cmd
	if inst == nil {
		p.flash, cmd = p.flash.Set(fmt.Sprintf("restart: pid %d not found", pid))
		return cmd
	}
	if p.ps == nil {
		p.flash, cmd = p.flash.Set("restart: profile store not available")
		return cmd
	}
	prof, err := p.ps.Get(inst.ProfileID)
	if err != nil {
		p.flash, cmd = p.flash.Set(fmt.Sprintf("restart: profile %q not found", inst.ProfileID))
		return cmd
	}
	payload := restartPayload{pid: pid, profile: prof, background: inst.Background}
	p.restartConfirm = components.NewConfirm(
		fmt.Sprintf("Restart pid=%d (%s)?", pid, prof.Name),
		payload,
		func(arg any) tea.Cmd {
			rp, _ := arg.(restartPayload)
			return func() tea.Msg {
				return monitorRestartConfirmedMsg{pid: rp.pid, profile: rp.profile, background: rp.background}
			}
		},
		"Restart",
		"Cancel",
	)
	return p.restartConfirm.Init()
}

func (p *MonitorPage) handleConfirmRestartKey(msg tea.KeyMsg) tea.Cmd {
	if msg.String() == "esc" {
		p.restartConfirm = components.Confirm{}
		return nil
	}
	var cmd tea.Cmd
	p.restartConfirm, cmd = p.restartConfirm.Update(msg)
	return cmd
}

// restartCmd performs Kill then Launch off the UI thread and delivers the
// result wrapped in a restartResultMsg.
func restartCmd(pm procMgrIface, pid int, prof domain.Profile, bg bool) tea.Cmd {
	return func() tea.Msg {
		if err := pm.Kill(pid); err != nil {
			return restartResultMsg{pid: pid, err: fmt.Errorf("kill: %w", err)}
		}
		mode := processmgr.LaunchBackground
		if !bg {
			mode = processmgr.LaunchForeground
		}
		if _, err := pm.Launch(prof, mode, log.NewAttemptID()); err != nil {
			return restartResultMsg{pid: pid, err: fmt.Errorf("launch: %w", err)}
		}
		return restartResultMsg{pid: pid}
	}
}

// IsCapturingInput tells the root model when the page owns global keys.
func (p *MonitorPage) IsCapturingInput() bool {
	return p.killConfirm.Active() || p.restartConfirm.Active()
}

// applyInstances reconciles the page's per-PID table rows and subscription
// state against a fresh instance list. Decomposed into single-purpose helpers:
// row rendering, cursor clamping, subscription bring-up, and dead-sub reaping.
func (p *MonitorPage) applyInstances(insts []domain.RunningInstance) tea.Cmd {
	p.tbl.SetRows(p.renderRows(insts))
	p.clampCursor(len(insts))
	cmds := p.ensureSubscriptions(insts)
	p.reapDeadSubscriptions(insts)
	return tea.Batch(cmds...)
}

// renderRows formats one table row per instance. Crashed instances get an
// error-styled badge across every column.
func (p *MonitorPage) renderRows(insts []domain.RunningInstance) []table.Row {
	rows := make([]table.Row, 0, len(insts))
	for _, ri := range insts {
		pidCol := fmt.Sprintf("%d", ri.PID)
		profileCol := ri.ProfileID
		if ri.Crashed {
			pidCol = "✗ " + pidCol
			profileCol = ri.ProfileID + " (crashed)"
		}
		uptime, vram, toks := "--", "--", "--"
		if !ri.StartedAt.IsZero() {
			uptime = humanDuration(time.Since(ri.StartedAt))
		}
		if st, ok := p.subs[ri.PID]; ok && st != nil {
			vram = formatVRAM(st.gpu.VRAMUsedMB, st.gpu.VRAMTotalMB)
			toks = formatTokensPerSec(st.mets.TokensPerSec)
		}
		portCol := fmt.Sprintf("%d", ri.Port)
		if ri.Crashed {
			pidCol = theme.Error.Render(pidCol)
			portCol = theme.Error.Render(portCol)
			profileCol = theme.Error.Render(profileCol)
			uptime = theme.Error.Render(uptime)
			vram = theme.Error.Render(vram)
			toks = theme.Error.Render(toks)
		}
		rows = append(rows, table.Row{
			pidCol,
			portCol,
			profileCol,
			uptime, vram, toks,
		})
	}
	return rows
}

// clampCursor pulls the table cursor back into range when the row count
// shrinks. Bubbles' table doesn't auto-clamp, so SelectedRow can later
// return an empty Row and panic on row[0] without this.
func (p *MonitorPage) clampCursor(rowCount int) {
	if cur := p.tbl.Cursor(); cur >= rowCount {
		if rowCount == 0 {
			p.tbl.SetCursor(0)
		} else {
			p.tbl.SetCursor(rowCount - 1)
		}
	}
}

// ensureSubscriptions starts a subscription for each non-crashed instance
// that doesn't already have a healthy one, returning a listenCmd per new
// subscription. Existing subscriptions with errors are retried.
func (p *MonitorPage) ensureSubscriptions(insts []domain.RunningInstance) []tea.Cmd {
	var cmds []tea.Cmd
	for _, ri := range insts {
		if ri.Crashed {
			continue
		}
		if st, ok := p.subs[ri.PID]; ok && st.subErr == "" {
			continue
		}
		ch, cancel, err := p.mm.Subscribe(ri.PID, ri.Port, ri.LogPath)
		if err != nil {
			p.subs[ri.PID] = &subState{cancel: func() error { return nil }, subErr: err.Error()}
			continue
		}
		delete(p.subs, ri.PID)
		p.subs[ri.PID] = &subState{cancel: cancel}
		p.chans[ri.PID] = ch
		cmds = append(cmds, listenCmd(ch))
	}
	return cmds
}

// reapDeadSubscriptions cancels and drops any subscription whose PID is
// either absent from insts (orphan) or marked Crashed (data source dead).
// Cancel runs in a goroutine so a slow teardown can't stall the UI thread.
func (p *MonitorPage) reapDeadSubscriptions(insts []domain.RunningInstance) {
	byPID := make(map[int]domain.RunningInstance, len(insts))
	for _, ri := range insts {
		byPID[ri.PID] = ri
	}
	for pid, st := range p.subs {
		inst, present := byPID[pid]
		if present && !inst.Crashed {
			continue
		}
		cancel := st.cancel
		go func() { _ = cancel() }()
		delete(p.subs, pid)
		delete(p.chans, pid)
	}
}

func (p *MonitorPage) View() string {
	if p.killConfirm.Active() {
		return p.killConfirm.View()
	}
	if p.restartConfirm.Active() {
		return p.restartConfirm.View()
	}
	if len(p.tbl.Rows()) == 0 {
		header := theme.Title.Render("Running instances")
		if p.flash.Message() != "" {
			header = p.flash.View() + "\n" + header
		}
		return header + "\n" + theme.Subtitle.Render("(no instances running — switch to Launcher [1] to start one)")
	}
	return p.renderTable() + "\n\n" + p.renderStatusLine() + "\n" + p.renderSubViewBody()
}

// renderTable renders the bold "Running instances" header (prefixed with the
// flash banner when set) followed by the bubbletea instances table.
func (p *MonitorPage) renderTable() string {
	header := theme.Title.Render("Running instances")
	if p.flash.Message() != "" {
		header = p.flash.View() + "\n" + header
	}
	return header + "\n" + p.tbl.View()
}

// renderStatusLine renders the Logs / Slots / Metrics tab strip that sits
// between the instance table and the active sub-view body.
func (p *MonitorPage) renderStatusLine() string {
	return renderSubViewTabs(p.subView)
}

// renderSubViewBody renders the body of the active sub-view (logs, slots, or
// metrics) for the currently-selected instance, or a fallback string when no
// subscription state is available.
func (p *MonitorPage) renderSubViewBody() string {
	pid := p.selectedPID()
	st := p.subs[pid]
	if st == nil {
		return "no subscription"
	}
	switch p.subView {
	case SubViewLogs:
		if st.subErr != "" {
			return theme.Error.Render("Logs unavailable: " + st.subErr)
		}
		start := len(st.logs) - 10
		if start < 0 {
			start = 0
		}
		bottom := strings.Join(st.logs[start:], "\n")
		if bottom == "" {
			bottom = "(no log lines yet)"
		}
		if p.paused {
			bottom = theme.Warn.Render("Logs (PAUSED — Space to resume)") + "\n" + bottom
		}
		if len(st.logs) > 10 {
			bottom += "\n" + theme.Subtitle.Render(fmt.Sprintf("— showing last 10 of %d (Space pauses, buffer 2000)", len(st.logs)))
		}
		return bottom
	case SubViewSlots:
		var b strings.Builder
		b.WriteString("idx | state      | ctx used/max | client\n")
		for _, s := range st.slots.Slots {
			fmt.Fprintf(&b, "%-3d | %-10s | %5d/%-5d | %s\n", s.ID, s.State, s.NCtxUsed, s.NCtxMax, s.Client)
		}
		bottom := b.String()
		if bottom == "idx | state      | ctx used/max | client\n" {
			bottom = "(no slot data yet)"
		}
		return bottom
	case SubViewMetrics:
		if st == nil || st.subErr != "" {
			return theme.Subtitle.Render("GPU metrics unavailable — check nvidia-smi or monitoring service")
		}
		if len(st.mets.TokensPerSec) == 0 && len(st.mets.RequestsPerSec) == 0 {
			return "(no metrics yet — first sample arrives after the slots tick)"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "tokens/s: %s\n", theme.OK.Render(components.Sparkline(st.mets.TokensPerSec, 40)))
		fmt.Fprintf(&b, "req/s   : %s\n", theme.Warn.Render(components.Sparkline(st.mets.RequestsPerSec, 40)))
		if st.gpu.VRAMTotalMB > 0 {
			fmt.Fprintf(&b, "VRAM    : %d/%d MB  util %.0f%%\n", st.gpu.VRAMUsedMB, st.gpu.VRAMTotalMB, st.gpu.Utilization)
		}
		return b.String()
	case SubViewHistory:
		return p.renderHistory()
	}
	return "no subscription"
}

// renderHistory renders the exit-history rows for the History sub-view.
func (p *MonitorPage) renderHistory() string {
	if len(p.history) == 0 {
		return "(no exit history yet)"
	}
	var b strings.Builder
	b.WriteString("profile          │ pid  │ started            │ exited             │ duration │ reason          │ stderr\n")
	for _, h := range p.history {
		started := h.StartedAt.Format("2006-01-02 15:04:05")
		exited := h.ExitedAt.Format("2006-01-02 15:04:05")
		dur := humanDuration(time.Duration(h.DurationSeconds) * time.Second)
		reason := h.ExitReason
		if reason == "" {
			reason = "—"
		}
		stderr := fmt.Sprintf("%d lines", len(h.StderrTail))
		if len(h.StderrTail) == 0 {
			stderr = "—"
		}
		fmt.Fprintf(&b, "%-16s │ %-4d │ %s │ %s │ %-8s │ %-15s │ %s\n",
			h.ProfileID, h.PID, started, exited, dur, reason, stderr)
	}
	return b.String()
}

// renderSubViewTabs draws the Logs / Slots / Metrics / History tab strip with the
// active sub-view styled via theme.TabActive. Cycled by the [v] key.
func renderSubViewTabs(active SubViewKind) string {
	render := func(k SubViewKind, label string) string {
		if k == active {
			return theme.TabActive.Render(label)
		}
		return theme.TabInactive.Render(label)
	}
	parts := []string{
		render(SubViewLogs, "Logs"),
		theme.Subtitle.Render(" │ "),
		render(SubViewSlots, "Slots"),
		theme.Subtitle.Render(" │ "),
		render(SubViewMetrics, "Metrics"),
		theme.Subtitle.Render(" │ "),
		render(SubViewHistory, "History"),
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// Hints implements ui.HintProvider for the Monitor tab.
func (p *MonitorPage) Hints() string {
	if p.killConfirm.Active() || p.restartConfirm.Active() {
		return "[←→] choose  [enter] confirm  [esc] cancel"
	}
	return "[v] cycle view  [Space] pause  [k] kill  [r] restart"
}

// humanDuration formats a Duration as a compact uptime string (e.g. "5s",
// "3m12s", "1h04m"). Negative or zero returns "--".
func humanDuration(d time.Duration) string {
	if d <= 0 {
		return "--"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d / time.Minute)
		s := int((d % time.Minute) / time.Second)
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	return fmt.Sprintf("%dh%02dm", h, m)
}

// formatVRAM renders the per-instance VRAM cell. Returns "--" when no
// totalsample yet.
func formatVRAM(usedMB, totalMB uint64) string {
	if totalMB == 0 {
		return "--"
	}
	return fmt.Sprintf("%d/%dMB", usedMB, totalMB)
}

// formatTokensPerSec returns the latest tokens-per-second sample formatted
// to one decimal place, or "--" when the metric series is empty.
func formatTokensPerSec(samples []float64) string {
	if len(samples) == 0 {
		return "--"
	}
	return fmt.Sprintf("%.1f", samples[len(samples)-1])
}

// selectRow positions the table cursor on the row matching pid (no-op if not found).
func (p *MonitorPage) selectRow(pid int) {
	rows := p.tbl.Rows()
	for i, r := range rows {
		pidCol := strings.TrimPrefix(stripANSI(r[0]), "✗ ")
		var rowPID int
		_, _ = fmt.Sscanf(pidCol, "%d", &rowPID)
		if rowPID == pid {
			p.tbl.SetCursor(i)
			return
		}
	}
}

// stripANSI removes ANSI SGR escape sequences from s. Used when parsing
// PIDs out of table rows that may have been styled (e.g. crashed rows
// rendered in theme.Error).
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			// Skip until 'm' (or end).
			i++
			for i < len(s) && s[i] != 'm' {
				i++
			}
			if i < len(s) {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// selectedPID returns the PID of the currently selected row, or 0 if no rows.
// Guarded against `table.Model.SelectedRow()` returning an empty Row when the
// cursor is out of range (e.g. rows shrunk after a row was killed) — without
// the length check, row[0] panics with index out of range.
func (p *MonitorPage) selectedPID() int {
	if len(p.tbl.Rows()) == 0 {
		return 0
	}
	row := p.tbl.SelectedRow()
	if len(row) == 0 {
		return 0
	}
	pidCol := strings.TrimPrefix(stripANSI(row[0]), "✗ ")
	var pid int
	_, _ = fmt.Sscanf(pidCol, "%d", &pid)
	return pid
}
