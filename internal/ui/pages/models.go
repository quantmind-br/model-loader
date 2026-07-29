// Package pages holds tab page implementations.
package pages

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/config"
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
	// pasteGuard suppresses Enter-after-filter actions for one frame so
	// bracketed-paste newlines don't leak through the filter to the
	// action menu (TUI_AUDIT I-04).
	pasteGuard bool
	flash      components.Flash

	// subView selects which section the page renders: the local model
	// Library, the Downloads queue, or HuggingFace Discover.
	subView modelsSubView

	action        *actionMenu
	deleteConfirm components.Confirm
	// clearDoneConfirm gates the Downloads section's [C] clear-all action
	// behind a yes/no modal (default Cancel) so a slipped Shift on the
	// reversible [c] hide-done toggle can't wipe the download history
	// (DESTRUCT-02).
	clearDoneConfirm components.Confirm
	// removePathConfirm gates the [X] "remove broken search path" action (B9)
	// behind a yes/no modal so a slipped key can't silently rewrite the config.
	removePathConfirm components.Confirm
	// persistSearchPaths persists an edited search-path list to config.toml.
	// nil-safe: when unset, [X] removal applies in-memory only for the session.
	persistSearchPaths func([]string) error

	profilePicker           *components.ProfilePicker
	profilePickerTargetPath string

	// Hugging Face integration. Wired by builders (T13); nil-safe until then.
	hfClient       *hfhub.Client
	dlManager      *downloadmgr.Manager
	hfSearch       *components.HFSearchPicker
	hfFilePicker   *components.HFFilePicker
	downloadEvents <-chan downloadmgr.Event
	pendingRepoID  string // carried between RepoInfo lookup and file picker open

	// Destination chooser: when more than one search path is configured, the
	// file-picker selection is parked in pendingDL while pathChooser asks the
	// user which root to download into.
	pendingDL   *pendingDownload
	pathChooser *pathChooser

	// Downloads section state: per-download transfer-rate samples (for
	// speed/ETA), keyboard focus index, and a visual "clear completed" flag.
	rates    map[string]rateSample
	dlFocus  int
	hideDone bool

	keys modelsKeyMap

	infoPanel       *components.InfoPanel
	infoPanelUsedBy []components.ProfileRef
}

// modelsSubView selects which section the Models tab renders.
type modelsSubView int

const (
	mvLibrary modelsSubView = iota
	mvDownloads
	mvDiscover
)

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

// StatusMessage implements ui.StatusMessageProvider: the page's flash also
// lands in the always-visible status bar, level included.
func (p ModelsPage) StatusMessage() (string, components.StatusLevel) {
	return p.flash.Current()
}

// NewModelsPage builds a page wired to a Scanner and configured search paths.
func NewModelsPage(scanner modelscanner.Scanner, paths []string) ModelsPage {
	cols := []table.Column{
		{Title: "", Width: markerColumnWidth},
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
		rates:     make(map[string]rateSample),
	}
}

// WithProfileStore enables the "Use in existing profile" action by
// giving the page access to the profile store. Without it, that option
// is hidden from the action menu.
func (p ModelsPage) WithProfileStore(store profilestore.Store) ModelsPage {
	p.store = store
	return p
}

// WithSearchPathPersister wires the callback that persists an edited search-
// path list to the config file, enabling the [X] "remove broken search path"
// action (B9). Without it, removal is session-only and a flash warns the user.
func (p ModelsPage) WithSearchPathPersister(fn func([]string) error) ModelsPage {
	p.persistSearchPaths = fn
	return p
}

// configTarget names the operator's config file for the empty-state hints. It
// asks config.DefaultConfigPath rather than restating "~/.config/model-loader"
// here: os.UserConfigDir honours $XDG_CONFIG_HOME, so a hardcoded hint pointed
// operators at a tree the running process never reads (CFG1). One base, one
// literal. Degrades to a path-free wording if the lookup fails.
func configTarget() string {
	path, err := config.DefaultConfigPath()
	if err != nil {
		return "your model-loader config.toml"
	}
	return path
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
	}
	return p
}

func (p ModelsPage) Init() tea.Cmd {
	cmds := []tea.Cmd{startScanCmd(p.scanner, p.paths, p.scanID)}
	if p.dlManager != nil {
		cmds = append(cmds, p.tickCmd())
	}
	return tea.Batch(cmds...)
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
	if p.clearDoneConfirm.Active() {
		return p.clearDoneConfirm.View()
	}
	if p.removePathConfirm.Active() {
		return p.removePathConfirm.View()
	}
	if p.action != nil {
		return p.renderActionMenu()
	}
	if p.pathChooser != nil {
		return components.Modal("", p.renderPathChooser(), p.width, p.height)
	}

	header := theme.Title.Render("Models")
	tabs := p.renderSubTabs()
	var body string
	switch p.subView {
	case mvDownloads:
		body = p.renderDownloadsView()
	case mvDiscover:
		body = p.renderDiscoverView()
	default:
		body = p.renderLibraryView()
	}
	footer := p.flash.View()
	return lipgloss.JoinVertical(lipgloss.Left, header, tabs, body, footer)
}

