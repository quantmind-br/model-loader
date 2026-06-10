package pages

import (
	"os"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// clipboardWriter is overridable in tests to bypass the system clipboard
// (which may be unavailable on CI without a display server).
var clipboardWriter = clipboard.WriteAll

var fileRemover = os.Remove

// actionOption is one row in the inline action selector overlay.
type actionOption struct{ label, value string }

// actionMenu is the inline modal used for "what should we do with this
// model file?". Replaces the earlier huh.NewForm wrapping a single
// Select that did not reliably reach huh.StateCompleted on a single
// Enter press.
type actionMenu struct {
	title      string
	options    []actionOption
	cursor     int
	targetPath string // GGUF path the menu acts on
	stage      actionStage
}

type actionStage int

const (
	actionStageRoot actionStage = iota
)

func (p ModelsPage) updateDeleteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	p.deleteConfirm, cmd = p.deleteConfirm.Update(msg)
	return p, cmd
}

// askClearDone arms the Downloads "clear all finished" confirm. The onYes
// callback emits downloadClearConfirmedMsg so the actual ClearTerminal runs in
// Update via performDownloadClear (DESTRUCT-02).
func (p ModelsPage) askClearDone() (tea.Model, tea.Cmd) {
	p.dlFocus = 0
	if p.dlManager == nil {
		return p, nil
	}
	p.clearDoneConfirm = components.NewConfirm(
		"Clear all finished downloads?",
		nil,
		func(any) tea.Cmd {
			return func() tea.Msg { return downloadClearConfirmedMsg{} }
		},
		"Clear",
		"Cancel",
	)
	return p, p.clearDoneConfirm.Init()
}

func (p ModelsPage) updateClearDoneConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	p.clearDoneConfirm, cmd = p.clearDoneConfirm.Update(msg)
	return p, cmd
}

// updateActionMenu owns the inline action selector while it is on
// screen. Up/Down move the cursor, Enter commits the highlighted option,
// Esc cancels.
func (p ModelsPage) updateActionMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.action = nil
		return p, nil
	case "up", "k":
		if p.action.cursor > 0 {
			p.action.cursor--
		}
		return p, nil
	case "down", "j":
		if p.action.cursor < len(p.action.options)-1 {
			p.action.cursor++
		}
		return p, nil
	case "enter":
		opt := p.action.options[p.action.cursor]
		return p.commitRootAction(opt.value, p.action.targetPath)
	}
	return p, nil
}

// commitRootAction handles the choice from the first action menu.
func (p ModelsPage) commitRootAction(choice, path string) (tea.Model, tea.Cmd) {
	switch choice {
	case "new":
		p.action = nil
		return p, func() tea.Msg { return UseInNewProfileMsg{Path: path} }
	case "reveal":
		p.action = nil
		if err := clipboardWriter(path); err != nil {
			p, fc := p.withFlashError("clipboard error: " + err.Error())
			return p, fc
		}
		p, fc := p.withFlash("path copied to clipboard")
		return p, fc
	case "existing":
		p.action = nil
		if p.store == nil {
			p, fc := p.withFlashError("profile store not wired")
			return p, fc
		}
		profiles, err := p.store.List()
		if err != nil {
			p, fc := p.withFlashError("load profiles: " + err.Error())
			return p, fc
		}
		if len(profiles) == 0 {
			p, fc := p.withFlashError("no existing profiles to update")
			return p, fc
		}
		picker := components.NewProfilePicker(profiles)
		p.profilePicker = &picker
		p.profilePickerTargetPath = path
		return p, nil
	case "delete":
		p.action = nil
		selectedName := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			selectedName = path[idx+1:]
		}
		p.deleteConfirm = components.NewConfirm(
			"Delete "+selectedName+" from disk?",
			path,
			func(payload any) tea.Cmd {
				ppath, _ := payload.(string)
				return func() tea.Msg { return modelDeleteConfirmedMsg{path: ppath} }
			},
			"Delete",
			"Cancel",
		)
		return p, p.deleteConfirm.Init()
	}
	p.action = nil
	return p, nil
}

