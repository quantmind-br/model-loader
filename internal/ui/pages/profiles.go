// Package pages holds tab page implementations.
package pages

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
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
	store          profilestore.Store
	schema         domain.FlagSchema
	catalogStore   backendcatalog.Store
	schemaStore    backendcatalog.SchemaStore
	list     list.Model
	listKeys profilesKeyMap
	width    int
	height   int

	editor        profile_editor.Editor
	deleteConfirm components.Confirm

	flash   string
	flashAt time.Time

	picker modelPickerOverlay

}

// NewProfilesPage constructs the page wired to a Store and FlagSchema.
func NewProfilesPage(store profilestore.Store, schema domain.FlagSchema) ProfilesPage {
	delegate := newProfileItemDelegate()
	l := list.New(nil, delegate, 0, 0)
	l.Title = "Profiles"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)

	return ProfilesPage{
		store:    store,
		schema:   schema,
		editor:   profile_editor.New(schema),
		list:     l,
		listKeys: defaultProfilesKeys(),
	}
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

// loadedMsg is emitted by the load command.
type loadedMsg struct {
	profiles []domain.Profile
	diags    []profilestore.ListDiagnostic
	err      error
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

// Update is a thin dispatcher: each typed-message arm delegates to a
// private handle<MsgType> method. Non-key messages forward to the editor
// (when active) or to forwardToConfirms so active huh forms can complete
// their internal Cmd→Msg handshakes.
func (p ProfilesPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		return p.handleResize(m)
	case flashClearMsg:
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
	case tea.KeyMsg:
		return p.handleKey(m)
	}
	return p.forwardNonKey(msg)
}

func (p ProfilesPage) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	p.width, p.height = msg.Width, msg.Height
	p.list.SetSize(msg.Width/3, msg.Height-2)
	return p, nil
}

func (p ProfilesPage) handleFlashClear(msg flashClearMsg) (tea.Model, tea.Cmd) {
	if msg.tag == "profiles" && msg.at.Equal(p.flashAt) {
		p.flash = ""
		p.flashAt = time.Time{}
	}
	return p, nil
}

func (p ProfilesPage) handleLoaded(msg loadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		p, fc := p.withFlash("load error: " + msg.err.Error())
		return p, fc
	}
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
		p, fc = p.withFlash("save failed: " + err.Error())
	} else {
		p, fc = p.withFlash("saved " + pr.ID)
	}
	return p, tea.Batch(p.loadCmd(), fc)
}

// handleKey routes key input. Priority: editor (which owns its discard
// confirm) > delete confirm > list nav. Picker is intercepted on ctrl+p
// or while open before forwarding to the editor.
func (p ProfilesPage) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if p.picker.active {
		return p.updatePicker(msg)
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
// non-key forwarding), then delete confirm.
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
	return p, nil
}

// profileDeleteConfirmedMsg is emitted by deleteConfirm.onYes when the user
// confirms a profile deletion. The page handles it in Update so the actual
// store mutation, flash, and reload all happen on the UI thread.
type profileDeleteConfirmedMsg struct{ id string }

func (p ProfilesPage) View() string {
	if p.picker.active {
		return p.picker.picker.View()
	}
	if p.editor.Active() {
		return p.editor.View()
	}
	if p.deleteConfirm.Active() {
		return p.deleteConfirm.View()
	}

	left := theme.Pane.Width(p.width / 3).Render(p.list.View())
	right := theme.Pane.Width((p.width*2)/3 - 2).Render(p.detailView())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	if p.flash != "" {
		style := theme.Subtitle
		if !p.flashAt.IsZero() && time.Since(p.flashAt) >= flashDimAfter {
			style = style.Faint(true)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, body, style.Render(p.flash))
	}
	return body
}

func (p ProfilesPage) detailView() string {
	if len(p.list.Items()) == 0 {
		return theme.Subtitle.Render("No profiles yet. Press [n] to create one.")
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
	return fmt.Sprintf(
		"%s\n%s\n\nID:      %s\nModel:   %s\nBackend: %s\nArgs:    %v",
		theme.Title.Render(pr.Name),
		theme.Subtitle.Render(pr.Description),
		pr.ID,
		pr.Model,
		backend,
		pr.Args,
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
		return "[enter] edit  [n] new  [d] dup  [x] del  [L] launch  [/] filter"
	}
}

func (p ProfilesPage) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	}

	updated, cmd := p.list.Update(msg)
	p.list = updated
	return p, cmd
}

