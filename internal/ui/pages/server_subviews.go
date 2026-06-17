package pages

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/service/metricsstore"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// SubViewKind selects which bottom region the ServerPage renders.
type SubViewKind int

const (
	SubViewLogs SubViewKind = iota
	SubViewSlots
	SubViewMetrics
	SubViewHistory
)

func (p *ServerPage) openHistoryChart() (tea.Model, tea.Cmd) {
	pid := p.selectedPID()
	if pid <= 0 {
		return p.withFlashError("history: select a running instance first")
	}
	if p.metricsDir == "" {
		return p.withFlashError("history: metrics directory unavailable")
	}
	insts := p.pm.List()
	var profileID string
	for _, inst := range insts {
		if inst.PID == pid {
			profileID = inst.ProfileID
			break
		}
	}
	if profileID == "" {
		return p.withFlashError("history: no profile for selected instance")
	}
	recs, err := metricsstore.Read(p.metricsDir, profileID, time.Now().Add(-24*time.Hour))
	if err != nil {
		return p.withFlashError("history: " + err.Error())
	}
	p.historyChart = &components.HistoryChart{}
	*p.historyChart = components.NewHistoryChart(recs, time.Hour)
	p.historyChart.SetSize(p.width, p.height/2)
	return p, nil
}

func (p *ServerPage) switchHistoryWindow(key rune) (tea.Model, tea.Cmd) {
	if p.historyChart == nil {
		return p, nil
	}
	var window time.Duration
	switch key {
	case '1':
		window = time.Hour
	case '2':
		window = 6 * time.Hour
	case '3':
		window = 24 * time.Hour
	case '4':
		window = 7 * 24 * time.Hour
	}
	pid := p.selectedPID()
	insts := p.pm.List()
	var profileID string
	for _, inst := range insts {
		if inst.PID == pid {
			profileID = inst.ProfileID
			break
		}
	}
	if profileID != "" && p.metricsDir != "" {
		recs, _ := metricsstore.Read(p.metricsDir, profileID, time.Now().Add(-window))
		*p.historyChart = components.NewHistoryChart(recs, window)
		p.historyChart.SetSize(p.width, p.height/2)
	}
	return p, nil
}

// renderTable renders the bold "Running instances" header (prefixed with the
// flash banner when set, suffixed with a PAUSED marker when refresh is
// paused) followed by the bubbletea instances table. The marker is plain
// ASCII — no ⏸ or em-dash — because ambiguous-width glyphs corrupt borders
// (see RENDER-02 in root.go).
func (p *ServerPage) renderTable() string {
	header := theme.Title.Render("Running instances")
	if p.paused {
		header += "  " + theme.Warn.Render("[PAUSED - Space to resume]")
	}
	if p.flash.Message() != "" {
		header = p.flash.View() + "\n" + header
	}
	return header + "\n" + p.tbl.View()
}

// renderStatusLine renders the Logs / Slots / Metrics tab strip that sits
// between the instance table and the active sub-view body.
func (p *ServerPage) renderStatusLine() string {
	return renderSubViewTabs(p.subView)
}

// renderSubViewBody renders the body of the active sub-view (logs, slots,
// metrics, or history) for the currently-selected instance, or a fallback
// string when no subscription state is available.
func (p *ServerPage) renderSubViewBody() string {
	pid := p.selectedPID()
	st := p.subs[pid]
	if st == nil {
		return "no subscription"
	}
	switch p.subView {
	case SubViewLogs:
		return p.renderLogs(st)
	case SubViewSlots:
		return p.renderSlots(st)
	case SubViewMetrics:
		return p.renderMetrics(st)
	case SubViewHistory:
		return p.renderHistory()
	}
	return "no subscription"
}

// renderLogs renders the tail of the selected instance's log buffer, sized to
// the available height and annotated with the paused banner / truncation note.
func (p *ServerPage) renderLogs(st *subState) string {
	if st.subErr != "" {
		return theme.Error.Render("Logs unavailable: " + st.subErr)
	}
	visible := p.height - 12 // header + table + sub-tabs + status + flash + margins
	if visible < 5 {
		visible = 5
	}
	start := len(st.logs) - visible
	if start < 0 {
		start = 0
	}
	bottom := strings.Join(st.logs[start:], "\n")
	if bottom == "" {
		bottom = "(no log lines yet)"
	}
	if p.paused {
		bottom = theme.Warn.Render("Logs (PAUSED — Space to resume)") + "\n" +
			theme.Subtitle.Render(centeredDivider("PAUSED", p.width-4)) + "\n" + bottom
	}
	if len(st.logs) > visible {
		bottom += "\n" + theme.Subtitle.Render(fmt.Sprintf("— showing last %d of %d (Space pauses, buffer 2000)", visible, len(st.logs)))
	}
	return bottom
}

