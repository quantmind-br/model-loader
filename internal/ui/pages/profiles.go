// Package pages holds tab page implementations.
package pages

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
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

// modelPickerOverlay groups the page-owned ctrl+p picker overlay state
// (active flag, the picker model, and the scanner/paths used to seed it).
// Bundled so ProfilesPage stays under the 12-field cap from CQ-002.
type modelPickerOverlay struct {
	active    bool
	picker    components.ModelPicker
	scanner   components.ModelScanner
	scanPaths []string
}

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

	running        []domain.RunningInstance
	bgMode         bool
	launchStatus   string
	launchStatusAt time.Time
	launchWaitPID  int
	launchSpinner  spinner.Model

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
		store:         store,
		schema:        schema,
		editor:        profile_editor.New(schema),
		list:          l,
		listKeys:      defaultProfilesKeys(),
		flash:         components.NewFlash("profiles"),
		bgMode:        true,
		launchSpinner: sp,
		logger:        log.Nop(),
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

// loadedMsg is emitted by the load command.
type loadedMsg struct {
	profiles []domain.Profile
	diags    []profilestore.ListDiagnostic
	err      error
}

// launchedMsg is emitted after a successful Launch + WaitHealthy.
type launchedMsg struct {
	inst      domain.RunningInstance
	attemptID string
}

// launchErrMsg is emitted when validation or Launch itself fails.
type launchErrMsg struct {
	err error
}

type healthyMsg struct{ pid int }

// profilesKillConfirmedMsg is emitted by killConfirm.onYes when the user
// confirms a kill. The page handles it in Update so manager I/O and status
// mutation stay on the UI thread.
type profilesKillConfirmedMsg struct{ pid int }

func (p ProfilesPage) Init() tea.Cmd {
	return p.loadCmd()
}

func (p ProfilesPage) loadCmd() tea.Cmd {
	return func() tea.Msg {
		ps, diags, err := p.store.ListWithDiagnostics()
		return loadedMsg{profiles: ps, diags: diags, err: err}
	}
}

// Update is a thin dispatcher: each typed-message arm delegates to a
// private handle<MsgType> method. Non-key messages forward to the editor
// (when active) or to forwardToConfirms so active huh forms can complete
// their internal Cmd→Msg handshakes.
func (p ProfilesPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		return p.handleResize(m)
	case components.FlashClearMsg:
		return p.handleFlashClear(m)
	case loadedMsg:
		return p.handleLoaded(m)
	case components.PickerScanStartedMsg, components.PickerScanEventMsg, components.PickerScanClosedMsg:
		return p.handlePickerScan(msg)
	case UseInNewProfileMsg:
		return p.handleUseInNewProfile(m)
	case components.ModelPickedMsg:
		return p.handleModelPicked(m)
	case components.ModelPickerCancelledMsg:
		return p.handleModelPickerCancelled(m)
	case profileDeleteConfirmedMsg:
		return p.performDelete(m.id)
	case profile_editor.EditorCommittedMsg:
		return p.handleEditorCommitted(m)
	case profile_editor.EditorCancelledMsg:
		return p, nil
	case importDoneMsg:
		return p.handleImportDone(m)
	case undoDoneMsg:
		return p.handleUndoDone(m)
	case NavigateToSizingMsg:
		return p.handleNavigateToSizing(m)
	case launchedMsg:
		return p.handleLaunched(m)
	case healthyMsg:
		return p.handleHealthy(m)
	case launchErrMsg:
		return p.handleLaunchErr(m)
	case spinner.TickMsg:
		return p.handleSpinnerTick(m)
	case LaunchProfileMsg:
		return p.handleLaunchProfile(m)
	case profilesKillConfirmedMsg:
		return p.handleKillConfirmed(m)
	case tea.KeyMsg:
		return p.handleKey(m)
	}
	return p.forwardNonKey(msg)
}

func (p ProfilesPage) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	p.width, p.height = msg.Width, msg.Height
	listWidth := p.width / 3
	listHeight := msg.Height - 6
	if listHeight < 3 {
		listHeight = 3
	}
	p.list.SetSize(listWidth, listHeight)
	return p, nil
}

