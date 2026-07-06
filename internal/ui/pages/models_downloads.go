package pages

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// rateSample is the last observed (time, bytes) point for a download plus an
// EMA-smoothed transfer speed in bytes/sec. The Downloads section derives the
// live speed and ETA from these samples; downloadmgr itself only reports
// cumulative Bytes/Total.
type rateSample struct {
	t     time.Time
	bytes int64
	speed float64
}

// modelsTickMsg drives the ~1s refresh of download speed/ETA while the
// download manager is wired (mirrors ServerPage.periodicTickCmd).
type modelsTickMsg struct{}

func (p ModelsPage) tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return modelsTickMsg{} })
}

// updateRates folds a fresh snapshot into p.rates, computing an EMA-smoothed
// speed per download. Terminal downloads keep their last sample so a finished
// transfer can still show its average without spiking.
func (p ModelsPage) updateRates(states []downloadmgr.State) {
	if p.rates == nil {
		return
	}
	now := time.Now()
	seen := make(map[string]struct{}, len(states))
	for _, st := range states {
		id := string(st.ID)
		seen[id] = struct{}{}
		prev, ok := p.rates[id]
		if ok && st.Status == downloadmgr.StatusActive {
			dt := now.Sub(prev.t).Seconds()
			if dt > 0 {
				inst := float64(st.Bytes-prev.bytes) / dt
				if inst < 0 {
					inst = 0
				}
				sp := inst
				if prev.speed > 0 {
					sp = 0.5*inst + 0.5*prev.speed
				}
				p.rates[id] = rateSample{t: now, bytes: st.Bytes, speed: sp}
				continue
			}
		}
		// First sighting, or a non-active state: record bytes, keep prior speed.
		p.rates[id] = rateSample{t: now, bytes: st.Bytes, speed: prev.speed}
	}
	// Drop samples for downloads the manager no longer reports.
	for id := range p.rates {
		if _, ok := seen[id]; !ok {
			delete(p.rates, id)
		}
	}
}

// activeDownloadCount counts downloads currently transferring or queued — the
// number shown on the Downloads section tab.
func (p ModelsPage) activeDownloadCount() int {
	if p.dlManager == nil {
		return 0
	}
	n := 0
	for _, st := range p.dlManager.Snapshot() {
		if st.Status == downloadmgr.StatusActive || st.Status == downloadmgr.StatusQueued {
			n++
		}
	}
	return n
}

// downloadList returns the downloads to display in the Downloads section,
// ordered by start time. When hideDone is set, terminal entries are filtered
// out (the [c] "clear done" action).
func (p ModelsPage) downloadList() []downloadmgr.State {
	if p.dlManager == nil {
		return nil
	}
	states := p.dlManager.Snapshot()
	sort.SliceStable(states, func(i, j int) bool {
		return states[i].StartedAt.Before(states[j].StartedAt)
	})
	if !p.hideDone {
		return states
	}
	out := states[:0]
	for _, st := range states {
		if isTerminalStatus(st.Status) {
			continue
		}
		out = append(out, st)
	}
	return out
}

func isTerminalStatus(s downloadmgr.Status) bool {
	switch s {
	case downloadmgr.StatusCompleted, downloadmgr.StatusFailed,
		downloadmgr.StatusCancelled, downloadmgr.StatusAbandoned:
		return true
	default:
		return false
	}
}

