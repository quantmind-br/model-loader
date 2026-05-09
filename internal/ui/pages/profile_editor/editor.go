package profile_editor

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/validator"
	"github.com/quantmind-br/llama-cpp-loader/internal/ui/components"
	"github.com/quantmind-br/llama-cpp-loader/internal/ui/theme"
)

// EditorCommittedMsg is emitted by Editor.Update when the user finishes
// the form (huh.StateCompleted). Draft is the final form state. The
// Editor self-closes (Active() == false) before this message lands.
type EditorCommittedMsg struct {
	Draft Draft
}

// EditorCancelledMsg is emitted by Editor.Update when the user
// affirmatively discards unsaved changes (or closes a clean editor with
// esc). The Editor self-closes before the message lands.
type EditorCancelledMsg struct{}

// internalDiscardYesMsg is the discard-confirm onYes payload. Private so
// it never escapes Update; translated into EditorCancelledMsg on arrival.
type internalDiscardYesMsg struct{}

// Editor is the value-by-default sub-model owning the in-flight profile
// edit. Embed it in a parent page; mirror the parent's value-receiver
// convention so form-binding pointers survive bubbletea Updates.
//
// Lifecycle: zero Editor → Active() == false. Open(d) starts editing
// (Active() == true). Update routes keys, resolves ctrl+t / esc /
// advanced filter mode, and forwards leftover messages to the embedded
// huh form or discard-confirm. Form completion fires EditorCommittedMsg;
// affirmative discard fires EditorCancelledMsg. Both messages bubble
// out via the returned tea.Cmd.
type Editor struct {
	schema         domain.FlagSchema
	validator      validator.Validator
	backendOptions []huh.Option[string]
	schemaStore    backendcatalog.SchemaStore
	catalogStore   backendcatalog.Store

	active bool

	form  *huh.Form
	draft *Draft

	openSnapshot  Draft
	lastBackendID string
	schemaError   string
	submitError   string

	subTab subTab

	advanced       table.Model
	advancedAll    []table.Row
	advancedFilter string
	filterMode     bool

	discardConfirm components.Confirm
}

// New constructs an idle Editor wired to the given flag schema. The
// schema seeds the advanced flag-reference table and labels Essentials
// inputs with --help text. Active() is false until Open is called.
func New(schema domain.FlagSchema) Editor {
	tbl := newAdvancedTable(schema, 100, 12)
	return Editor{
		schema:      schema,
		validator:   validator.New(),
		advanced:    tbl,
		advancedAll: tbl.Rows(),
	}
}

// Active reports whether the editor currently owns the screen (form
// in flight or discard confirmation open).
func (e Editor) Active() bool {
	return e.active || e.discardConfirm.Active()
}

// SetBackendOptions injects the list of available backends for the select field.
func (e Editor) SetBackendOptions(opts []huh.Option[string]) Editor {
	e.backendOptions = opts
	return e
}

// SetSchemaStore injects the schema store so the editor can reload schemas
// when the user changes the selected backend.
func (e Editor) SetSchemaStore(s backendcatalog.SchemaStore) Editor {
	e.schemaStore = s
	return e
}

// SetCatalogStore injects the catalog store for backend lookups.
func (e Editor) SetCatalogStore(s backendcatalog.Store) Editor {
	e.catalogStore = s
	return e
}

// Open starts editing the given draft. Resets sub-tab to Essentials and
// clears any advanced-filter state from a previous session. Loads the
// schema for the draft's BackendID before building the form so existing
// profiles validate against their own backend's schema immediately.
func (e Editor) Open(d Draft) (Editor, tea.Cmd) {
	dp := d
	e.draft = &dp
	e.openSnapshot = dp
	e.lastBackendID = dp.BackendID
	e = e.loadSchemaForDraft()
	e.form = buildForm(e.draft, e.schema, e.backendOptions)
	e.active = true
	e.subTab = subTabEssentials
	e.advancedFilter = ""
	e.filterMode = false
	e.advanced.SetRows(e.advancedAll)
	e.discardConfirm = components.Confirm{}
	return e, e.form.Init()
}

// Cancel forces the editor closed without prompting (e.g. external
// abort). Use sparingly — the in-form esc path handles dirty-check.
func (e Editor) Cancel() Editor {
	return e.close()
}

// SetModelPath updates the in-flight draft's Model field and rebuilds
// the form so the new value is visible on Essentials. No-op when
// inactive. Callers use this to forward picker results into the editor.
func (e Editor) SetModelPath(path string) (Editor, tea.Cmd) {
	if !e.active || e.draft == nil {
		return e, nil
	}
	e.draft.Model = path
	e.form = buildForm(e.draft, e.schema, e.backendOptions)
	return e, e.form.Init()
}

// CurrentDraft returns a copy of the in-flight draft. Returns the zero
// Draft when inactive. Provided so parent pages can render a live
// preview (validator, status hints) without reaching into Editor fields.
func (e Editor) CurrentDraft() Draft {
	if e.draft == nil {
		return Draft{}
	}
	return *e.draft
}