// renderSlots renders the per-slot table for the selected instance.
func (p *ServerPage) renderSlots(st *subState) string {
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
}

// renderMetrics renders the tokens/s + req/s sparklines and VRAM line for the
// selected instance.
func (p *ServerPage) renderMetrics(st *subState) string {
	if st.subErr != "" {
		return theme.Subtitle.Render("GPU metrics unavailable — check nvidia-smi or monitoring service")
	}
	if len(st.mets.TokensPerSec) == 0 && len(st.mets.RequestsPerSec) == 0 {
		return "(no metrics yet — first sample arrives after the slots tick)"
	}
	// tokens/s is only sampled while the model is actively decoding, but
	// req/s gets a 0-rate sample every slot tick. Inject an idle baseline
	// so the tokens/s row renders a flat sparkline instead of blank space
	// next to a populated req/s row (RENDER-03).
	tokens := st.mets.TokensPerSec
	if len(tokens) == 0 {
		tokens = []float64{0}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "tokens/s: %s\n", theme.OK.Render(components.Sparkline(tokens, 40)))
	fmt.Fprintf(&b, "req/s   : %s\n", theme.Warn.Render(components.Sparkline(st.mets.RequestsPerSec, 40)))
	if st.gpu.VRAMTotalMB > 0 {
		fmt.Fprintf(&b, "VRAM    : %d/%d MB  util %.0f%%\n", st.gpu.VRAMUsedMB, st.gpu.VRAMTotalMB, st.gpu.Utilization)
	}
	return b.String()
}

// renderHistory renders the exit-history rows for the History sub-view,
// fitting the table to the available terminal width. The pid/started/exited/
// duration columns are fixed; the remaining budget is shared by the variable
// profile/reason/stderr columns and every cell is truncated so a row never
// overflows and wraps onto a second line (RENDER: history overflow).
func (p *ServerPage) renderHistory() string {
	if len(p.history) == 0 {
		return "(no exit history yet)"
	}
	const pidW, startedW, exitedW, durW = 7, 10, 10, 8
	const sep = " │ "
	const sepCount = 6 // separators between the 7 columns
	width := p.width
	if width <= 0 {
		width = 80
	}
	flex := width - (pidW + startedW + exitedW + durW) - len([]rune(sep))*sepCount
	if flex < 24 {
		flex = 24
	}
	profileW := flex * 4 / 10 // ~40%
	if profileW < 8 {
		profileW = 8
	}
	reasonW := flex * 3 / 10 // ~30%
	if reasonW < 6 {
		reasonW = 6
	}
	stderrW := flex - profileW - reasonW
	if stderrW < 6 {
		stderrW = 6
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%-*s%s%-*s%s%-*s%s%-*s%s%-*s%s%-*s%s%s\n",
		profileW, truncate("profile", profileW), sep,
		pidW, "pid", sep,
		startedW, "started", sep,
		exitedW, "exited", sep,
		durW, "duration", sep,
		reasonW, truncate("reason", reasonW), sep,
		truncate("stderr", stderrW))
	now := time.Now()
	for _, h := range p.history {
		started := humanRelative(h.StartedAt, now)
		exited := humanRelative(h.ExitedAt, now)
		dur := humanDuration(time.Duration(h.DurationSeconds) * time.Second)
		reason := h.ExitReason
		if reason == "" {
			reason = "—"
		}
		stderr := fmt.Sprintf("%d lines", len(h.StderrTail))
		if len(h.StderrTail) == 0 {
			stderr = "—"
		}
		fmt.Fprintf(&b, "%-*s%s%-*d%s%-*s%s%-*s%s%-*s%s%-*s%s%s\n",
			profileW, truncate(h.ProfileID, profileW), sep,
			pidW, h.PID, sep,
			startedW, truncate(started, startedW), sep,
			exitedW, truncate(exited, exitedW), sep,
			durW, truncate(dur, durW), sep,
			reasonW, truncate(reason, reasonW), sep,
			truncate(stderr, stderrW))
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

func humanRelative(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return t.Format("2006-01-02")
	}
}

func centeredDivider(label string, width int) string {
	lw := len(label)
	if width < lw+6 {
		return strings.Repeat("─", width)
	}
	side := (width - lw - 2) / 2
	return strings.Repeat("─", side) + " " + label + " " + strings.Repeat("─", width-side-lw-2)
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
func (p *ServerPage) selectedPID() int {
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
