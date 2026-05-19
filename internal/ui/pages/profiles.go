// Package pages holds tab page implementations.
package pages

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/service/validator"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/pages/profile_editor"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// ProfilesPage is the master-detail page for managing profiles.
//
// After CQ-002 the editor (form, draft, sub-tab, advanced table,
// discard-confirm) lives in a profile_editor.Editor sub-model. The page
// keeps only master-list, delete-confirm, picker overlay, status flash.
type ProfilesPage struct {
	store        profilestore.Store
	schema       domain.FlagSchema
	catalogStore backendcatalog.Store
	schemaStore  backendcatalog.SchemaStore
	list         list.Model
	listKeys     profilesKeyMap
	width        int
	height       int

	editor        profile_editor.Editor
	deleteConfirm components.Confirm
	conflictModal components.ConflictModal
	undoModal     components.UndoModal

	flash components.Flash

	picker modelPickerOverlay

	importPickerActive bool
	importPicker       filepicker.Model

	exportDir string

	// --- launcher fields ---
	manager   processmgr.Manager
	validator validator.Validator
	resolver  backendcatalog.Resolver
	logger    *slog.Logger

	running []domain.RunningInstance
	bgMode  bool
	launch  launchTracker

	killConfirm components.Confirm
}

// NewProfilesPage constructs the page wired to a Store and FlagSchema.
func NewProfilesPage(store profilestore.Store, schema domain.FlagSchema) ProfilesPage {
	delegate := newProfileItemDelegate()
	l := list.New(nil, delegate, 0, 0)
	l.Title = "Profiles"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	return ProfilesPage{
		store:    store,
		schema:   schema,
		editor:   profile_editor.New(schema),
		list:     l,
		listKeys: defaultProfilesKeys(),
		flash:    components.NewFlash("profiles"),
		bgMode:   true,
		launch: launchTracker{
			spinner: sp,
		},
		logger: log.Nop(),
	}
}

// WithProcessManager injects the process manager and validator for launching
// profiles directly from this page.
func (p ProfilesPage) WithProcessManager(mgr processmgr.Manager, val validator.Validator) ProfilesPage {
	p.manager = mgr
	p.validator = val
	return p
}

// WithBackendResolver injects the backend resolver so the launcher can
// validate against the correct schema and resolve the executable per profile.
func (p ProfilesPage) WithBackendResolver(r backendcatalog.Resolver) ProfilesPage {
	p.resolver = r
	return p
}

// WithLogger injects the application logger.
func (p ProfilesPage) WithLogger(lg *slog.Logger) ProfilesPage {
	if lg != nil {
		p.logger = lg
	}
	return p
}

// WithModelScanner enables the ctrl+p model picker overlay in the editor.
func (p ProfilesPage) WithModelScanner(scanner components.ModelScanner, paths []string) ProfilesPage {
	p.picker.scanner = scanner
	p.picker.scanPaths = paths
	return p
}

// WithBackendCatalog injects the backend catalog so the editor can list backends.
func (p ProfilesPage) WithBackendCatalog(catalogStore backendcatalog.Store, schemaStore backendcatalog.SchemaStore) ProfilesPage {
	p.catalogStore = catalogStore
	p.schemaStore = schemaStore
	return p
}

// WithExportDir enables the [e] export shortcut by configuring the
// destination directory for profile-bundle JSON files. Empty dir disables
// the shortcut at runtime (it flashes a "not configured" message instead
// of writing).
func (p ProfilesPage) WithExportDir(dir string) ProfilesPage {
	p.exportDir = dir
	return p
}

func (p ProfilesPage) Init() tea.Cmd {
	return p.loadCmd()
}

func (p ProfilesPage) loadCmd() tea.Cmd {
	return func() tea.Msg {
		ps, diags, err := p.store.ListWithDiagnostics()
		return loadedMsg{profiles: ps, diags: diags, err: err}
	}
}

