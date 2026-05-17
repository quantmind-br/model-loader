package components

import (
	"context"
	"fmt"
	"strings"

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

type hfFileListMsg struct {
	files []FileItem
	err   error
}

// HFFilePicker is an overlay for selecting files from a Hugging Face repo.
type HFFilePicker struct {
	lister     HFFileLister
	repoID     string
	isSnapshot bool
	active     bool
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
		active:     true,
		loading:    true,
		width:      width,
		height:     height,
	}
}

// Init implements tea.Model.
func (p *HFFilePicker) Init() tea.Cmd {
	return func() tea.Msg {
		info, err := p.lister.RepoInfo(context.Background(), p.repoID)
		if err != nil {
			return hfFileListMsg{err: err}
		}
		files := make([]FileItem, 0, len(info.Siblings))
		for _, s := range info.Siblings {
			files = append(files, FileItem{
				RFilename: s.RFilename,
				Size:      s.Size,
			})
		}
		return hfFileListMsg{files: files}
	}
}

// Update implements tea.Model.
func (p *HFFilePicker) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			p.active = false
		case "up":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down":
			if p.cursor < len(p.files)-1 {
				p.cursor++
			}
		case " ":
			if !p.isSnapshot && p.cursor >= 0 && p.cursor < len(p.files) {
				p.files[p.cursor].Selected = !p.files[p.cursor].Selected
			}
		case "enter":
			p.active = false
		}
	case hfFileListMsg:
		p.loading = false
		if msg.err != nil {
			p.err = msg.err
			return nil
		}
		if p.isSnapshot {
			p.files = msg.files
			for i := range p.files {
				p.files[i].Selected = true
			}
		} else {
			filtered := make([]FileItem, 0, len(msg.files))
			for _, f := range msg.files {
				if strings.HasSuffix(f.RFilename, ".gguf") {
					filtered = append(filtered, f)
				}
			}
			p.files = filtered
		}
	}
	return nil
}

// View implements tea.Model.
func (p *HFFilePicker) View() string {
	if p.loading {
		return "Loading files..."
	}
	if p.err != nil {
		return fmt.Sprintf("Error: %v", p.err)
	}

	var b strings.Builder
	if p.isSnapshot {
		b.WriteString("Snapshot mode\n")
	} else {
		b.WriteString("Select .gguf files\n")
	}
	b.WriteString("\n")

	for i, f := range p.files {
		cursor := " "
		if i == p.cursor {
			cursor = ">"
		}
		checked := "[ ]"
		if f.Selected {
			checked = "[x]"
		}
		size := humanizeBytes(f.Size)
		b.WriteString(fmt.Sprintf("%s %s %s (%s)\n", cursor, checked, f.RFilename, size))
	}

	b.WriteString("\n")
	if p.isSnapshot {
		b.WriteString("enter: confirm, esc: cancel")
	} else {
		b.WriteString("space: toggle, enter: confirm, esc: cancel")
	}
	return b.String()
}

// SelectedFiles returns the selected file names.
func (p *HFFilePicker) SelectedFiles() []string {
	if p.isSnapshot {
		result := make([]string, 0, len(p.files))
		for _, f := range p.files {
			result = append(result, f.RFilename)
		}
		return result
	}
	result := make([]string, 0, len(p.files))
	for _, f := range p.files {
		if f.Selected {
			result = append(result, f.RFilename)
		}
	}
	return result
}

// SetSize updates the picker dimensions.
func (p *HFFilePicker) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// IsActive reports whether the picker is open.
func (p *HFFilePicker) IsActive() bool {
	return p.active
}

func humanizeBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
