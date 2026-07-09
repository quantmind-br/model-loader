package components

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// HFFileListMsg carries the async file-listing response back into Update.
type HFFileListMsg struct {
	Files []FileItem
	Err   error
}

type hfFileListMsg = HFFileListMsg

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
	spinner    spinner.Model
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
		spinner:    NewLoadingSpinner(),
		width:      width,
		height:     height,
	}
}

// Init implements tea.Model.
func (p *HFFilePicker) Init() tea.Cmd {
	return func() tea.Msg {
		info, err := p.lister.RepoInfo(context.Background(), p.repoID)
		if err != nil {
			return hfFileListMsg{Err: err}
		}
		files := make([]FileItem, 0, len(info.Siblings))
		for _, s := range info.Siblings {
			files = append(files, FileItem{
				RFilename: s.RFilename,
				Size:      s.Size,
			})
		}
		return hfFileListMsg{Files: files}
	}
}

// Update implements tea.Model.
func (p *HFFilePicker) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if p.loading {
			var cmd tea.Cmd
			p.spinner, cmd = p.spinner.Update(msg)
			return cmd
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			p.active = false
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.files)-1 {
				p.cursor++
			}
		case "pgup":
			p.cursor = max(0, p.cursor-10)
		case "pgdown":
			p.cursor = min(len(p.files)-1, p.cursor+10)
		case "home", "g":
			p.cursor = 0
		case "end", "G":
			p.cursor = max(0, len(p.files)-1)
		case " ":
			if !p.isSnapshot && p.cursor >= 0 && p.cursor < len(p.files) {
				p.files[p.cursor].Selected = !p.files[p.cursor].Selected
			}
		case "enter":
			p.active = false
		}
	case hfFileListMsg:
		p.loading = false
		if msg.Err != nil {
			p.err = msg.Err
			return nil
		}
		if p.isSnapshot {
			p.files = msg.Files
			for i := range p.files {
				p.files[i].Selected = true
			}
		} else {
			filtered := make([]FileItem, 0, len(msg.Files))
			for _, f := range msg.Files {
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
		return LoadingLine(p.spinner, "Loading files", 0)
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

	const (
		hfFilePrefixWidth = 5 // cursor(1) + checked(3) + space(1)
		hfFileSizeWidth   = 12
	)
	sizeStyle := lipgloss.NewStyle().Width(hfFileSizeWidth).Align(lipgloss.Right)
	for i, f := range p.files {
		cursor := " "
		if i == p.cursor {
			cursor = ">"
		}
		checked := "[ ]"
		if f.Selected {
			checked = "[x]"
		}
		size := sizeStyle.Render(humanBytes(f.Size))
		name := truncatePath(f.RFilename, p.width-hfFilePrefixWidth-hfFileSizeWidth-1)
		b.WriteString(fmt.Sprintf("%s%s %s %s\n", cursor, checked, name, size))
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

// IsSnapshot reports whether picker will download whole repository snapshot.
func (p *HFFilePicker) IsSnapshot() bool {
	return p.isSnapshot
}
