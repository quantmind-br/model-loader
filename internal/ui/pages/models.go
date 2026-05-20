// Package pages holds tab page implementations.
package pages

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/quantmind-br/model-loader/internal/service/hfhub"
	"github.com/quantmind-br/model-loader/internal/service/modelscanner"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// ModelsPage browses GGUF files discovered by ModelScanner.
type ModelsPage struct {
	scanner modelscanner.Scanner
	paths   []string
	store   profilestore.Store
	cancel  context.CancelFunc
	scanID  int // epoch; bumped each rescan to discard stale events

	files     []domain.ModelFile
	statusMap map[string]pathStatus

	table      table.Model
	width      int
	height     int
	nameColW   int // flexed Name column width, recomputed on resize (RENDER-01)
	pathColW   int // flexed Path column width, recomputed on resize (RENDER-01)
	filter     string
	filterMode bool
	flash      components.Flash

	action        *actionMenu
	deleteConfirm components.Confirm

	profilePicker           *components.ProfilePicker
	profilePickerTargetPath string

	// Hugging Face integration. Wired by builders (T13); nil-safe until then.
	hfClient       *hfhub.Client
	dlManager      *downloadmgr.Manager
	hfSearch       *components.HFSearchPicker
	hfFilePicker   *components.HFFilePicker
	downloads      *components.DownloadProgress
	downloadEvents <-chan downloadmgr.Event
	searchEpoch    int    // bumped per search keystroke for debounce
	pendingRepoID  string // carried between RepoInfo lookup and file picker open

	keys modelsKeyMap

	infoPanel       *components.InfoPanel
	infoPanelUsedBy []components.ProfileRef
}

func (p ModelsPage) withFlash(msg string) (ModelsPage, tea.Cmd) {
	var cmd tea.Cmd
	p.flash, cmd = flashSuccess(p.flash, msg)
	return p, cmd
}

func (p ModelsPage) withFlashError(msg string) (ModelsPage, tea.Cmd) {
	var cmd tea.Cmd
	p.flash, cmd = flashError(p.flash, msg)
	return p, cmd
}

// NewModelsPage builds a page wired to a Scanner and configured search paths.
func NewModelsPage(scanner modelscanner.Scanner, paths []string) ModelsPage {
	cols := []table.Column{
		{Title: "Name", Width: 36},
		{Title: "Size", Width: 10},
		{Title: "Quant", Width: 10},
		{Title: "Params", Width: 8},
		{Title: "Path", Width: 40},
	}
	t := table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(12))

	statusMap := make(map[string]pathStatus, len(paths))
	for _, p := range paths {
		statusMap[p] = pathStatus{state: "scanning"}
	}
	return ModelsPage{
		scanner:   scanner,
		paths:     paths,
		statusMap: statusMap,
		table:     t,
		nameColW:  36,
		pathColW:  40,
		keys:      defaultModelsKeys(),
		flash:     components.NewFlash("models"),
	}
}

// WithProfileStore enables the "Use in existing profile" action by
// giving the page access to the profile store. Without it, that option
// is hidden from the action menu.
func (p ModelsPage) WithProfileStore(store profilestore.Store) ModelsPage {
	p.store = store
	return p
}

// WithHFClient injects a Hugging Face Hub client so the page can offer
// remote model search ("s" key in T14). Without it, the HF entry points
// stay disabled.
func (p ModelsPage) WithHFClient(c *hfhub.Client) ModelsPage {
	p.hfClient = c
	return p
}

// WithDownloadManager injects the download manager and subscribes the
// page to its event channel. Without it, download UI stays inert.
func (p ModelsPage) WithDownloadManager(m *downloadmgr.Manager) ModelsPage {
	p.dlManager = m
	if m != nil {
		p.downloadEvents = m.Subscribe()
		p.downloads = components.NewDownloadProgress(downloadSnapshotAdapter{manager: m}, p.width)
	}
	return p
}

func (p ModelsPage) Init() tea.Cmd {
	return startScanCmd(p.scanner, p.paths, p.scanID)
}

// Reload implements the ui.Reloader contract. RootModel calls this on
// tab activation; we re-enter the rescan path through Update so the
// state mutations happen in a single place (beginRescan).
func (p ModelsPage) Reload() tea.Cmd {
	return func() tea.Msg { return modelsReloadMsg{} }
}

func (p ModelsPage) View() string {
	if p.profilePicker != nil {
		return p.profilePicker.View()
	}
	if p.deleteConfirm.Active() {
		return p.deleteConfirm.View()
	}
	if p.action != nil {
		return p.renderActionMenu()
	}
	return p.regularBodyView()
}

func (p ModelsPage) regularBodyView() string {
	header := theme.Title.Render("Models")
	statusLine := p.renderStatus()
	filterLine := ""
	if p.filterMode || p.filter != "" {
		filterLine = theme.Subtitle.Render(fmt.Sprintf("filter: %q", p.filter))
	}
	footer := p.flash.View()
	var content string
	if len(p.files) == 0 && (len(p.paths) == 0 || p.hasScannedRoot()) {
		emptyMsg := components.EmptyState("No .gguf files in configured search paths", "Press [R] to rescan, or edit ~/.config/model-loader/config.toml")
		content = lipgloss.JoinVertical(lipgloss.Left, header, statusLine, emptyMsg, filterLine, footer)
	} else if len(p.visibleFiles()) == 0 && p.filter != "" {
		emptyMsg := components.EmptyState("No models match the current filter", "Press [esc] to clear filter, or [/] to edit filter")
		content = lipgloss.JoinVertical(lipgloss.Left, header, statusLine, emptyMsg, filterLine, footer)
	} else {
		content = lipgloss.JoinVertical(lipgloss.Left, header, statusLine, p.table.View(), filterLine, footer)
	}
	if p.hfSearch != nil && p.hfSearch.IsActive() {
		content = p.hfSearch.View()
	}
	if p.hfFilePicker != nil && p.hfFilePicker.IsActive() {
		content = p.hfFilePicker.View()
	}
	if p.downloads != nil && p.downloads.IsVisible() {
		progressView := p.downloads.View()
		if progressView != "" {
			content = content + "\n" + progressView
		}
	}
	if p.infoPanel != nil {
		w := p.width / 3
		if w > 60 {
			w = 60
		}
		if w < 30 {
			w = 30
		}
		panel := p.infoPanel.Render(w)
		content = lipgloss.JoinHorizontal(lipgloss.Top, content, panel)
	}
	return content
}

// Hints implements ui.HintProvider for the Models tab.
func (p ModelsPage) Hints() string {
	if p.profilePicker != nil {
		return "[↑↓] move  [enter] select  [esc] cancel"
	}
	if p.deleteConfirm.Active() {
		return "[enter] confirm  [esc] cancel"
	}
	if p.action != nil {
		return "[↑↓] move  [enter] select  [esc] cancel"
	}
	if p.filterMode {
		return "[type] filter  [esc] clear"
	}
	hints := "[/] filter  [R] rescan  [s] search HF  [enter] actions  [i] info  [esc] clear"
	if p.downloads != nil && p.downloads.IsVisible() {
		hints += "  [x] cancel dl  [r] resume"
	}
	if p.infoPanel != nil {
		hints = "[→/g] sizing  [esc] close info"
	}
	return hints
}