func (p ProfilesPage) handleFlashClear(msg components.FlashClearMsg) (tea.Model, tea.Cmd) {
	p.flash, _ = p.flash.Update(msg)
	return p, nil
}

func (p ProfilesPage) handleLoaded(msg loadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		p, fc := p.withFlashError("load error: " + msg.err.Error())
		return p, fc
	}
	sortProfilesPinnedFirst(msg.profiles)
	items := make([]list.Item, 0, len(msg.profiles)+len(msg.diags))
	for _, pr := range msg.profiles {
		items = append(items, item{p: pr})
	}
	for _, d := range msg.diags {
		items = append(items, corruptItem{id: d.ID, err: d.Err})
	}
	p.list.SetItems(items)
	return p, nil
}

func (p ProfilesPage) handlePickerScan(msg tea.Msg) (tea.Model, tea.Cmd) {
	if p.picker.active {
		return p.updatePicker(msg)
	}
	return p, nil
}

func (p ProfilesPage) handleUseInNewProfile(msg UseInNewProfileMsg) (tea.Model, tea.Cmd) {
	d := p.newDraftDefaults()
	d.Model = msg.Path
	p.editor = p.prepareEditor()
	var openCmd tea.Cmd
	p.editor, openCmd = p.editor.Open(d)
	p, fc := p.withFlash("new profile prefilled with picked model")
	return p, tea.Batch(openCmd, fc)
}

func (p ProfilesPage) handleModelPicked(msg components.ModelPickedMsg) (tea.Model, tea.Cmd) {
	p.picker.active = false
	if c := p.picker.picker.Cancel(); c != nil {
		c()
	}
	var cmd tea.Cmd
	p.editor, cmd = p.editor.SetModelPath(msg.Path)
	return p, cmd
}

func (p ProfilesPage) handleModelPickerCancelled(_ components.ModelPickerCancelledMsg) (tea.Model, tea.Cmd) {
	p.picker.active = false
	return p, nil
}

// resolveSchemaForBackendID looks up the schema for a backend ID through
// the catalog and schema stores. Returns an empty schema on any error.
func (p ProfilesPage) resolveSchemaForBackendID(backendID string) domain.FlagSchema {
	if p.catalogStore == nil || p.schemaStore == nil {
		return domain.FlagSchema{}
	}
	catalog, err := p.catalogStore.Load()
	if err != nil {
		return domain.FlagSchema{}
	}
	if backendID == "" {
		backendID = catalog.DefaultBackendID
	}
	var backend domain.Backend
	for _, b := range catalog.Backends {
		if b.ID == backendID {
			backend = b
			break
		}
	}
	if backend.ID == "" {
		return domain.FlagSchema{}
	}
	schema, err := p.schemaStore.Load(backendcatalog.SchemaStoreRef(backend.SchemaRef))
	if err != nil {
		return domain.FlagSchema{}
	}
	return schema.ToFlagSchema()
}

// handleEditorCommitted persists the saved Draft via the store, refreshes
// the list, and surfaces success/failure as a flash.
func (p ProfilesPage) handleEditorCommitted(msg profile_editor.EditorCommittedMsg) (tea.Model, tea.Cmd) {
	d := msg.Draft
	if d.ID == "" {
		d.ID = domain.Slugify(d.Name)
	}
	schema := p.resolveSchemaForBackendID(d.BackendID)

	var pr domain.Profile
	if !d.IsNew {
		if existing, err := p.store.Get(d.ID); err == nil {
			pr = d.ApplyToWithSchema(existing, schema)
			pr.Meta = existing.Meta
		} else {
			pr = d.ToProfileWithSchema(schema)
		}
	} else {
		pr = d.ToProfileWithSchema(schema)
	}

	var fc tea.Cmd
	if err := p.store.Save(pr); err != nil {
		p, fc = p.withFlashError("save failed: " + err.Error())
	} else {
		p, fc = p.withFlash("saved " + pr.ID)
	}
	return p, tea.Batch(p.loadCmd(), fc)
}