// renderSubTabs draws the Library / Downloads / Discover section strip with
// the active section highlighted. The Downloads label carries a live count of
// in-flight transfers so the user notices background activity from any tab.
func (p ModelsPage) renderSubTabs() string {
	label := func(v modelsSubView, text string) string {
		return components.ActiveLabel(text, v == p.subView)
	}
	dl := "Downloads"
	if n := p.activeDownloadCount(); n > 0 {
		dl = fmt.Sprintf("Downloads (%d)", n)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		label(mvLibrary, "Library"),
		label(mvDownloads, dl),
		label(mvDiscover, "Discover"),
	)
}

// renderLibraryView renders the local model table (or empty state) plus the
// filter line and the optional info panel. The page header, section tabs and
// flash are added by View.
func (p ModelsPage) renderLibraryView() string {
	statusLine := p.renderStatus()
	filterLine := components.FilterLine(p.filterMode, p.filter)
	// While a filter is active the per-path header count ([N]) still shows the
	// unfiltered total, so surface the live match count next to the filter
	// itself (TUI_AUDIT F-05). The zero-match case has its own empty state.
	if p.filter != "" {
		if n := len(p.visibleFiles()); n > 0 {
			word := "matches"
			if n == 1 {
				word = "match"
			}
			filterLine += theme.Subtitle.Render(fmt.Sprintf("  (%d %s)", n, word))
		}
	}
	var content string
	switch {
	case len(p.files) == 0 && p.hasErrorRoot():
		// Zero files with a failed root is an error artifact, not an empty
		// library — say so instead of the misleading "No .gguf files" copy.
		emptyMsg := components.EmptyState("Scan failed for one or more paths", "Press [R] to retry or edit "+configTarget())
		content = lipgloss.JoinVertical(lipgloss.Left, statusLine, emptyMsg, filterLine)
	case len(p.files) == 0 && p.isScanning():
		content = lipgloss.JoinVertical(lipgloss.Left, statusLine, theme.Subtitle.Render("Scanning configured paths…"), filterLine)
	case len(p.files) == 0 && (len(p.paths) == 0 || p.hasScannedRoot()):
		emptyMsg := components.EmptyState("No .gguf files in configured search paths", "Press [R] to rescan, or edit "+configTarget())
		content = lipgloss.JoinVertical(lipgloss.Left, statusLine, emptyMsg, filterLine)
	case len(p.visibleFiles()) == 0 && p.filter != "":
		emptyMsg := components.EmptyState("No models match the current filter", "Press [esc] to clear filter, or [/] to edit filter")
		content = lipgloss.JoinVertical(lipgloss.Left, statusLine, emptyMsg, filterLine)
	default:
		content = lipgloss.JoinVertical(lipgloss.Left, statusLine, p.table.View(), filterLine)
	}
	if p.infoPanel != nil {
		// F-09 audit: at narrow widths (≤80 cols) splitting the row
		// crams the panel into the table cells; stack vertically instead
		// so the panel still reads cleanly. Above the narrow threshold
		// fall back to the side-by-side layout.
		if p.width > 0 && p.width < theme.NarrowWidthThreshold {
			panelW := p.width
			if panelW > 80 {
				panelW = 80
			}
			panel := lipgloss.NewStyle().MaxHeight(max(3, p.height-6-p.table.Height())).Render(p.infoPanel.Render(panelW))
			content = lipgloss.JoinVertical(lipgloss.Left, content, panel)
		} else {
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
	}
	return content
}

// Hints implements ui.HintProvider for the Models tab.
func (p ModelsPage) Hints() string {
	if p.profilePicker != nil {
		return "[↑↓] move  [enter] select  [esc] cancel"
	}
	if p.deleteConfirm.Active() {
		return components.ConfirmHints
	}
	if p.clearDoneConfirm.Active() {
		return components.ConfirmHints
	}
	if p.removePathConfirm.Active() {
		return components.ConfirmHints
	}
	if p.action != nil {
		return "[↑↓] move  [enter] select  [esc] cancel"
	}
	if p.pathChooser != nil {
		return "[↑↓] move  [enter] choose path  [esc] cancel"
	}
	if p.filterMode {
		return "[type] filter  [esc] clear"
	}
	if p.hfSearch != nil && p.hfSearch.IsActive() {
		return "[↑↓] move  [enter] select  [esc] back  [ctrl+g] GGUF-only"
	}
	if p.hfFilePicker != nil && p.hfFilePicker.IsActive() {
		return "[↑↓] move  [space] toggle  [enter] download  [esc] back"
	}
	if p.infoPanel != nil {
		return "[→/g] sizing  [esc] close info"
	}
	switch p.subView {
	case mvDownloads:
		return "[←/→] section  [↑↓] focus  [x] cancel  [r] resume  [c] hide done  [C] clear done"
	case mvDiscover:
		return "[←/→] section  [enter] search HF  [esc] back to library"
	default:
		base := "[←/→] section  [/] filter  [R] rescan  [s] search HF  [enter] actions  [i] info  [esc] clear"
		if p.hasErrorRoot() {
			base += "  [X] remove broken path"
		}
		return base
	}
}
