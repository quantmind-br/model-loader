package components

import (
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
	return nil
}

// View implements tea.Model.
func (p *DownloadProgress) View() string {
	return "Downloads: not implemented"
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