// handleKey routes key input. Priority: editor (which owns its discard
// confirm) > delete confirm > list nav. Picker is intercepted on ctrl+p
// or while open before forwarding to the editor.
func (p ProfilesPage) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if p.killConfirm.Active() {
		return p.updateKillConfirm(msg)
	}
	if p.importPickerActive {
		updated, cmd := p.importPicker.Update(msg)
		p.importPicker = updated
		if didSelect, path := p.importPicker.DidSelectFile(msg); didSelect {
			p.importPickerActive = false
			return p.startImportWithPath(path)
		}
		if msg.String() == "esc" {
			p.importPickerActive = false
			return p, nil
		}
		return p, cmd
	}
	if p.picker.active {
		return p.updatePicker(msg)
	}
	if p.conflictModal.Active() {
		var cmd tea.Cmd
		p.conflictModal, cmd = p.conflictModal.Update(msg)
		return p, cmd
	}
	if p.undoModal.Active() {
		var cmd tea.Cmd
		p.undoModal, cmd = p.undoModal.Update(msg)
		return p, cmd
	}
	if p.editor.Active() {
		if msg.String() == "ctrl+p" && p.picker.scanner != nil {
			p.picker.picker = components.NewModelPicker(p.picker.scanner, p.picker.scanPaths)
			p.picker.active = true
			return p, p.picker.picker.Init()
		}
		var cmd tea.Cmd
		p.editor, cmd = p.editor.Update(msg)
		return p, cmd
	}
	if p.deleteConfirm.Active() {
		return p.updateConfirm(msg)
	}
	return p.updateList(msg)
}

// forwardNonKey routes non-key messages to the highest-priority active
// surface so its internal Cmd→Msg loops complete (huh focus init, async
// validation). Editor first (its discard confirm and form both need
// non-key forwarding), then delete confirm. As fallback, forwards
// list-internal messages (e.g. list.FilterMatchesMsg) to the list so the
// filter Cmd→Msg cycle settles.
func (p ProfilesPage) forwardNonKey(msg tea.Msg) (tea.Model, tea.Cmd) {
	if p.editor.Active() {
		var cmd tea.Cmd
		p.editor, cmd = p.editor.Update(msg)
		return p, cmd
	}
	if p.deleteConfirm.Active() {
		var cmd tea.Cmd
		p.deleteConfirm, cmd = p.deleteConfirm.Update(msg)
		return p, cmd
	}
	if _, isFilterMatches := msg.(list.FilterMatchesMsg); isFilterMatches {
		updated, cmd := p.list.Update(msg)
		p.list = updated
		return p, cmd
	}
	return p, nil
}

// profileDeleteConfirmedMsg is emitted by deleteConfirm.onYes when the user
// confirms a profile deletion. The page handles it in Update so the actual
// store mutation, flash, and reload all happen on the UI thread.
type profileDeleteConfirmedMsg struct{ id string }

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

func (p ProfilesPage) OverlayView() (string, int, int, bool) {
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
		return "", 0, 0, false
	}
	// Center the modal box inside a full p.width × p.height canvas so the
	// surrounding spaces from lipgloss.Place fully overwrite the body when
	// components.Overlay composites in root.go (F-03 audit: stops master-list
	// rows from bleeding around the modal frame).
	placed := lipgloss.Place(p.width, p.height, lipgloss.Center, lipgloss.Center, raw)
	return placed, p.width, p.height, true
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

func formatArgsBlock(args map[string]any) string {
	if len(args) == 0 {
		return "    (none)"
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString("\n    --")
		b.WriteString(k)
		if v := formatArgValue(args[k]); v != "" {
			b.WriteString(" ")
			b.WriteString(v)
		}
	}
	return b.String()
}

func formatArgValue(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
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

func (p ProfilesPage) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if p.list.FilterState() == list.Filtering {
		updated, cmd := p.list.Update(msg)
		p.list = updated
		return p, cmd
	}
	switch {
	case key.Matches(msg, p.listKeys.New):
		return p.startNew()
	case key.Matches(msg, p.listKeys.Edit):
		return p.startEditSelected()
	case key.Matches(msg, p.listKeys.Duplicate):
		return p.duplicateSelected()
	case key.Matches(msg, p.listKeys.Delete):
		return p.askDeleteSelected()
	case key.Matches(msg, p.listKeys.Launch):
		return p.launchSelected()
	case key.Matches(msg, p.listKeys.BgToggle):
		p.bgMode = !p.bgMode
		return p, nil
	case key.Matches(msg, p.listKeys.Kill):
		return p.askKillMostRecent()
	case key.Matches(msg, p.listKeys.Refresh):
		return p, p.loadCmd()
	case key.Matches(msg, p.listKeys.Export):
		return p.exportProfiles()
	case key.Matches(msg, p.listKeys.Pin):
		return p.togglePinSelected()
	case key.Matches(msg, p.listKeys.Import):
		return p.startImport()
	case key.Matches(msg, p.listKeys.Undo):
		return p.startUndo()
	}

	updated, cmd := p.list.Update(msg)
	p.list = updated
	return p, cmd
}

