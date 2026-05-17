package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// DownloadStateSnapshotter is the minimal interface for observing download states.
type DownloadStateSnapshotter interface {
	Snapshot() []DownloadState
}

// DownloadState is a point-in-time snapshot of a single download.
type DownloadState struct {
	ID     string
	Name   string
	Status string
	Bytes  int64
	Total  int64
	Err    string
}

type DownloadCancelMsg struct {
	ID string
}

// DownloadProgress renders active download progress lines.
type DownloadProgress struct {
	snapshotter DownloadStateSnapshotter
	focusIndex  int
	width       int
	visible     bool
}

// NewDownloadProgress creates a new progress component.
func NewDownloadProgress(snapshotter DownloadStateSnapshotter, width int) *DownloadProgress {
	return &DownloadProgress{
		snapshotter: snapshotter,
		width:       width,
	}
}

// Init implements tea.Model.
func (p *DownloadProgress) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (p *DownloadProgress) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up":
			if p.visible && p.focusIndex > 0 {
				p.focusIndex--
			}
		case "down":
			if p.visible {
				snap := p.snapshotter.Snapshot()
				if p.focusIndex < len(snap)-1 {
					p.focusIndex++
				}
			}
		case "x":
			if p.visible {
				snap := p.snapshotter.Snapshot()
				if p.focusIndex >= 0 && p.focusIndex < len(snap) {
					state := snap[p.focusIndex]
					if state.Status == "active" {
						return func() tea.Msg {
							return DownloadCancelMsg{ID: state.ID}
						}
					}
				}
			}
		case "d":
			p.visible = !p.visible
			if !p.visible {
				p.focusIndex = 0
			}
		default:
			return nil
		}
	}
	return nil
}

// View implements tea.Model.
func (p *DownloadProgress) View() string {
	if !p.visible {
		return ""
	}

	snap := p.snapshotter.Snapshot()
	if len(snap) == 0 {
		return ""
	}

	var lines []string
	for i, state := range snap {
		line := formatProgress(state)
		if i == p.focusIndex {
			line = "> " + line
		} else {
			line = "  " + line
		}
		if len(line) > p.width {
			line = line[:p.width]
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func formatProgress(state DownloadState) string {
	var percent int64
	if state.Total > 0 {
		percent = state.Bytes * 100 / state.Total
	}

	line := fmt.Sprintf("%s [%s] %s/%s (%d%%)",
		state.Name,
		state.Status,
		humanBytes(state.Bytes),
		humanBytes(state.Total),
		percent,
	)

	if state.Status == "failed" && state.Err != "" {
		line += ": " + state.Err
	}

	return line
}

func humanBytes(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)

	switch {
	case b >= TB:
		return fmt.Sprintf("%.2fTB", float64(b)/TB)
	case b >= GB:
		return fmt.Sprintf("%.2fGB", float64(b)/GB)
	case b >= MB:
		return fmt.Sprintf("%.2fMB", float64(b)/MB)
	case b >= KB:
		return fmt.Sprintf("%.2fKB", float64(b)/KB)
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// SetWidth updates the component width.
func (p *DownloadProgress) SetWidth(w int) {
	p.width = w
}

// IsVisible reports whether the progress footer should be shown.
func (p *DownloadProgress) IsVisible() bool {
	return p.visible
}

// IsFocusVisible reports whether the progress footer has keyboard focus.
func (p *DownloadProgress) IsFocusVisible() bool {
	return p.visible && p.focusIndex >= 0
}