// handleDownloadsKey owns keystrokes while the Downloads section is shown.
// None of these keys are global shortcuts, so the page does not need to claim
// input capture for this section.
func (p ModelsPage) handleDownloadsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	list := p.downloadList()
	switch msg.String() {
	case "up", "k":
		if p.dlFocus > 0 {
			p.dlFocus--
		}
		return p, nil
	case "down", "j":
		if p.dlFocus < len(list)-1 {
			p.dlFocus++
		}
		return p, nil
	case "c":
		p.hideDone = !p.hideDone
		p.dlFocus = 0
		return p, nil
	case "C":
		// Clearing the history is irreversible and 'C' sits one Shift away
		// from the reversible [c] hide-done toggle, so gate it behind a
		// confirm (default Cancel) instead of acting immediately (DESTRUCT-02).
		return p.askClearDone()
	case "x":
		if st, ok := p.focusedDownload(list); ok && st.Status == downloadmgr.StatusActive {
			if p.dlManager != nil {
				_ = p.dlManager.Cancel(st.ID)
			}
			return p.withFlash("cancelling " + st.Spec.Filename)
		}
		return p, nil
	case "r":
		if st, ok := p.focusedDownload(list); ok &&
			(st.Status == downloadmgr.StatusFailed || st.Status == downloadmgr.StatusAbandoned) {
			if p.dlManager != nil {
				if err := p.dlManager.Resume(st.ID); err != nil {
					return p.withFlashError("resume: " + err.Error())
				}
			}
			return p.withFlash("resuming " + st.Spec.Filename)
		}
		return p, nil
	}
	return p, nil
}

func (p ModelsPage) focusedDownload(list []downloadmgr.State) (downloadmgr.State, bool) {
	if p.dlFocus >= 0 && p.dlFocus < len(list) {
		return list[p.dlFocus], true
	}
	return downloadmgr.State{}, false
}

// performDownloadClear runs the actual ClearTerminal after the user confirms
// the [C] clear-all action (DESTRUCT-02). Invoked from Update on
// downloadClearConfirmedMsg so manager I/O stays on the page.
func (p ModelsPage) performDownloadClear() (tea.Model, tea.Cmd) {
	if p.dlManager == nil {
		return p, nil
	}
	removed, err := p.dlManager.ClearTerminal()
	if err != nil {
		return p.withFlashError("clear history: " + err.Error())
	}
	if removed == 0 {
		return p.withFlash("no finished downloads to clear")
	}
	return p.withFlash(fmt.Sprintf("cleared %d finished download(s)", removed))
}

// renderDownloadsView renders the Downloads queue: a progress bar with
// percent, transferred/total, speed and ETA per active transfer, plus queued
// and finished entries and an aggregate line. The page header, section tabs
// and flash are added by View.
func (p ModelsPage) renderDownloadsView() string {
	if p.dlManager == nil {
		return theme.Subtitle.Render("Downloads are not available (no download manager wired).")
	}
	list := p.downloadList()
	if len(list) == 0 {
		return theme.Subtitle.Render("No downloads yet. Open Discover [→] to find models on Hugging Face.")
	}

	width := p.width
	if width <= 0 {
		width = 80
	}

	var lines []string
	var totBytes, totTotal int64
	var totSpeed float64
	for i, st := range list {
		focused := i == p.dlFocus
		lines = append(lines, p.renderDownloadRow(st, focused, width))
		totBytes += st.Bytes
		totTotal += st.Total
		if st.Status == downloadmgr.StatusActive {
			totSpeed += p.rates[string(st.ID)].speed
		}
	}

	agg := fmt.Sprintf("Total: %s / %s", fmtBytes(totBytes), fmtBytes(totTotal))
	if totSpeed > 0 {
		agg += "   " + fmtSpeed(totSpeed)
	}
	lines = append(lines, "", theme.Subtitle.Render(agg))
	return strings.Join(lines, "\n")
}

func (p ModelsPage) renderDownloadRow(st downloadmgr.State, focused bool, width int) string {
	name := st.Spec.Filename
	if name == "" {
		name = st.Spec.RepoID
	}

	cursor := "  "
	if focused {
		cursor = theme.OK.Render("> ")
	}

	var ratio float64
	if st.Total > 0 {
		ratio = float64(st.Bytes) / float64(st.Total)
		if ratio > 1 {
			ratio = 1
		}
	}

	switch st.Status {
	case downloadmgr.StatusActive:
		bar := renderBar(ratio, min(24, max(8, width-30)))
		speed := p.rates[string(st.ID)].speed
		eta := fmtETA(st.Bytes, st.Total, speed)
		header := fmt.Sprintf("%s%s", cursor, theme.TruncateRuneWidth(name, width-2, "…"))
		stats := fmt.Sprintf("  %s %3.0f%%  %s / %s  %s  ETA %s",
			bar, ratio*100, fmtBytes(st.Bytes), fmtBytes(st.Total), fmtSpeed(speed), eta)
		return header + "\n" + stats
	case downloadmgr.StatusQueued:
		return fmt.Sprintf("%s%s  %s  %s", cursor,
			theme.TruncateRuneWidth(name, max(10, width-24), "…"),
			theme.Subtitle.Render("[queued]"), fmtBytes(st.Total))
	default:
		status := styleDownloadStatus(st.Status)
		line := fmt.Sprintf("%s%s  %s", cursor,
			theme.TruncateRuneWidth(name, max(10, width-24), "…"), status)
		if st.Status == downloadmgr.StatusFailed && st.Err != nil {
			line += theme.Error.Render(": " + truncate(st.Err.Error(), max(8, width-lipgloss.Width(line)-2)))
		}
		return line
	}
}