func (p ProfilesPage) exportProfiles() (tea.Model, tea.Cmd) {
	if p.exportDir == "" {
		p, fc := p.withFlashError("export directory not configured")
		return p, fc
	}
	bundle, err := profilestore.ExportAll(p.store, p.exportDir)
	if err != nil {
		p, fc := p.withFlashError("export failed: " + err.Error())
		return p, fc
	}
	filename := filepath.Base(profilestore.ExportFilename(p.exportDir, bundle.ExportedAt))
	p, fc := p.withFlash("exported to " + filename)
	return p, fc
}

func (p ProfilesPage) togglePinSelected() (tea.Model, tea.Cmd) {
	if _, isCorrupt := p.list.SelectedItem().(corruptItem); isCorrupt {
		return p, nil
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok {
		return p, nil
	}
	pr := sel.p
	pr.Pinned = !pr.Pinned
	if err := p.store.Save(pr); err != nil {
		p, fc := p.withFlashError("pin failed: " + err.Error())
		return p, fc
	}
	return p, p.loadCmd()
}

func (p ProfilesPage) startImport() (tea.Model, tea.Cmd) {
	fp := filepicker.New()
	fp.AllowedTypes = []string{".json"}
	fp.FileAllowed = true
	fp.DirAllowed = false
	if home, err := os.UserHomeDir(); err == nil {
		fp.CurrentDirectory = home
	}
	p.importPicker = fp
	p.importPickerActive = true
	return p, p.importPicker.Init()
}

func (p ProfilesPage) startImportWithPath(path string) (tea.Model, tea.Cmd) {
	p.conflictModal = components.NewConflictModal(path,
		func(mode profilestore.ConflictMode) tea.Cmd {
			return p.importBundleCmd(path, mode)
		},
		func() tea.Cmd {
			_, fc := p.withFlash("import cancelled")
			return fc
		},
	)
	return p, p.conflictModal.Init()
}

func (p ProfilesPage) importBundleCmd(path string, mode profilestore.ConflictMode) tea.Cmd {
	return func() tea.Msg {
		res, err := profilestore.ImportBundle(p.store, path, mode)
		if err != nil {
			return components.FlashClearMsg{}
		}
		return importDoneMsg{Result: res}
	}
}

type importDoneMsg struct {
	Result profilestore.ImportResult
}

func (p ProfilesPage) handleImportDone(msg importDoneMsg) (tea.Model, tea.Cmd) {
	p, fc := p.withFlash(fmt.Sprintf("imported: +%d ~%d !%d ->%d", msg.Result.Added, msg.Result.Skipped, msg.Result.Renamed, msg.Result.Replaced))
	return p, tea.Batch(fc, p.loadCmd())
}

func (p ProfilesPage) handleNavigateToSizing(msg NavigateToSizingMsg) (tea.Model, tea.Cmd) {
	items := p.list.Items()
	for i, it := range items {
		if sel, ok := it.(item); ok && sel.p.ID == msg.ProfileID {
			p.list.Select(i)
			p, cmd := p.startEditSelected()
			if rm, ok := p.(ProfilesPage); ok {
				rm.editor = rm.editor.SetSubTabSizing()
				return rm, cmd
			}
			return p, cmd
		}
	}
	p, fc := p.withFlashError("profile not found for sizing navigation")
	return p, fc
}

func (p ProfilesPage) startUndo() (tea.Model, tea.Cmd) {
	if _, isCorrupt := p.list.SelectedItem().(corruptItem); isCorrupt {
		return p, nil
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok {
		return p, nil
	}
	fsStore, ok := p.store.(*profilestore.FSStore)
	if !ok {
		p, fc := p.withFlash("undo not available for this store")
		return p, fc
	}
	prev, found, err := profilestore.LoadPrevious(fsStore.Dir(), sel.p.ID)
	if err != nil || !found {
		p, fc := p.withFlash("No previous version available")
		return p, fc
	}
	var diffs []components.FieldDiff
	if prev.Name != sel.p.Name {
		diffs = append(diffs, components.FieldDiff{Key: "Name", OldValue: prev.Name, NewValue: sel.p.Name})
	}
	if prev.Model != sel.p.Model {
		diffs = append(diffs, components.FieldDiff{Key: "Model", OldValue: prev.Model, NewValue: sel.p.Model})
	}
	p.undoModal = components.NewUndoModal(diffs,
		func() tea.Cmd {
			return p.restorePreviousCmd(prev)
		},
		func() tea.Cmd {
			_, fc := p.withFlash("undo cancelled")
			return fc
		},
	)
	return p, p.undoModal.Init()
}

func (p ProfilesPage) restorePreviousCmd(prev domain.Profile) tea.Cmd {
	return func() tea.Msg {
		if err := p.store.Save(prev); err != nil {
			return components.FlashClearMsg{}
		}
		return undoDoneMsg{}
	}
}

type undoDoneMsg struct{}

func (p ProfilesPage) handleUndoDone(_ undoDoneMsg) (tea.Model, tea.Cmd) {
	p, fc := p.withFlash("undo complete")
	return p, tea.Batch(fc, p.loadCmd())
}

// launchSelected starts the currently selected profile directly.
func (p ProfilesPage) launchSelected() (tea.Model, tea.Cmd) {
	if p.launchWaitPID != 0 {
		return p, nil
	}
	if _, isCorrupt := p.list.SelectedItem().(corruptItem); isCorrupt {
		p, fc := p.withFlashError("corrupt entry — fix the JSON file or delete it")
		return p, fc
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok || p.manager == nil {
		return p, nil
	}
	return p, p.launchProfileCmd(sel.p)
}

func (p ProfilesPage) IsCapturingInput() bool {
	if p.editor.Active() || p.deleteConfirm.Active() || p.picker.active || p.conflictModal.Active() || p.undoModal.Active() || p.importPickerActive || p.killConfirm.Active() {
		return true
	}
	return p.list.FilterState() != list.Unfiltered
}

// Reload triggers a fresh load from the underlying store. Called by the
// root when the user navigates back to the Profiles tab.
func (p ProfilesPage) Reload() tea.Cmd {
	return p.loadCmd()
}

func (p ProfilesPage) withFlash(msg string) (ProfilesPage, tea.Cmd) {
	var cmd tea.Cmd
	p.flash, cmd = p.flash.Set(msg)
	return p, cmd
}

func (p ProfilesPage) withFlashError(msg string) (ProfilesPage, tea.Cmd) {
	var cmd tea.Cmd
	p.flash, cmd = p.flash.SetError(msg)
	return p, cmd
}

func (p ProfilesPage) handleLaunched(msg launchedMsg) (tea.Model, tea.Cmd) {
	p.running = append(p.running, msg.inst)
	p.launchWaitPID = msg.inst.PID
	p.launchStatus = fmt.Sprintf("pid=%d port=%d — waiting for /health…", msg.inst.PID, msg.inst.Port)
	p.launchStatusAt = time.Time{}
	mgr := p.manager
	port := msg.inst.Port
	pid := msg.inst.PID
	attemptID := msg.attemptID
	waitCmd := func() tea.Msg {
		if err := mgr.WaitHealthy(pid, port, 30*time.Second, attemptID); err != nil {
			return launchErrMsg{err: fmt.Errorf("pid %d not healthy: %w", pid, err)}
		}
		return healthyMsg{pid: pid}
	}
	return p, tea.Batch(p.launchSpinner.Tick, waitCmd)
}

func (p ProfilesPage) handleHealthy(msg healthyMsg) (tea.Model, tea.Cmd) {
	p.launchWaitPID = 0
	p, fc := p.withFlash(fmt.Sprintf("healthy pid=%d", msg.pid))
	pid := msg.pid
	return p, tea.Batch(fc, func() tea.Msg { return SwitchToServerMsg{PID: pid} })
}

func (p ProfilesPage) handleLaunchErr(msg launchErrMsg) (tea.Model, tea.Cmd) {
	pid := p.launchWaitPID
	p.launchWaitPID = 0
	base := friendlyLaunchError(msg.err)
	if pid != 0 && p.manager != nil {
		if exit, ok := p.manager.GetExitInfo(pid); ok {
			base = enrichWithExit(base, exit)
		}
	}
	p, fc := p.withFlash(base)
	return p, fc
}

func (p ProfilesPage) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if p.launchWaitPID == 0 {
		return p, nil
	}
	updated, cmd := p.launchSpinner.Update(msg)
	p.launchSpinner = updated
	return p, cmd
}

func (p ProfilesPage) handleLaunchProfile(msg LaunchProfileMsg) (tea.Model, tea.Cmd) {
	if p.manager == nil {
		p, fc := p.withFlashError("launch failed: process manager unavailable")
		return p, fc
	}
	selected, err := p.store.Get(msg.ID)
	if err != nil {
		p, fc := p.withFlashError("launch failed: " + err.Error())
		return p, fc
	}
	return p, p.launchProfileCmd(selected)
}

func (p ProfilesPage) handleKillConfirmed(msg profilesKillConfirmedMsg) (tea.Model, tea.Cmd) {
	var fc tea.Cmd
	p, fc = p.performKill(msg.pid)
	return p, fc
}

func (p ProfilesPage) askKillMostRecent() (tea.Model, tea.Cmd) {
	if len(p.running) == 0 || p.manager == nil {
		return p, nil
	}
	pid := p.running[len(p.running)-1].PID
	p.killConfirm = components.NewConfirm(
		fmt.Sprintf("Kill pid=%d?", pid),
		pid,
		func(payload any) tea.Cmd {
			id, _ := payload.(int)
			return func() tea.Msg { return profilesKillConfirmedMsg{pid: id} }
		},
		"Kill",
		"Cancel",
	)
	return p, p.killConfirm.Init()
}

func (p ProfilesPage) updateKillConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	p.killConfirm, cmd = p.killConfirm.Update(msg)
	return p, cmd
}

func (p ProfilesPage) performKill(pid int) (ProfilesPage, tea.Cmd) {
	if err := p.manager.Kill(pid); err != nil {
		return p.withFlashError("error: " + err.Error())
	}
	out := p.running[:0]
	for _, ri := range p.running {
		if ri.PID != pid {
			out = append(out, ri)
		}
	}
	p.running = out
	return p.withFlash(fmt.Sprintf("killed pid=%d", pid))
}

func (p ProfilesPage) launchProfileCmd(selected domain.Profile) tea.Cmd {
	val := p.validator
	mgr := p.manager
	res := p.resolver
	lg := p.logger
	attemptID := log.NewAttemptID()
	mode := processmgr.LaunchBackground
	if !p.bgMode {
		mode = processmgr.LaunchForeground
	}
	return func() tea.Msg {
		evt := lg.With("attempt_id", attemptID, "profile_id", selected.ID)
		evt.Info("launch_pipeline_start", "mode", modeString(mode))
		if res == nil {
			return launchErrMsg{err: fmt.Errorf("no backend resolver configured")}
		}
		rb, err := res.Resolve(selected)
		if err != nil {
			evt.Error("launch_pipeline_failed", "step", "resolve", "err", err)
			return launchErrMsg{err: fmt.Errorf("resolve backend: %w", err)}
		}
		activeSchema := rb.Schema.ToFlagSchema()
		if val != nil {
			rep := val.Validate(selected, activeSchema)
			if rep.HasBlockingErrors() {
				evt.Error("launch_pipeline_failed",
					"step", "validate", "err_count", len(rep.Errors))
				return launchErrMsg{err: fmt.Errorf("validation failed: %d errors", len(rep.Errors))}
			}
		}
		selected.Launch.ResolvedExecutable = rb.ExecutablePath
		selected.Launch.ResolvedBackendKind = rb.Backend.Kind
		inst, err := mgr.Launch(selected, mode, attemptID)
		if err != nil {
			evt.Error("launch_pipeline_failed", "step", "spawn", "err", err)
			return launchErrMsg{err: err}
		}
		return launchedMsg{inst: inst, attemptID: attemptID}
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
	if p.launchStatus != "" {
		statusLine := p.launchStatus
		if p.launchWaitPID != 0 {
			statusLine = p.launchSpinner.View() + " " + statusLine
		}
		return theme.Subtitle.Render(statusLine)
	}
	return p.flash.View()
}

func friendlyLaunchError(err error) string {
	switch {
	case errors.Is(err, processmgr.ErrPortBusy):
		return "error: port in use — change the profile port or kill the running PID"
	case errors.Is(err, processmgr.ErrModelNotFound):
		return "error: model file not found — fix the profile's Model path"
	case errors.Is(err, processmgr.ErrForegroundBusy):
		return "error: a foreground instance is already running — toggle [b] to background mode"
	case errors.Is(err, processmgr.ErrHealthCheckTimeout):
		return "error: server did not become healthy within timeout — check logs"
	default:
		return "error: " + err.Error()
	}
}

func enrichWithExit(base string, exit processmgr.ExitInfo) string {
	parts := []string{base}
	switch {
	case exit.ExitSignal != "":
		parts = append(parts, "(signal: "+exit.ExitSignal+")")
	case exit.ExitCode != nil:
		parts = append(parts, fmt.Sprintf("(exit %d)", *exit.ExitCode))
	}
	if last := lastNonEmpty(exit.StderrTail); last != "" {
		parts = append(parts, "— last: "+truncRunes(last, 80))
	}
	if len(parts) == 1 {
		return base
	}
	return strings.Join(parts, " ")
}

func lastNonEmpty(s []string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if strings.TrimSpace(s[i]) != "" {
			return s[i]
		}
	}
	return ""
}

func modeString(mode processmgr.LaunchMode) string {
	if mode == processmgr.LaunchForeground {
		return "foreground"
	}
	return "background"
}

func (p ProfilesPage) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	picker, cmd := p.picker.picker.Update(msg)
	p.picker.picker = picker
	return p, cmd
}

