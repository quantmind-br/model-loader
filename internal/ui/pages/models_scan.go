package pages

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/modelscanner"
	"github.com/quantmind-br/model-loader/internal/ui/internal/filter"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// pathStatus tracks per-root scan progress shown above the table.
type pathStatus struct {
	state string // "scanning" | "scanned" | "error"
	count int
	err   string
}

// beginRescan cancels any in-flight scan, bumps the scan epoch, resets
// per-root status to "scanning", and returns the page plus a Cmd that
// kicks off a fresh scan. When showFlash is true a "rescan started"
// message is shown — used for the manual R key, suppressed for the
// silent reload-on-focus path.
func (p ModelsPage) beginRescan(showFlash bool) (ModelsPage, tea.Cmd) {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.scanID++
	p.files = nil
	for _, root := range p.paths {
		p.statusMap[root] = pathStatus{state: "scanning"}
	}
	p.refreshRows()
	cmds := []tea.Cmd{startScanCmd(p.scanner, p.paths, p.scanID)}
	if showFlash {
		var fc tea.Cmd
		p, fc = p.withFlash("rescan started")
		cmds = append(cmds, fc)
	}
	return p, tea.Batch(cmds...)
}

// startScanCmd builds a Cmd that creates ctx+cancel, kicks off the
// scanner, and delivers the channel via scanStartedMsg. The Cmd's
// closure owns the cancel until Update captures it.
func startScanCmd(scanner modelscanner.Scanner, paths []string, scanID int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := scanner.Scan(ctx, paths)
		if err != nil {
			cancel()
			return scanStartedMsg{scanID: scanID, err: err}
		}
		return scanStartedMsg{scanID: scanID, ch: ch, cancel: cancel}
	}
}

func waitForScanEvent(ch <-chan domain.ScanEvent, scanID int) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return scanChannelClosedMsg{scanID: scanID}
		}
		return scanEventMsg{scanID: scanID, ch: ch, evt: evt}
	}
}

func (p ModelsPage) handleScanEvent(evt domain.ScanEvent) (tea.Model, tea.Cmd) {
	switch evt.Type {
	case domain.ScanEventFile:
		if evt.File != nil {
			p.files = append(p.files, *evt.File)
			p.refreshRows()
		}
	case domain.ScanEventProgress:
		st := p.statusMap[evt.Root]
		st.count = evt.Count
		st.state = "scanned"
		p.statusMap[evt.Root] = st
	case domain.ScanEventError:
		st := p.statusMap[evt.Root]
		st.state = "error"
		if evt.Error != nil {
			if errors.Is(evt.Error, fs.ErrNotExist) {
				st.err = "path not found"
			} else {
				st.err = evt.Error.Error()
			}
		}
		p.statusMap[evt.Root] = st
	case domain.ScanEventDone:
		// Channel will close right after; nothing to do.
	}
	return p, nil
}

func (p ModelsPage) visibleFiles() []domain.ModelFile {
	files := filter.ContainsFold(p.files, p.filter, func(f domain.ModelFile) string { return f.Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files
}

// refreshRows rebuilds table rows from p.files honoring the current
// filter. Sorted by name for stable display. Name/Path cells are truncated
// to the flexed column widths so the row never exceeds the terminal width
// and wraps onto a second line (RENDER-01).
func (p *ModelsPage) refreshRows() {
	nameW, pathW := p.nameColW, p.pathColW
	if nameW <= 0 {
		nameW = 36
	}
	if pathW <= 0 {
		pathW = 40
	}
	files := p.visibleFiles()
	rows := make([]table.Row, 0, len(files))
	for _, f := range files {
		rows = append(rows, table.Row{
			truncate(f.Name, nameW),
			humanSize(f.SizeBytes),
			f.Quant,
			f.Params,
			truncate(f.Path, pathW),
		})
	}
	p.table.SetRows(rows)
}

// resizeColumns recomputes the Name/Path column widths so the table fits
// within the available terminal width instead of overflowing with a fixed
// 104-char layout. Size/Quant/Params stay fixed; Name and Path share the
// remaining width. Rows are re-truncated to match (RENDER-01).
func (p *ModelsPage) resizeColumns(width int) {
	if width <= 0 {
		return
	}
	const sizeW, quantW, paramsW = 10, 10, 8
	avail := width
	// Leave room for the info panel when it's open (rendered side-by-side).
	if p.infoPanel != nil {
		panelW := width / 3
		if panelW > 60 {
			panelW = 60
		}
		if panelW < 30 {
			panelW = 30
		}
		avail -= panelW
	}
	// Reserve fixed columns plus per-column cell padding. bubbletea's table
	// cell style adds Padding(0,1) = 2 cols per column (5 cols = 10); reserve
	// that plus a small safety margin so a truncated Path never wraps (RENDER-01).
	flex := avail - (sizeW + quantW + paramsW) - 12
	if flex < 32 {
		flex = 32
	}
	nameW := flex * 9 / 20 // ~45% to Name
	if nameW < 12 {
		nameW = 12
	}
	pathW := flex - nameW
	if pathW < 12 {
		pathW = 12
	}
	p.nameColW, p.pathColW = nameW, pathW
	p.table.SetColumns([]table.Column{
		{Title: "Name", Width: nameW},
		{Title: "Size", Width: sizeW},
		{Title: "Quant", Width: quantW},
		{Title: "Params", Width: paramsW},
		{Title: "Path", Width: pathW},
	})
	p.refreshRows()
}

// isScanning reports whether any configured root is still being scanned.
// Used to guard duplicate manual rescans.
func (p ModelsPage) isScanning() bool {
	for _, st := range p.statusMap {
		if st.state == "scanning" {
			return true
		}
	}
	return false
}

// hasScannedRoot reports whether at least one configured root has finished
// its initial scan. Empty-state copy only renders once we know the scan is
// complete — otherwise the user might think their models are missing
// while the scanner is still walking the filesystem.
func (p ModelsPage) hasScannedRoot() bool {
	for _, st := range p.statusMap {
		if st.state == "scanned" {
			return true
		}
	}
	return false
}

// humanSize formats bytes as "X.YG" / "X.YM".
func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

func (p ModelsPage) renderStatus() string {
	if len(p.paths) == 0 {
		return theme.Subtitle.Render("no search paths configured")
	}
	parts := make([]string, 0, len(p.paths))
	for _, root := range p.paths {
		display := truncFront(root, 40)
		st := p.statusMap[root]
		var label string
		switch st.state {
		case "scanning":
			label = theme.Subtitle.Render(fmt.Sprintf("%s [scanning]", display))
		case "scanned":
			label = theme.OK.Render(fmt.Sprintf("%s [%d]", display, st.count))
		case "error":
			label = theme.Error.Render(fmt.Sprintf("%s [error: %s]", display, truncate(st.err, 30)))
		default:
			label = theme.Subtitle.Render(display)
		}
		parts = append(parts, label)
	}
	// If the joined width fits, single-line — otherwise wrap one label per
	// line so labels remain legible on narrow terminals.
	const sep = "  "
	width := 0
	for i, lbl := range parts {
		width += lipgloss.Width(lbl)
		if i > 0 {
			width += len(sep)
		}
	}
	if p.width > 0 && width > p.width-2 {
		return strings.Join(parts, "\n")
	}
	return strings.Join(parts, sep)
}

// truncFront returns s with leading characters replaced by "…" when its
// length exceeds n. Preserves the tail because that's the discriminator
// for similar-looking root paths.
func truncFront(s string, n int) string {
	if n <= 1 || len(s) <= n {
		return s
	}
	return "…" + s[len(s)-(n-1):]
}
