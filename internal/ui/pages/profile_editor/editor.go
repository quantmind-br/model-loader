package profile_editor

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/validator"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
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

	form          *huh.Form
	essentialPtrs map[string]*string
	fieldMap      map[string]huh.Field
	draft         *Draft

	openSnapshot  Draft
	lastBackendID string
	backendKind   domain.BackendKind
	schemaError   string
	submitError   string

	subTab subTab

	advanced       table.Model
	advancedAll    []table.Row
	advancedFilter string
	filterMode     bool

	advancedEditing  bool
	advancedEditFlag string
	advancedEditVal  string

	// Environment sub-tab state. Mirrors the advanced* fields above.
	envTable       table.Model
	envAll         []table.Row
	envFilter      string
	envFilterMode  bool
	envEditing     bool
	envEditKey     string
	envEditValue   string
	envEditIndex   int // -1 = adding new row; >=0 = editing existing
	envEditField   envField
	envSubmitError string

	sizingTab SizingTab

	discardConfirm components.Confirm
}

// envField selects which of (key, value) the user is typing into while
// editing an environment row inline.
type envField int

const (
	envFieldKey envField = iota
	envFieldValue
)

// envKeyRE matches a POSIX-shell identifier: leading letter or underscore,
// then letters/digits/underscores. Applied to every env Key on submit.
var envKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// New constructs an idle Editor wired to the given flag schema. The
// schema seeds the advanced flag-reference table and labels Essentials
// inputs with --help text. Active() is false until Open is called.
func New(schema domain.FlagSchema) Editor {
	tbl := newAdvancedTable(schema, nil, "", 100, 12)
	return Editor{
		schema:      schema,
		validator:   validator.New(log.Nop()),
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
	if dp.RestartPolicy == "" {
		dp.RestartPolicy = string(domain.RestartPolicyNone)
	}
	if dp.MaxRestarts == "" {
		dp.MaxRestarts = "3"
	}
	if dp.BackoffSeconds == "" {
		dp.BackoffSeconds = "5"
	}
	e.draft = &dp
	e.lastBackendID = dp.BackendID
	e = e.loadSchemaForDraft()
	e = e.hydrateEssentials()
	e.form, e.essentialPtrs, e.fieldMap = buildForm(e.draft, e.schema, e.backendOptions, e.backendKind)
	e.active = true
	e.subTab = subTabEssentials
	e.advancedFilter = ""
	e.filterMode = false
	e.advanced.SetRows(e.advancedAll)
	e.envTable = newEnvTable(e.draft.Env)
	e.envEditing = false
	e.envEditKey = ""
	e.envEditValue = ""
	e.envEditIndex = -1
	e.envEditField = envFieldKey
	e.envSubmitError = ""
	e.discardConfirm = components.Confirm{}
	e.sizingTab = newSizingTabForDraft(*e.draft)
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
	e = e.rebuildFormAndRestoreFocus()
	return e, nil
}

// SetSubTabSizing switches the editor to the Sizing sub-tab. No-op when
// inactive or when the backend is not llama-server.
func (e Editor) SetSubTabSizing() Editor {
	if !e.active || e.backendKind != domain.BackendKindLlamaServer {
		return e
	}
	e.subTab = subTabSizing
	return e
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

func (e Editor) Dirty() bool {
	if !e.active || e.draft == nil {
		return false
	}
	return !reflect.DeepEqual(*e.draft, e.openSnapshot)
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
	if e.Dirty() {
		header += " " + theme.Warn.Render("(unsaved changes)")
	}
	var body string
	switch {
	case e.subTab == subTabEssentials:
		body = e.form.View()
	case e.subTab == subTabEnvironment:
		body = e.renderEnvBody()
	case e.subTab == subTabSizing:
		body = e.sizingTab.View()
	case e.advancedEditing:
		body = theme.Subtitle.Render(fmt.Sprintf("Editing --%s: %s_", e.advancedEditFlag, e.advancedEditVal))
	default:
		body = e.advanced.View()
	}
	report := e.validator.Validate(e.CurrentDraft().ToProfileWithSchema(e.schema), e.schema)
	var lines []string
	if e.schemaError != "" {
		lines = append(lines, theme.Error.Render("✗ schema: "+e.schemaError))
	}
	if e.submitError != "" {
		lines = append(lines, theme.Error.Render("✗ "+e.submitError))
	}
	if e.envSubmitError != "" {
		lines = append(lines, theme.Error.Render("✗ "+e.envSubmitError))
	}
	for _, er := range report.Errors {
		lines = append(lines, theme.Error.Render("✗ "+er.Field+": "+er.Message))
	}
	for _, w := range report.Warnings {
		lines = append(lines, theme.Warn.Render("! "+w.Field+": "+w.Message))
	}
	filterLine := ""
	if e.subTab == subTabAdvanced {
		filterLine = theme.Subtitle.Render(fmt.Sprintf("filter: %q  (type to filter, esc to clear, enter to edit flag)", e.advancedFilter))
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
	if suggest, ok := msg.(suggestAppliedMsg); ok {
		if e.draft != nil {
			if e.draft.Essentials == nil {
				e.draft.Essentials = map[string]string{}
			}
			val := fmt.Sprintf("%d", suggest.ngl)
			e.draft.Essentials["n-gpu-layers"] = val
			if p := e.essentialPtrs["n-gpu-layers"]; p != nil {
				*p = val
			}
			e = e.rebuildFormAndRestoreFocus()
		}
		return e, nil
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
	}, "Discard", "Keep")
	return e, e.discardConfirm.Init()
}

func (e Editor) handleKey(msg tea.KeyMsg) (Editor, tea.Cmd) {
	if msg.String() == "esc" {
		if e.draft != nil && !reflect.DeepEqual(*e.draft, e.openSnapshot) {
			return e.askDiscard()
		}
		e = e.close()
		return e, emitCancelled
	}
	if key.Matches(msg, tabKey) {
		e.subTab = e.nextSubTab()
		return e, nil
	}
	if e.subTab == subTabAdvanced {
		return e.handleAdvancedKey(msg)
	}
	if e.subTab == subTabEnvironment {
		return e.handleEnvKey(msg)
	}
	if e.subTab == subTabSizing {
		updated, cmd := e.sizingTab.Update(msg)
		e.sizingTab = updated
		return e, cmd
	}
	e.submitError = ""
	return e.forwardToForm(msg)
}

func (e Editor) nextSubTab() subTab {
	max := subTab(3)
	if e.backendKind == domain.BackendKindLlamaServer {
		max = subTab(4)
	}
	return (e.subTab + 1) % max
}

func (e Editor) handleCellEscape() Editor {
	e.advancedEditing = false
	e.advancedEditFlag = ""
	e.advancedEditVal = ""
	e.envEditing = false
	e.envEditKey = ""
	e.envEditValue = ""
	e.envEditIndex = -1
	e.envEditField = envFieldKey
	e.envSubmitError = ""
	return e
}

func (e Editor) moveTableCursor(tbl table.Model, msg tea.Msg) (table.Model, tea.Cmd) {
	t, cmd := tbl.Update(msg)
	if rows := t.Rows(); len(rows) > 0 {
		c := t.Cursor()
		if c < 0 {
			t.SetCursor(0)
		} else if c >= len(rows) {
			t.SetCursor(len(rows) - 1)
		}
	}
	return t, cmd
}

func (e Editor) saveAdvancedEdit() (Editor, tea.Cmd) {
	if e.draft != nil {
		if e.draft.Args == nil {
			e.draft.Args = map[string]any{}
		}
		if e.advancedEditVal == "" {
			delete(e.draft.Args, e.advancedEditFlag)
		} else {
			val, err := parseFlagValue(e.advancedEditVal, e.schema, e.advancedEditFlag)
			if err != nil {
				e.submitError = fmt.Sprintf("Invalid value for --%s: %s", e.advancedEditFlag, err)
				return e, nil
			}
			e.draft.Args[e.advancedEditFlag] = val
		}
		tbl := newAdvancedTable(e.schema, e.draft.Args, e.backendKind, 100, 12)
		e.advanced = tbl
		e.advancedAll = tbl.Rows()
		if e.advancedFilter != "" {
			e.advanced.SetRows(filterRows(e.advancedAll, e.advancedFilter))
		}
	}
	e.advancedEditing = false
	e.advancedEditFlag = ""
	e.advancedEditVal = ""
	e.submitError = ""
	return e, nil
}

func (e Editor) startAdvancedEdit() (Editor, tea.Cmd) {
	if e.draft == nil {
		return e, nil
	}
	row := e.advanced.SelectedRow()
	if len(row) == 0 {
		return e, nil
	}
	flag := string(row[0])
	e.advancedEditing = true
	e.advancedEditFlag = flag
	e.advancedEditVal = ArgString(e.draft.Args[flag])
	return e, nil
}

func (e Editor) saveEnvEdit() (Editor, tea.Cmd) {
	if e.draft == nil {
		return e, nil
	}
	key := strings.TrimSpace(e.envEditKey)
	if !envKeyRE.MatchString(key) {
		e.envSubmitError = fmt.Sprintf("Invalid env key %q: must match [A-Za-z_][A-Za-z0-9_]*", e.envEditKey)
		return e, nil
	}
	for i, ev := range e.draft.Env {
		if ev.Key == key && i != e.envEditIndex {
			e.envSubmitError = fmt.Sprintf("Duplicate env key %q", key)
			return e, nil
		}
	}
	nev := domain.EnvVar{Key: key, Value: e.envEditValue}
	if e.envEditIndex < 0 {
		e.draft.Env = append(e.draft.Env, nev)
	} else if e.envEditIndex < len(e.draft.Env) {
		e.draft.Env[e.envEditIndex] = nev
	}
	e.envTable = newEnvTable(e.draft.Env)
	e.envEditing = false
	e.envEditKey = ""
	e.envEditValue = ""
	e.envEditIndex = -1
	e.envEditField = envFieldKey
	e.envSubmitError = ""
	return e, nil
}

func (e Editor) startEnvEdit() (Editor, tea.Cmd) {
	if e.draft == nil || len(e.draft.Env) == 0 {
		return e, nil
	}
	row := e.envTable.SelectedRow()
	if len(row) < 2 {
		return e, nil
	}
	idx := e.envTable.Cursor()
	if idx < 0 || idx >= len(e.draft.Env) {
		return e, nil
	}
	e.envEditing = true
	e.envEditIndex = idx
	e.envEditKey = e.draft.Env[idx].Key
	e.envEditValue = e.draft.Env[idx].Value
	e.envEditField = envFieldValue
	e.envSubmitError = ""
	return e, nil
}

func (e Editor) deleteEnvRow() (Editor, tea.Cmd) {
	if e.draft == nil || len(e.draft.Env) == 0 {
		return e, nil
	}
	idx := e.envTable.Cursor()
	if idx < 0 || idx >= len(e.draft.Env) {
		return e, nil
	}
	e.draft.Env = append(e.draft.Env[:idx], e.draft.Env[idx+1:]...)
	e.envTable = newEnvTable(e.draft.Env)
	e.envSubmitError = ""
	return e, nil
}

func (e Editor) handleAdvancedKey(msg tea.KeyMsg) (Editor, tea.Cmd) {
	if e.advancedEditing {
		switch msg.String() {
		case "esc":
			return e.handleCellEscape(), nil
		case "enter":
			return e.saveAdvancedEdit()
		case "backspace":
			if len(e.advancedEditVal) > 0 {
				e.advancedEditVal = e.advancedEditVal[:len(e.advancedEditVal)-1]
			}
			return e, nil
		}
		if len(msg.Runes) == 1 && msg.Runes[0] >= 32 {
			e.advancedEditVal += string(msg.Runes)
			return e, nil
		}
		return e, nil
	}

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
	case "enter":
		return e.startAdvancedEdit()
	}
	if e.filterMode && len(msg.Runes) == 1 {
		e.advancedFilter += string(msg.Runes)
		e.advanced.SetRows(filterRows(e.advancedAll, e.advancedFilter))
		return e, nil
	}
	updated, cmd := e.moveTableCursor(e.advanced, msg)
	e.advanced = updated
	return e, cmd
}

func (e Editor) renderEnvBody() string {
	if e.envEditing {
		marker := "_"
		keyDisp := e.envEditKey
		valDisp := e.envEditValue
		if e.envEditField == envFieldKey {
			keyDisp += marker
		} else {
			valDisp += marker
		}
		mode := "Editing env var:"
		if e.envEditIndex < 0 {
			mode = "Adding env var:"
		}
		return theme.Subtitle.Render(fmt.Sprintf(
			"%s\n  KEY:   %s\n  VALUE: %s\n  tab: switch field   enter: save   esc: cancel",
			mode, keyDisp, valDisp,
		))
	}
	hint := "n: new   enter: edit   d: delete   ctrl+t: switch tab"
	if e.draft != nil && len(e.draft.Env) == 0 {
		hint = "(no env vars)   n: new   ctrl+t: switch tab"
	}
	return e.envTable.View() + "\n" + theme.Subtitle.Render(hint)
}

func (e Editor) handleEnvKey(msg tea.KeyMsg) (Editor, tea.Cmd) {
	if e.envEditing {
		switch msg.String() {
		case "esc":
			return e.handleCellEscape(), nil
		case "tab", "shift+tab":
			if e.envEditField == envFieldKey {
				e.envEditField = envFieldValue
			} else {
				e.envEditField = envFieldKey
			}
			return e, nil
		case "enter":
			return e.saveEnvEdit()
		case "backspace":
			if e.envEditField == envFieldKey && len(e.envEditKey) > 0 {
				e.envEditKey = e.envEditKey[:len(e.envEditKey)-1]
			} else if e.envEditField == envFieldValue && len(e.envEditValue) > 0 {
				e.envEditValue = e.envEditValue[:len(e.envEditValue)-1]
			}
			return e, nil
		}
		if len(msg.Runes) == 1 && msg.Runes[0] >= 32 {
			if e.envEditField == envFieldKey {
				e.envEditKey += string(msg.Runes)
			} else {
				e.envEditValue += string(msg.Runes)
			}
			return e, nil
		}
		return e, nil
	}

	switch msg.String() {
	case "n":
		e.envEditing = true
		e.envEditIndex = -1
		e.envEditKey = ""
		e.envEditValue = ""
		e.envEditField = envFieldKey
		e.envSubmitError = ""
		return e, nil
	case "enter":
		return e.startEnvEdit()
	case "d":
		return e.deleteEnvRow()
	}
	updated, cmd := e.moveTableCursor(e.envTable, msg)
	e.envTable = updated
	return e, cmd
}

func (e Editor) rebuildFormAndRestoreFocus() Editor {
	focusedKey := ""
	if e.form != nil {
		if f := e.form.GetFocusedField(); f != nil {
			focusedKey = f.GetKey()
		}
	}
	e.form, e.essentialPtrs, e.fieldMap = buildForm(e.draft, e.schema, e.backendOptions, e.backendKind)
	if e.form != nil && focusedKey != "" && focusedKey != "name" {
		for i := 0; i < 20; i++ {
			if e.form.GetFocusedField().GetKey() == focusedKey {
				break
			}
			_ = e.form.NextField()
		}
	}
	return e
}

func (e Editor) forwardToForm(msg tea.Msg) (Editor, tea.Cmd) {
	if e.form == nil {
		return e, nil
	}
	updated, cmd := e.form.Update(msg)
	if f, ok := updated.(*huh.Form); ok {
		e.form = f
	}
	e = e.syncEssentials()
	if e.draft != nil && e.draft.BackendID != e.lastBackendID {
		e.lastBackendID = e.draft.BackendID
		e = e.reloadSchema()
		e = e.rebuildFormAndRestoreFocus()
	}
	if e.form != nil && e.form.State == huh.StateCompleted {
		if e.schemaError != "" {
			e.submitError = "Cannot save: " + e.schemaError
			e = e.rebuildFormAndRestoreFocus()
			return e, cmd
		}
		report := e.validator.Validate(e.CurrentDraft().ToProfileWithSchema(e.schema), e.schema)
		if report.HasBlockingErrors() {
			e.submitError = fmt.Sprintf("Cannot save: %d validation errors", len(report.Errors))
			e = e.rebuildFormAndRestoreFocus()
			return e, cmd
		}
		committed := *e.draft
		e = e.close()
		commitCmd := func() tea.Msg { return EditorCommittedMsg{Draft: committed} }
		return e, tea.Batch(cmd, commitCmd)
	}
	e.submitError = ""
	return e, cmd
}

func (e Editor) syncEssentials() Editor {
	if e.draft == nil || len(e.essentialPtrs) == 0 {
		return e
	}
	if e.draft.Essentials == nil {
		e.draft.Essentials = map[string]string{}
	}
	for long, p := range e.essentialPtrs {
		if p == nil {
			continue
		}
		e.draft.Essentials[long] = *p
	}
	return e
}

// hydrateEssentials promotes schema-driven essential flags from
// draft.Args into draft.Essentials, seeds defaults for missing fields,
// and peels the migrated keys out of Args so they do not double-render
// in the Advanced tab. The post-hydration draft is re-snapshotted so
// the dirty-check baseline matches what the user actually sees, which
// prevents a freshly-opened profile from appearing dirty just because
// defaults were seeded. Safe to call when draft, Args, or Essentials
// are nil.
func (e Editor) hydrateEssentials() Editor {
	e = e.hydrateEssentialsCore()
	if e.draft != nil {
		e.openSnapshot = *e.draft
	}
	return e
}

// hydrateEssentialsForSwitch performs the same peel/seed as
// hydrateEssentials but intentionally does NOT re-snapshot. Use when
// the user is switching backends mid-edit: the backend change itself
// is a legitimate dirty mutation and the snapshot must keep pointing
// at the pre-switch draft so esc/Cancel/discard still detect dirt.
func (e Editor) hydrateEssentialsForSwitch() Editor {
	return e.hydrateEssentialsCore()
}

// hydrateEssentialsCore is the shared peel/seed engine. For each
// registered essential field on the current backend that exists in
// the active schema:
//
//  1. Walk the candidate keys (canonical long, short, then aliases)
//     and, on the first present hit, migrate the value into
//     Essentials[spec.Long] (only if not already set, so the canonical
//     wins over later legacy aliases).
//  2. Delete every present candidate from Args so legacy duplicates
//     do not survive into the Advanced tab.
//  3. If no candidate matched and the field declares a non-empty
//     Default, seed Essentials[spec.Long] with that default.
//
// Maps are created if nil to keep the rest of the editor pipeline
// (binders, suggesters, validators) from having to nil-check.
func (e Editor) hydrateEssentialsCore() Editor {
	if e.draft == nil {
		return e
	}
	if e.draft.Essentials == nil {
		e.draft.Essentials = map[string]string{}
	}
	if e.draft.Args == nil {
		e.draft.Args = map[string]any{}
	}
	for _, f := range essentialsFor(e.backendKind, e.schema) {
		spec, ok := e.schema.Lookup(f.Flag)
		if !ok {
			continue
		}
		candidates := append([]string{spec.Long, spec.Short}, spec.Aliases...)
		found := false
		for _, key := range candidates {
			if key == "" {
				continue
			}
			v, present := e.draft.Args[key]
			if !present {
				continue
			}
			if _, set := e.draft.Essentials[spec.Long]; !set {
				e.draft.Essentials[spec.Long] = f.coerce(v)
			}
			delete(e.draft.Args, key)
			found = true
		}
		if !found {
			if _, set := e.draft.Essentials[spec.Long]; !set && f.Default != "" {
				e.draft.Essentials[spec.Long] = f.Default
			}
		}
	}
	return e
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
	e.backendKind = backend.Kind
	e = e.hydrateEssentialsForSwitch()
	var args map[string]any
	if e.draft != nil {
		args = e.draft.Args
	}
	tbl := newAdvancedTable(e.schema, args, e.backendKind, 100, 12)
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
	e.backendKind = backend.Kind
	var args map[string]any
	if e.draft != nil {
		args = e.draft.Args
	}
	tbl := newAdvancedTable(e.schema, args, e.backendKind, 100, 12)
	e.advanced = tbl
	e.advancedAll = tbl.Rows()
	if e.advancedFilter != "" {
		e.advanced.SetRows(filterRows(e.advancedAll, e.advancedFilter))
	}
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
	e.advancedEditing = false
	e.advancedEditFlag = ""
	e.advancedEditVal = ""
	e.envEditing = false
	e.envEditKey = ""
	e.envEditValue = ""
	e.envEditIndex = -1
	e.envEditField = envFieldKey
	e.envSubmitError = ""
	e.schemaError = ""
	e.submitError = ""
	return e
}

func emitCancelled() tea.Msg  { return EditorCancelledMsg{} }
func emitDiscardYes() tea.Msg { return internalDiscardYesMsg{} }