func (p ProfilesPage) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	p.deleteConfirm, cmd = p.deleteConfirm.Update(msg)
	return p, cmd
}

// performDelete executes the actual store deletion in response to the
// profileDeleteConfirmedMsg emitted by deleteConfirm.onYes. Splitting it
// out keeps store I/O and flash mutation on the page rather than inside
// the Confirm callback closure.
func (p ProfilesPage) performDelete(id string) (tea.Model, tea.Cmd) {
	var fc tea.Cmd
	if err := p.store.Delete(id); err != nil {
		p, fc = p.withFlashError("delete failed: " + err.Error())
	} else {
		p, fc = p.withFlash("deleted " + id)
	}
	return p, tea.Batch(p.loadCmd(), fc)
}

// newDraftDefaults builds a fresh Draft pre-seeded with sensible defaults
// for new profiles. Shared by [n] (start new) and "use in new profile".
func (p ProfilesPage) newDraftDefaults() profile_editor.Draft {
	d := profile_editor.Draft{
		Name:           "New Profile",
		Tags:           "",
		NGL:            "99",
		CtxSize:        "8192",
		BatchSize:      "2048",
		UBatchSize:     "512",
		Port:           "4321",
		FlashAttn:      "auto",
		CacheTypeK:     "q8_0",
		CacheTypeV:     "q8_0",
		RestartPolicy:  string(domain.RestartPolicyNone),
		MaxRestarts:    "3",
		BackoffSeconds: "5",
		IsNew:          true,
		Args:           map[string]any{},
	}
	if p.catalogStore != nil {
		if catalog, err := p.catalogStore.Load(); err == nil && catalog.DefaultBackendID != "" {
			d.BackendID = catalog.DefaultBackendID
		}
	}
	return d
}