// View renders the editor: discard confirm if open, else header +
// (form|advanced table) + filter line + validator footer.
func (e Editor) View() string {
	if e.discardConfirm.Active() {
		return e.discardConfirm.View()
	}
	if !e.active || e.form == nil {
		return ""
	}
	header := theme.Title.Render(fmt.Sprintf("Editor — [%s]   ctrl+t to switch  ctrl+p to pick model", e.subTab))
	var body string
	if e.subTab == subTabEssentials {
		body = e.form.View()
	} else {
		body = e.advanced.View()
	}
	report := e.validator.Validate(e.CurrentDraft().ToProfile(), e.schema)
	var lines []string
	if e.schemaError != "" {
		lines = append(lines, theme.Error.Render("✗ schema: "+e.schemaError))
	}
	if e.submitError != "" {
		lines = append(lines, theme.Error.Render("✗ "+e.submitError))
	}
	for _, er := range report.Errors {
		lines = append(lines, theme.Error.Render("✗ "+er.Field+": "+er.Message))
	}
	for _, w := range report.Warnings {
		lines = append(lines, theme.Warn.Render("! "+w.Field+": "+w.Message))
	}
	filterLine := ""
	if e.subTab == subTabAdvanced {
		filterLine = theme.Subtitle.Render(fmt.Sprintf("filter: %q", e.advancedFilter))
	}
	footer := strings.Join(lines, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, header, body, filterLine, footer)
}

// tabKey is the binding for ctrl+t (sub-tab toggle). Kept private so
// callers don't have to wire it into the parent's keymap.
var tabKey = key.NewBinding(key.WithKeys("ctrl+t"))

// Update routes msg through the editor:
//   - internalDiscardYesMsg → close + emit EditorCancelledMsg.
//   - discard confirm has highest priority while open.
//   - tea.KeyMsg gets processed (esc / ctrl+t / advanced filter / form).
//   - other messages forward to the form so its Cmd→Msg loops complete.
//
// On huh.StateCompleted the editor self-closes and returns
// EditorCommittedMsg via the returned cmd.
func (e Editor) Update(msg tea.Msg) (Editor, tea.Cmd) {
	if _, ok := msg.(internalDiscardYesMsg); ok {
		e = e.close()
		return e, emitCancelled
	}
	if e.discardConfirm.Active() {
		return e.updateDiscardConfirm(msg)
	}
	if !e.active {
		return e, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		return e.handleKey(km)
	}
	return e.forwardToForm(msg)
}

func (e Editor) updateDiscardConfirm(msg tea.Msg) (Editor, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "esc" {
		e.discardConfirm = components.Confirm{}
		return e, nil
	}
	var cmd tea.Cmd
	e.discardConfirm, cmd = e.discardConfirm.Update(msg)
	return e, cmd
}

// askDiscard arms the discard-confirm overlay. onYes emits
// internalDiscardYesMsg which Update folds into EditorCancelledMsg.
func (e Editor) askDiscard() (Editor, tea.Cmd) {
	e.discardConfirm = components.NewConfirm("Discard unsaved changes?", nil, func(_ any) tea.Cmd {
		return emitDiscardYes
	})
	return e, e.discardConfirm.Init()
}

func (e Editor) handleKey(msg tea.KeyMsg) (Editor, tea.Cmd) {
	if msg.String() == "esc" {
		if e.draft != nil && *e.draft != e.openSnapshot {
			return e.askDiscard()
		}
		e = e.close()
		return e, emitCancelled
	}
	if key.Matches(msg, tabKey) {
		if e.subTab == subTabEssentials {
			e.subTab = subTabAdvanced
		} else {
			e.subTab = subTabEssentials
		}
		return e, nil
	}
	if e.subTab == subTabAdvanced {
		return e.handleAdvancedKey(msg)
	}
	e.submitError = ""
	return e.forwardToForm(msg)
}

func (e Editor) handleAdvancedKey(msg tea.KeyMsg) (Editor, tea.Cmd) {
	switch msg.String() {
	case "/":
		e.filterMode = !e.filterMode
		return e, nil
	case "backspace":
		if e.filterMode && len(e.advancedFilter) > 0 {
			e.advancedFilter = e.advancedFilter[:len(e.advancedFilter)-1]
			e.advanced.SetRows(filterRows(e.advancedAll, e.advancedFilter))
		}
		return e, nil
	}
	if e.filterMode && len(msg.Runes) == 1 {
		e.advancedFilter += string(msg.Runes)
		e.advanced.SetRows(filterRows(e.advancedAll, e.advancedFilter))
		return e, nil
	}
	t, cmd := e.advanced.Update(msg)
	e.advanced = t
	return e, cmd
}

