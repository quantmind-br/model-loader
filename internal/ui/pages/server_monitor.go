package pages

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/metricsstore"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
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

// monitorPeriodicTickMsg is delivered every 2s by Init/periodicTickCmd to drive
// background refreshes of the instance list, so crashes detected by the
// processmgr liveness goroutine surface in the UI without user interaction.
type monitorPeriodicTickMsg struct{}

type monitorInstancesRefreshedMsg struct {
	insts []domain.RunningInstance
}

func (p *ServerPage) periodicTickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg { return monitorPeriodicTickMsg{} })
}

func (p *ServerPage) handleInstancesRefreshed(m monitorInstancesRefreshedMsg) (tea.Model, tea.Cmd) {
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

func (p *ServerPage) handleSelectPID(m ServerSelectPIDMsg) (tea.Model, tea.Cmd) {
	// Defer the cursor move until the next refresh has applied fresh rows,
	// so we never select against a stale row list.
	p.pendingSelectPID = m.PID
	return p, tea.Batch(p.refreshInstancesCmd(), p.forwardToConfirms(m))
}

func (p *ServerPage) handlePeriodicTick() (tea.Model, tea.Cmd) {
	return p, tea.Batch(p.refreshInstancesCmd(), p.periodicTickCmd(), p.forwardToConfirms(monitorPeriodicTickMsg{}))
}

func (p *ServerPage) handleMonitorEvent(m monitorEventMsg) (tea.Model, tea.Cmd) {
	if st, ok := p.subs[m.ev.PID]; ok {
		st.Apply(m.ev, p.paused)
	}
	if m.ev.Source == monitor.SourceMetrics && p.metricsDir != "" {
		if metrics, ok := m.ev.Data.(monitor.Metrics); ok {
			var profileID string
			for _, inst := range p.pm.List() {
				if inst.PID == m.ev.PID {
					profileID = inst.ProfileID
					break
				}
			}
			if profileID != "" {
				var tps float64
				if len(metrics.TokensPerSec) > 0 {
					tps = metrics.TokensPerSec[len(metrics.TokensPerSec)-1]
				}
				var rps float64
				if len(metrics.RequestsPerSec) > 0 {
					rps = metrics.RequestsPerSec[len(metrics.RequestsPerSec)-1]
				}
				_ = metricsstore.Append(p.metricsDir, profileID, metricsstore.Record{
					TS:           m.ev.Timestamp.Unix(),
					TokensPerSec: tps,
					RPS:          rps,
				})
			}
		}
	}
	var cmds []tea.Cmd
	// Re-arm listener for this PID.
	if ch, ok := p.chans[m.ev.PID]; ok {
		cmds = append(cmds, listenCmd(ch))
	}
	cmds = append(cmds, p.forwardToConfirms(m))
	return p, tea.Batch(cmds...)
}

// applyInstances reconciles the page's per-PID table rows and subscription
// state against a fresh instance list. Decomposed into single-purpose helpers:
// row rendering, cursor clamping, subscription bring-up, and dead-sub reaping.
func (p *ServerPage) applyInstances(insts []domain.RunningInstance) tea.Cmd {
	p.tbl.SetRows(p.renderRows(insts))
	p.clampCursor(len(insts))
	cmds := p.ensureSubscriptions(insts)
	p.reapDeadSubscriptions(insts)
	return tea.Batch(cmds...)
}

// renderRows formats one table row per instance. Crashed instances get an
// error-styled badge across every column.
func (p *ServerPage) renderRows(insts []domain.RunningInstance) []table.Row {
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
func (p *ServerPage) clampCursor(rowCount int) {
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
func (p *ServerPage) ensureSubscriptions(insts []domain.RunningInstance) []tea.Cmd {
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
func (p *ServerPage) reapDeadSubscriptions(insts []domain.RunningInstance) {
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

// selectRow positions the table cursor on the row matching pid (no-op if not found).
func (p *ServerPage) selectRow(pid int) {
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