func (p ProfilesPage) backendOptions() []huh.Option[string] {
	if p.catalogStore == nil {
		return nil
	}
	catalog, err := p.catalogStore.Load()
	if err != nil {
		return nil
	}
	opts := make([]huh.Option[string], 0, len(catalog.Backends))
	for _, b := range catalog.Backends {
		opts = append(opts, huh.NewOption(b.Name, b.ID))
	}
	return opts
}

func (p ProfilesPage) prepareEditor() profile_editor.Editor {
	return p.editor.
		SetBackendOptions(p.backendOptions()).
		SetCatalogStore(p.catalogStore).
		SetSchemaStore(p.schemaStore)
}

func (p ProfilesPage) startNew() (tea.Model, tea.Cmd) {
	p.editor = p.prepareEditor()
	var cmd tea.Cmd
	p.editor, cmd = p.editor.Open(p.newDraftDefaults())
	return p, cmd
}

func (p ProfilesPage) startEditSelected() (tea.Model, tea.Cmd) {
	if _, isCorrupt := p.list.SelectedItem().(corruptItem); isCorrupt {
		p, fc := p.withFlashError("selected entry is corrupt — delete it (x) or fix the JSON file")
		return p, fc
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok {
		return p, nil
	}
	pr := sel.p
	d := profile_editor.Draft{
		ID:             pr.ID,
		Name:           pr.Name,
		Description:    pr.Description,
		Tags:           profile_editor.FormatTags(pr.Tags),
		Model:          pr.Model,
		BackendID:      pr.Launch.BackendID,
		NGL:            profile_editor.ArgString(pr.Args["ngl"]),
		CtxSize:        profile_editor.ArgString(pr.Args["ctx-size"]),
		BatchSize:      profile_editor.ArgString(pr.Args["batch-size"]),
		UBatchSize:     profile_editor.ArgString(pr.Args["ubatch-size"]),
		Port:           profile_editor.ArgString(pr.Args["port"]),
		FlashAttn:      profile_editor.FlashAttnToString(pr.Args["flash-attn"]),
		CacheTypeK:     profile_editor.ArgString(pr.Args["cache-type-k"]),
		CacheTypeV:     profile_editor.ArgString(pr.Args["cache-type-v"]),
		Env:            append([]domain.EnvVar(nil), pr.Launch.Env...),
		RestartPolicy:  string(pr.Launch.RestartPolicy),
		MaxRestarts:    strconv.Itoa(pr.Launch.MaxRestarts),
		BackoffSeconds: strconv.Itoa(pr.Launch.BackoffSeconds),
	}
	// Copy remaining args not mapped to hardcoded Essentials fields into
	// the generic Args map so the Advanced tab can edit them.
	d.Args = map[string]any{}
	for k, v := range pr.Args {
		switch k {
		case "ngl", "ctx-size", "batch-size", "ubatch-size", "port", "flash-attn", "cache-type-k", "cache-type-v":
			continue
		}
		d.Args[k] = v
	}
	p.editor = p.prepareEditor()
	var cmd tea.Cmd
	p.editor, cmd = p.editor.Open(d)
	return p, cmd
}

func (p ProfilesPage) duplicateSelected() (tea.Model, tea.Cmd) {
	if _, isCorrupt := p.list.SelectedItem().(corruptItem); isCorrupt {
		p, fc := p.withFlashError("selected entry is corrupt — delete it (x) or fix the JSON file")
		return p, fc
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok {
		return p, nil
	}
	newID := sel.p.ID + "-copy"
	if _, err := p.store.Duplicate(sel.p.ID, newID); err != nil {
		p, fc := p.withFlashError("duplicate failed: " + err.Error())
		return p, fc
	}
	p, fc := p.withFlash("duplicated as " + newID)
	return p, tea.Batch(p.loadCmd(), fc)
}

func (p ProfilesPage) askDeleteSelected() (tea.Model, tea.Cmd) {
	var id string
	switch sel := p.list.SelectedItem().(type) {
	case item:
		id = sel.p.ID
	case corruptItem:
		id = sel.id
	default:
		return p, nil
	}
	p.deleteConfirm = components.NewConfirm(
		"Delete profile "+id+"?",
		id,
		func(payload any) tea.Cmd {
			pid, _ := payload.(string)
			return func() tea.Msg { return profileDeleteConfirmedMsg{id: pid} }
		},
		"Delete",
		"Cancel",
	)
	return p, p.deleteConfirm.Init()
}