func styleDownloadStatus(s downloadmgr.Status) string {
	switch s {
	case downloadmgr.StatusCompleted:
		return theme.OK.Render("[done]")
	case downloadmgr.StatusFailed:
		return theme.Error.Render("[failed]")
	case downloadmgr.StatusAbandoned:
		return theme.Warn.Render("[abandoned]")
	case downloadmgr.StatusCancelled:
		return theme.Subtitle.Render("[cancelled]")
	default:
		return theme.Subtitle.Render("[" + s.String() + "]")
	}
}

// renderBar draws a fixed-width progress bar. The filled portion is accent
// coloured; the remainder is dim. NoColor terminals still read correctly via
// the distinct glyphs.
func renderBar(ratio float64, width int) string {
	if width < 1 {
		return ""
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	full := lipgloss.NewStyle().Foreground(theme.ColorAccent).Render(strings.Repeat("█", filled))
	empty := theme.Subtitle.Render(strings.Repeat("░", width-filled))
	return "[" + full + empty + "]"
}

func fmtBytes(b int64) string {
	const (
		kb = 1 << 10
		mb = 1 << 20
		gb = 1 << 30
		tb = 1 << 40
	)
	switch {
	case b >= tb:
		return fmt.Sprintf("%.2f TB", float64(b)/tb)
	case b >= gb:
		return fmt.Sprintf("%.2f GB", float64(b)/gb)
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/mb)
	case b >= kb:
		return fmt.Sprintf("%.0f KB", float64(b)/kb)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func fmtSpeed(bytesPerSec float64) string {
	if bytesPerSec <= 0 {
		return "-- MB/s"
	}
	return fmt.Sprintf("%.1f MB/s", bytesPerSec/(1<<20))
}

func fmtETA(bytes, total int64, speed float64) string {
	if speed <= 0 || total <= 0 || bytes >= total {
		return "--"
	}
	secs := float64(total-bytes) / speed
	if secs > 99*3600 {
		return "--"
	}
	d := time.Duration(secs) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func (p ModelsPage) handleDownloadEvent(ev downloadmgr.Event) (tea.Model, tea.Cmd) {
	if p.dlManager != nil {
		p.updateRates(p.dlManager.Snapshot())
	}
	// Terminal download states also raise a tab attention badge so the user
	// notices the background completion/failure from any other tab.
	attention := func() tea.Msg { return TabAttentionMsg{Page: AttentionModels} }
	switch ev.State.Status {
	case downloadmgr.StatusCompleted:
		p, fc := p.withFlash("downloaded: " + ev.State.Spec.Filename)
		next, scanCmd := p.beginRescan(false)
		return next, tea.Batch(fc, scanCmd, attention)
	case downloadmgr.StatusFailed:
		msg := "download failed"
		if ev.State.Err != nil {
			msg += ": " + ev.State.Err.Error()
		}
		next, fc := p.withFlashError(msg)
		return next, tea.Batch(fc, attention)
	default:
		return p, nil
	}
}

// pendingDownload parks a file-picker selection while the destination chooser
// is open (only used when more than one search path is configured).
type pendingDownload struct {
	files      []string
	isSnapshot bool
	repoID     string
}

// pathChooser is the inline modal that asks which configured search path a
// download should land in.
type pathChooser struct {
	paths  []string
	cursor int
}

// startSelectedDownloads captures the file-picker selection and either starts
// the downloads right away (single search path) or opens the destination
// chooser (multiple search paths).
func (p ModelsPage) startSelectedDownloads() (tea.Model, tea.Cmd) {
	if p.hfFilePicker == nil {
		return p, nil
	}
	pd := pendingDownload{
		files:      p.hfFilePicker.SelectedFiles(),
		isSnapshot: p.hfFilePicker.IsSnapshot(),
		repoID:     p.pendingRepoID,
	}
	p.hfFilePicker = nil
	p.pendingRepoID = ""

	if p.dlManager == nil {
		return p.withFlashError("download manager not wired")
	}
	if len(p.paths) == 0 {
		return p.withFlashError(downloadmgr.ErrNoSearchPath.Error())
	}
	if len(pd.files) == 0 {
		return p.withFlash("no files selected")
	}
	if len(p.paths) > 1 {
		p.pendingDL = &pd
		p.pathChooser = &pathChooser{paths: p.paths}
		return p, nil
	}
	return p.startDownloads(pd, p.paths[0])
}

// updatePathChooser owns keystrokes while the destination chooser is open.
func (p ModelsPage) updatePathChooser(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.pathChooser = nil
		p.pendingDL = nil
		return p, nil
	case "up", "k":
		if p.pathChooser.cursor > 0 {
			p.pathChooser.cursor--
		}
		return p, nil
	case "down", "j":
		if p.pathChooser.cursor < len(p.pathChooser.paths)-1 {
			p.pathChooser.cursor++
		}
		return p, nil
	case "enter":
		chosen := p.pathChooser.paths[p.pathChooser.cursor]
		pd := *p.pendingDL
		p.pathChooser = nil
		p.pendingDL = nil
		return p.startDownloads(pd, chosen)
	}
	return p, nil
}

func (p ModelsPage) renderPathChooser() string {
	lines := []string{theme.Title.Render("Download into which path?")}
	for i, path := range p.pathChooser.paths {
		prefix := "  "
		label := path
		if i == p.pathChooser.cursor {
			prefix = "> "
			label = theme.OK.Render(label)
		}
		lines = append(lines, prefix+label)
	}
	lines = append(lines, "", theme.Subtitle.Render("[↑/↓] move  [enter] select  [esc] cancel"))
	return strings.Join(lines, "\n")
}

// startDownloads queues every file in pd under searchPath, switching to the
// Downloads section so progress is visible immediately.
func (p ModelsPage) startDownloads(pd pendingDownload, searchPath string) (tea.Model, tea.Cmd) {
	started := 0
	var cmds []tea.Cmd
	for _, file := range pd.files {
		destDir, destFile, err := downloadmgr.ResolveDest(searchPath, pd.repoID, file, pd.isSnapshot)
		if errors.Is(err, downloadmgr.ErrAlreadyExists) {
			var cmd tea.Cmd
			p.flash, cmd = p.flash.Set("already exists: " + file)
			cmds = append(cmds, cmd)
			continue
		}
		if err != nil {
			var cmd tea.Cmd
			p.flash, cmd = p.flash.SetError(err.Error())
			cmds = append(cmds, cmd)
			continue
		}
		url := p.hfClient.DownloadURL(pd.repoID, file)
		_, err = p.dlManager.Start(downloadmgr.Spec{
			RepoID:     pd.repoID,
			Filename:   file,
			URL:        url,
			DestDir:    destDir,
			DestFile:   destFile,
			IsSnapshot: pd.isSnapshot,
		})
		if err != nil {
			var cmd tea.Cmd
			p.flash, cmd = p.flash.SetError(err.Error())
			cmds = append(cmds, cmd)
			continue
		}
		started++
	}

	if started > 0 {
		// Surface progress immediately and reset any prior "clear done" filter.
		p.subView = mvDownloads
		p.hideDone = false
		p.dlFocus = 0
		cmds = append(cmds, p.tickCmd())
	}
	p, fc := p.withFlash(fmt.Sprintf("starting %d download(s)", started))
	cmds = append(cmds, fc)
	return p, tea.Batch(cmds...)
}