func (p ModelsPage) performDelete(path string) (tea.Model, tea.Cmd) {
	if err := fileRemover(path); err != nil {
		p, fc := p.withFlashError("delete failed: " + err.Error())
		return p, fc
	}
	filtered := p.files[:0]
	for _, f := range p.files {
		if f.Path != path {
			filtered = append(filtered, f)
		}
	}
	p.files = filtered
	p.refreshRows()
	p, fc := p.withFlash("deleted " + path)
	return p, fc
}

func (p ModelsPage) openInfoPanel() (tea.Model, tea.Cmd) {
	visible := p.visibleFiles()
	idx := p.table.Cursor()
	if idx < 0 || idx >= len(visible) {
		return p, nil
	}
	mf := visible[idx]
	var usedBy []components.ProfileRef
	if p.store != nil {
		profiles, _ := p.store.List()
		for _, pr := range profiles {
			if pr.Model == mf.Path {
				usedBy = append(usedBy, components.ProfileRef{ID: pr.ID, Name: pr.Name})
			}
		}
	}
	p.infoPanel = &components.InfoPanel{
		Filename:       mf.Name,
		Path:           mf.Path,
		SizeOnDisk:     mf.SizeBytes,
		ParameterCount: mf.Params,
		SizeLabel:      "",
		Quantization:   mf.Quant,
		Architecture:   mf.Architecture,
		BlockCount:     mf.BlockCount,
		UsedByProfiles: usedBy,
	}
	p.infoPanelUsedBy = usedBy
	// The table now shares the row with the info panel — reflow columns so
	// they fit the narrower pane (RENDER-01).
	p.resizeColumns(p.width)
	return p, nil
}

// openActionMenuForSelection builds the per-row action menu for the
// currently selected model file, returning the page unchanged when the
// table is empty or the cursor is out of range.
func (p ModelsPage) openActionMenuForSelection() (tea.Model, tea.Cmd) {
	if len(p.table.Rows()) == 0 {
		return p, nil
	}
	idx := p.table.Cursor()
	if idx < 0 {
		return p, nil
	}
	// Map row idx to file via filtered ordering. Recompute filtered
	// list to match what's displayed.
	visible := p.visibleFiles()
	if idx >= len(visible) {
		return p, nil
	}
	selected := visible[idx]
	opts := []actionOption{
		{label: "Use in new profile", value: "new"},
	}
	if p.store != nil {
		opts = append(opts, actionOption{label: "Use in existing profile", value: "existing"})
	}
	opts = append(opts, actionOption{label: "Copy path to clipboard", value: "reveal"})
	opts = append(opts, actionOption{label: "Delete file", value: "delete"})
	p.action = &actionMenu{
		title:      "Action for " + selected.Name,
		options:    opts,
		targetPath: selected.Path,
		stage:      actionStageRoot,
	}
	return p, nil
}

// renderActionMenu returns the action modal placed on a full p.width × p.height
// canvas. The surrounding spaces from lipgloss.Place are what make the modal
// opaque — passing height=0 would skip Place and let table rows leak around
// the box frame (F-03 audit regression).
func (p ModelsPage) renderActionMenu() string {
	return components.Modal("", p.renderActionMenuContent(), p.width, p.height)
}

func (p ModelsPage) renderActionMenuContent() string {
	lines := []string{theme.Title.Render(p.action.title)}
	for i, opt := range p.action.options {
		prefix := "  "
		label := opt.label
		if i == p.action.cursor {
			prefix = "> "
			label = theme.OK.Render(label)
		}
		lines = append(lines, prefix+label)
	}
	lines = append(lines, "", theme.Subtitle.Render("[↑/↓] move  [enter] select  [esc] cancel"))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}