func (e Editor) forwardToForm(msg tea.Msg) (Editor, tea.Cmd) {
	if e.form == nil {
		return e, nil
	}
	updated, cmd := e.form.Update(msg)
	if f, ok := updated.(*huh.Form); ok {
		e.form = f
	}
	if e.draft != nil && e.draft.BackendID != e.lastBackendID {
		e.lastBackendID = e.draft.BackendID
		e = e.reloadSchema()
		cmd = tea.Batch(cmd, e.form.Init())
	}
	if e.form != nil && e.form.State == huh.StateCompleted {
		if e.schemaError != "" {
			e.submitError = "Cannot save: " + e.schemaError
			e.form = buildForm(e.draft, e.schema, e.backendOptions)
			return e, tea.Batch(cmd, e.form.Init())
		}
		report := e.validator.Validate(e.CurrentDraft().ToProfile(), e.schema)
		if report.HasBlockingErrors() {
			e.submitError = fmt.Sprintf("Cannot save: %d validation errors", len(report.Errors))
			e.form = buildForm(e.draft, e.schema, e.backendOptions)
			return e, tea.Batch(cmd, e.form.Init())
		}
		committed := *e.draft
		e = e.close()
		commitCmd := func() tea.Msg { return EditorCommittedMsg{Draft: committed} }
		return e, tea.Batch(cmd, commitCmd)
	}
	e.submitError = ""
	return e, cmd
}

func (e Editor) reloadSchema() Editor {
	if e.schemaStore == nil || e.catalogStore == nil || e.draft == nil {
		return e
	}
	backend, schema, err := e.resolveBackendSchema(e.draft.BackendID)
	if err != nil {
		e.schemaError = err.Error()
		e.schema = domain.FlagSchema{}
		return e
	}
	e.schemaError = ""
	e.schema = schema.ToFlagSchema()
	e.form = buildForm(e.draft, e.schema, e.backendOptions)
	tbl := newAdvancedTable(e.schema, 100, 12)
	e.advanced = tbl
	e.advancedAll = tbl.Rows()
	if e.advancedFilter != "" {
		e.advanced.SetRows(filterRows(e.advancedAll, e.advancedFilter))
	}
	_ = backend
	return e
}

func (e Editor) loadSchemaForDraft() Editor {
	if e.schemaStore == nil || e.catalogStore == nil || e.draft == nil {
		return e
	}
	backend, schema, err := e.resolveBackendSchema(e.draft.BackendID)
	if err != nil {
		e.schemaError = err.Error()
		e.schema = domain.FlagSchema{}
		return e
	}
	e.schemaError = ""
	e.schema = schema.ToFlagSchema()
	tbl := newAdvancedTable(e.schema, 100, 12)
	e.advanced = tbl
	e.advancedAll = tbl.Rows()
	if e.advancedFilter != "" {
		e.advanced.SetRows(filterRows(e.advancedAll, e.advancedFilter))
	}
	_ = backend
	return e
}

func (e Editor) resolveBackendSchema(backendID string) (domain.Backend, domain.BackendValidationSchema, error) {
	catalog, err := e.catalogStore.Load()
	if err != nil {
		return domain.Backend{}, domain.BackendValidationSchema{}, fmt.Errorf("catalog load failed: %w", err)
	}
	if backendID == "" {
		backendID = catalog.DefaultBackendID
	}
	if backendID == "" {
		return domain.Backend{}, domain.BackendValidationSchema{}, fmt.Errorf("no backend selected and no default backend")
	}
	var backend domain.Backend
	for _, b := range catalog.Backends {
		if b.ID == backendID {
			backend = b
			break
		}
	}
	if backend.ID == "" {
		return domain.Backend{}, domain.BackendValidationSchema{}, fmt.Errorf("backend %s not found in catalog", backendID)
	}
	schema, err := e.schemaStore.Load(backendcatalog.SchemaStoreRef(backend.SchemaRef))
	if err != nil {
		return domain.Backend{}, domain.BackendValidationSchema{}, fmt.Errorf("schema load failed for %s: %w", backend.SchemaRef, err)
	}
	if schema.BackendID != "" && schema.BackendID != backend.ID {
		return domain.Backend{}, domain.BackendValidationSchema{}, fmt.Errorf("schema/backend mismatch: schema has backend_id=%q, expected %q", schema.BackendID, backend.ID)
	}
	if backend.Kind != "" && schema.BackendKind != "" && schema.BackendKind != backend.Kind {
		return domain.Backend{}, domain.BackendValidationSchema{}, fmt.Errorf("schema/backend kind mismatch: schema has kind=%q, expected %q", schema.BackendKind, backend.Kind)
	}
	return backend, schema, nil
}

// close clears all in-flight editor state. Idempotent.
func (e Editor) close() Editor {
	e.active = false
	e.form = nil
	e.draft = nil
	e.openSnapshot = Draft{}
	e.discardConfirm = components.Confirm{}
	e.subTab = subTabEssentials
	e.advancedFilter = ""
	e.filterMode = false
	e.schemaError = ""
	e.submitError = ""
	return e
}

func emitCancelled() tea.Msg  { return EditorCancelledMsg{} }
func emitDiscardYes() tea.Msg { return internalDiscardYesMsg{} }
