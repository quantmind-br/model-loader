package components

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// HFFileLister is the minimal interface needed to list files in a HF repo.
type HFFileLister interface {
	RepoInfo(ctx context.Context, repoID string) (*RepoInfo, error)
}

// RepoInfo mirrors hfhub.RepoInfo locally.
type RepoInfo struct {
	ID       string
	Siblings []Sibling
	Tags     []string
}

// Sibling mirrors hfhub.Sibling locally.
type Sibling struct {
	RFilename string
	Size      int64
}

// FileItem represents a file in the picker.
type FileItem struct {
	RFilename string
	Size      int64
	Selected  bool
}

// HFFilePicker is an overlay for selecting files from a Hugging Face repo.
type HFFilePicker struct {
	lister     HFFileLister
	repoID     string
	isSnapshot bool
	files      []FileItem
	cursor     int
	width      int
	height     int
	loading    bool
	err        error
}

// NewHFFilePicker creates a new file picker.
func NewHFFilePicker(lister HFFileLister, repoID string, isSnapshot bool, width, height int) *HFFilePicker {
	return &HFFilePicker{
		lister:     lister,
		repoID:     repoID,
		isSnapshot: isSnapshot,
		width:      width,
		height:     height,
	}
}

// Init implements tea.Model.
func (p *HFFilePicker) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (p *HFFilePicker) Update(msg tea.Msg) tea.Cmd {
	return nil
}

// View implements tea.Model.
func (p *HFFilePicker) View() string {
	return "HF File Picker (not implemented)"
}

// SelectedFiles returns the selected file names.
func (p *HFFilePicker) SelectedFiles() []string {
	return nil
}

// SetSize updates the picker dimensions.
func (p *HFFilePicker) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// IsActive reports whether the picker is open.
func (p *HFFilePicker) IsActive() bool {
	return true
}