// launchSelected emits a LaunchProfileMsg for the currently selected
// profile so the root model can switch to the Launcher tab and run it.
func (p ProfilesPage) launchSelected() (tea.Model, tea.Cmd) {
	if _, isCorrupt := p.list.SelectedItem().(corruptItem); isCorrupt {
		p, fc := p.withFlash("corrupt entry — fix the JSON file or delete it")
		return p, fc
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok {
		return p, nil
	}
	id := sel.p.ID
	return p, func() tea.Msg { return LaunchProfileMsg{ID: id} }
}

func (p ProfilesPage) IsCapturingInput() bool {
	return p.editor.Active() || p.deleteConfirm.Active() || p.picker.active
}

// Reload triggers a fresh load from the underlying store. Called by the
// root when the user navigates back to the Profiles tab.
func (p ProfilesPage) Reload() tea.Cmd {
	return p.loadCmd()
}

// withFlash sets the flash message, stamps the time, and returns the
// page plus the Cmd that schedules the lifetime clear. Centralizing this
// keeps every flash site honest about the timer.
func (p ProfilesPage) withFlash(msg string) (ProfilesPage, tea.Cmd) {
	p.flash = msg
	p.flashAt = time.Now()
	return p, scheduleFlashClear("profiles", p.flashAt)
}

func (p ProfilesPage) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	picker, cmd := p.picker.picker.Update(msg)
	p.picker.picker = picker
	return p, cmd
}

func (p ProfilesPage) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		p.deleteConfirm = components.Confirm{}
		p, fc := p.withFlash("delete cancelled")
		return p, fc
	}
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
		p, fc = p.withFlash("delete failed: " + err.Error())
	} else {
		p, fc = p.withFlash("deleted " + id)
	}
	return p, tea.Batch(p.loadCmd(), fc)
}

// newDraftDefaults builds a fresh Draft pre-seeded with sensible defaults
// for new profiles. Shared by [n] (start new) and "use in new profile".
func (p ProfilesPage) newDraftDefaults() profile_editor.Draft {
	d := profile_editor.Draft{
		Name:       "New Profile",
		NGL:        "99",
		CtxSize:    "8192",
		BatchSize:  "2048",
		UBatchSize: "512",
		Port:       "4321",
		FlashAttn:  "auto",
		CacheTypeK: "q8_0",
		CacheTypeV: "q8_0",
		IsNew:      true,
		Args:       map[string]any{},
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
		p, fc := p.withFlash("selected entry is corrupt — delete it (x) or fix the JSON file")
		return p, fc
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok {
		return p, nil
	}
	pr := sel.p
	d := profile_editor.Draft{
		ID:          pr.ID,
		Name:        pr.Name,
		Description: pr.Description,
		Model:       pr.Model,
		BackendID:   pr.Launch.BackendID,
		NGL:         profile_editor.ArgString(pr.Args["ngl"]),
		CtxSize:     profile_editor.ArgString(pr.Args["ctx-size"]),
		BatchSize:   profile_editor.ArgString(pr.Args["batch-size"]),
		UBatchSize:  profile_editor.ArgString(pr.Args["ubatch-size"]),
		Port:        profile_editor.ArgString(pr.Args["port"]),
		FlashAttn:   profile_editor.FlashAttnToString(pr.Args["flash-attn"]),
		CacheTypeK:  profile_editor.ArgString(pr.Args["cache-type-k"]),
		CacheTypeV:  profile_editor.ArgString(pr.Args["cache-type-v"]),
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
		p, fc := p.withFlash("selected entry is corrupt — delete it (x) or fix the JSON file")
		return p, fc
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok {
		return p, nil
	}
	newID := sel.p.ID + "-copy"
	if _, err := p.store.Duplicate(sel.p.ID, newID); err != nil {
		p, fc := p.withFlash("duplicate failed: " + err.Error())
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
	)
	return p, p.deleteConfirm.Init()
}