func (p ProfilesPage) View() string {
	leftW := p.width / 3
	rightW := (p.width*2)/3 - 2
	left := lipgloss.NewStyle().Width(leftW).Render(p.list.View())
	right := lipgloss.NewStyle().Width(rightW).Render(p.detailView())
	// Build a multi-line divider matching the tallest pane.
	leftH := len(strings.Split(left, "\n"))
	rightH := len(strings.Split(right, "\n"))
	divH := leftH
	if rightH > divH {
		divH = rightH
	}
	divLine := lipgloss.NewStyle().Foreground(theme.ColorDim).Render("│")
	divider := strings.Repeat(divLine+"\n", divH-1) + divLine
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)

	running := p.renderRunningList()
	if running != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, body, "", running)
	}

	if v := p.flash.View(); v != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, body, v)
	}

	if status := p.renderLaunchStatus(); status != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, body, status)
	}

	return body
}

func (p ProfilesPage) OverlayView() Overlay {
	var raw string
	switch {
	case p.killConfirm.Active():
		raw = p.killConfirm.View()
	case p.importPickerActive:
		raw = p.importPicker.View()
	case p.picker.active:
		raw = p.picker.picker.View()
	case p.conflictModal.Active():
		raw = p.conflictModal.View()
	case p.undoModal.Active():
		raw = p.undoModal.View()
	case p.editor.Active():
		raw = p.editor.View()
	case p.deleteConfirm.Active():
		raw = p.deleteConfirm.View()
	default:
		return Overlay{}
	}
	// Center the modal box inside a full p.width × p.height canvas so the
	// surrounding spaces from lipgloss.Place fully overwrite the body when
	// components.Overlay composites in root.go (F-03 audit: stops master-list
	// rows from bleeding around the modal frame).
	placed := lipgloss.Place(p.width, p.height, lipgloss.Center, lipgloss.Center, raw)
	return Overlay{Content: placed, Width: p.width, Height: p.height, Active: true}
}

func (p ProfilesPage) detailView() string {
	if len(p.list.Items()) == 0 {
		return components.EmptyState("No profiles yet", "Press [n] to create one")
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok {
		return ""
	}
	pr := sel.p
	backend := pr.Launch.BackendID
	if backend == "" {
		backend = "(default)"
	}
	tags := profile_editor.FormatTags(pr.Tags)
	if tags == "" {
		tags = "(none)"
	}
	argsBlock := formatArgsBlock(pr.Args)
	return fmt.Sprintf(
		"%s\n%s\n\nID:      %s\nModel:   %s\nBackend: %s\nTags:    %s\nArgs:%s",
		theme.Title.Render(pr.Name),
		theme.Subtitle.Render(pr.Description),
		pr.ID,
		pr.Model,
		backend,
		tags,
		argsBlock,
	)
}

// Hints implements ui.HintProvider — returns page-local key reminders for
// the status bar. Varies by editor / picker / confirm / list mode.
func (p ProfilesPage) Hints() string {
	switch {
	case p.picker.active:
		return "[↑↓] move  [enter] pick  [esc] cancel"
	case p.deleteConfirm.Active():
		return "[←→] choose  [enter] confirm"
	case p.editor.Active():
		return "[ctrl+t] sub-tab  [ctrl+p] pick model  [esc] cancel"
	default:
		return "[enter] launch  [E] edit  [n] new  [d] dup  [x] del  [b] bg/fg  [k] kill  [r] refresh  [e] export  [p] pin  [I] import  [u] undo  [/] filter"
	}
}

func (p ProfilesPage) renderRunningList() string {
	if len(p.running) == 0 {
		return "Running: " + components.EmptyState("(none)", "Press [enter] to launch selected profile")
	}
	lines := []string{theme.Subtitle.Render("Running")}
	for _, ri := range p.running {
		tag := "fg"
		if ri.Background {
			tag = "bg"
		}
		lines = append(lines, fmt.Sprintf("  %s pid=%d port=%d %s", ri.ProfileID, ri.PID, ri.Port, tag))
	}
	return strings.Join(lines, "\n")
}

func (p ProfilesPage) renderLaunchStatus() string {
	if p.launch.status != "" {
		statusLine := p.launch.status
		if p.launch.waitPID != 0 {
			statusLine = p.launch.spinner.View() + " " + statusLine
		}
		return theme.Subtitle.Render(statusLine)
	}
	return p.flash.View()
}
