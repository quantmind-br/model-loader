// Package pages holds tab page implementations.
package pages

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

type formMode int

const (
	formModeNone formMode = iota
	formModeAdd
	formModeEdit
)

type backendDraft struct {
	ID          string
	Name        string
	Kind        string
	Executable  string
	Description string
	Tags        string
}

type backendItem struct{ backend domain.Backend }

func (i backendItem) Title() string       { return i.backend.Name }
func (i backendItem) Description() string { return i.backend.ID }
func (i backendItem) FilterValue() string {
	return strings.Join([]string{i.backend.Name, i.backend.ID, string(i.backend.Kind), i.backend.Executable, strings.Join(i.backend.Tags, " ")}, " ")
}

type backendsKeyMap struct {
	New, Edit, Delete, Default, Refresh key.Binding
}

func defaultBackendsKeys() backendsKeyMap {
	return backendsKeyMap{
		New:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		Edit:    key.NewBinding(key.WithKeys("enter", "e"), key.WithHelp("enter/e", "edit")),
		Delete:  key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "del")),
		Default: key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "default")),
		Refresh: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh schema")),
	}
}

// BackendsPage is the master-detail page for backend catalog CRUD.
type BackendsPage struct {
	manager *backendschema.Manager
	list    list.Model
	keys    backendsKeyMap

	width  int
	height int

	form     *huh.Form
	formMode formMode
	draft    *backendDraft

	deleteConfirm components.Confirm

	flash   string
	flashAt time.Time

	defaultBackendID string
}

type backendsLoadedMsg struct {
	backends  []domain.Backend
	defaultID string
	err       error
}

type backendDeleteConfirmedMsg struct{ id string }

// NewBackendsPage constructs the page wired to a backendschema.Manager.
func NewBackendsPage(manager *backendschema.Manager) BackendsPage {
	delegate := list.NewDefaultDelegate()
	l := list.New(nil, delegate, 0, 0)
	l.Title = "Backends"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)

	return BackendsPage{
		manager: manager,
		list:    l,
		keys:    defaultBackendsKeys(),
	}
}

func (p BackendsPage) Init() tea.Cmd { return p.loadCmd() }

func (p BackendsPage) Reload() tea.Cmd { return p.loadCmd() }

func (p BackendsPage) loadCmd() tea.Cmd {
	return func() tea.Msg {
		if p.manager == nil {
			return backendsLoadedMsg{err: fmt.Errorf("backend manager not available")}
		}
		backends, err := p.manager.ListBackends()
		if err != nil {
			return backendsLoadedMsg{err: err}
		}
		defaultID, err := p.manager.DefaultBackendID()
		if err != nil {
			return backendsLoadedMsg{err: err}
		}
		return backendsLoadedMsg{backends: backends, defaultID: defaultID}
	}
}

func (p BackendsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = m.Width, m.Height
		p.list.SetSize(m.Width/3, m.Height-2)
		return p, nil
	case flashClearMsg:
		if m.tag == "backends" && m.at.Equal(p.flashAt) {
			p.flash = ""
			p.flashAt = time.Time{}
		}
		return p, nil
	case backendsLoadedMsg:
		return p.handleLoaded(m)
	case backendDeleteConfirmedMsg:
		return p.performDelete(m.id)
	case tea.KeyMsg:
		return p.handleKey(m)
	}
	return p.forwardNonKey(msg)
}

func (p BackendsPage) handleLoaded(msg backendsLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		p, fc := p.withFlash("load backends failed: " + msg.err.Error())
		return p, fc
	}
	items := make([]list.Item, 0, len(msg.backends))
	for _, b := range msg.backends {
		items = append(items, backendItem{backend: b})
	}
	p.list.SetItems(items)
	p.defaultBackendID = msg.defaultID
	return p, nil
}

func (p BackendsPage) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if p.form != nil {
		if msg.String() == "esc" {
			p.form = nil
			p.formMode = formModeNone
			p.draft = nil
			p, fc := p.withFlash("backend form cancelled")
			return p, fc
		}
		return p.forwardToForm(msg)
	}
	if p.deleteConfirm.Active() {
		if msg.String() == "esc" {
			p.deleteConfirm = components.Confirm{}
			p, fc := p.withFlash("delete cancelled")
			return p, fc
		}
		var cmd tea.Cmd
		p.deleteConfirm, cmd = p.deleteConfirm.Update(msg)
		return p, cmd
	}
	return p.updateList(msg)
}

func (p BackendsPage) forwardNonKey(msg tea.Msg) (tea.Model, tea.Cmd) {
	if p.form != nil {
		return p.forwardToForm(msg)
	}
	if p.deleteConfirm.Active() {
		var cmd tea.Cmd
		p.deleteConfirm, cmd = p.deleteConfirm.Update(msg)
		return p, cmd
	}
	return p, nil
}

func (p BackendsPage) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, p.keys.New):
		return p.startAdd()
	case key.Matches(msg, p.keys.Edit):
		return p.startEditSelected()
	case key.Matches(msg, p.keys.Delete):
		return p.askDeleteSelected()
	case key.Matches(msg, p.keys.Default):
		return p.setDefaultSelected()
	case key.Matches(msg, p.keys.Refresh):
		return p.refreshSelected()
	}
	updated, cmd := p.list.Update(msg)
	p.list = updated
	return p, cmd
}

func (p BackendsPage) View() string {
	if p.form != nil {
		return components.Modal("Backend", p.form.View(), p.width, p.height)
	}
	if p.deleteConfirm.Active() {
		return components.Modal("Confirm", p.deleteConfirm.View(), p.width, p.height)
	}

	leftWidth := p.width / 3
	rightWidth := (p.width * 2 / 3) - 2
	if leftWidth <= 0 {
		leftWidth = 40
	}
	if rightWidth <= 0 {
		rightWidth = 80
	}
	left := theme.Pane.Width(leftWidth).Render(p.list.View())
	right := theme.Pane.Width(rightWidth).Render(p.detailView())
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

func (p BackendsPage) detailView() string {
	if len(p.list.Items()) == 0 {
		return theme.Subtitle.Render("No backends yet. Press [n] to add one.")
	}
	sel, ok := p.selectedBackend()
	if !ok {
		return ""
	}
	defaultMark := ""
	if sel.ID == p.defaultBackendID {
		defaultMark = " " + theme.OK.Render("default")
	}
	tags := strings.Join(sel.Tags, ", ")
	if tags == "" {
		tags = "(none)"
	}
	desc := sel.Description
	if desc == "" {
		desc = "(none)"
	}
	return fmt.Sprintf(
		"%s%s\n%s\n\nID:          %s\nKind:        %s\nExecutable:  %s\nSchemaRef:   %s\nDescription: %s\nTags:        %s\nCreated:     %s\nUpdated:     %s",
		theme.Title.Render(sel.Name),
		defaultMark,
		theme.Subtitle.Render(string(sel.Kind)),
		sel.ID,
		sel.Kind,
		sel.Executable,
		sel.SchemaRef,
		desc,
		tags,
		formatBackendTime(sel.Meta.CreatedAt),
		formatBackendTime(sel.Meta.UpdatedAt),
	)
}

func (p BackendsPage) Hints() string {
	switch {
	case p.form != nil:
		return "[enter] submit  [esc] cancel"
	case p.deleteConfirm.Active():
		return "[←→] choose  [enter] confirm  [esc] cancel"
	default:
		return "[enter/e] edit  [n] new  [x] del  [D] default  [R] refresh schema  [/] filter"
	}
}

func (p BackendsPage) IsCapturingInput() bool {
	return p.form != nil || p.deleteConfirm.Active()
}

func (p BackendsPage) withFlash(msg string) (BackendsPage, tea.Cmd) {
	p.flash = msg
	p.flashAt = time.Now()
	return p, scheduleFlashClear("backends", p.flashAt)
}

func (p BackendsPage) startAdd() (tea.Model, tea.Cmd) {
	if p.manager == nil {
		p, fc := p.withFlash("backend manager not available")
		return p, fc
	}
	if len(p.kindOptions()) == 0 {
		p, fc := p.withFlash("no backend generators registered")
		return p, fc
	}
	p.formMode = formModeAdd
	p.draft = &backendDraft{Kind: string(domain.BackendKindLlamaServer)}
	if p.draft.Kind == "" || !p.hasKind(domain.BackendKind(p.draft.Kind)) {
		opts := p.sortedKinds()
		if len(opts) > 0 {
			p.draft.Kind = string(opts[0])
		}
	}
	p.form = p.buildAddForm()
	return p, p.form.Init()
}

func (p BackendsPage) startEditSelected() (tea.Model, tea.Cmd) {
	b, ok := p.selectedBackend()
	if !ok {
		return p, nil
	}
	p.formMode = formModeEdit
	p.draft = &backendDraft{
		ID:          b.ID,
		Name:        b.Name,
		Kind:        string(b.Kind),
		Executable:  b.Executable,
		Description: b.Description,
		Tags:        strings.Join(b.Tags, ", "),
	}
	p.form = p.buildEditForm()
	return p, p.form.Init()
}

func (p BackendsPage) buildAddForm() *huh.Form {
	return huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Name").Value(&p.draft.Name).Validate(required("name")),
		huh.NewSelect[string]().Title("Kind").Options(p.kindOptions()...).Value(&p.draft.Kind),
		huh.NewInput().Title("Executable").Value(&p.draft.Executable).Validate(required("executable")),
		huh.NewInput().Title("Description").Value(&p.draft.Description),
		huh.NewInput().Title("Tags").Description("comma-separated").Value(&p.draft.Tags),
	)).WithShowHelp(true)
}

func (p BackendsPage) buildEditForm() *huh.Form {
	return huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Name").Value(&p.draft.Name).Validate(required("name")),
		huh.NewInput().Title("Executable").Value(&p.draft.Executable).Validate(required("executable")),
		huh.NewInput().Title("Description").Value(&p.draft.Description),
		huh.NewInput().Title("Tags").Description("comma-separated").Value(&p.draft.Tags),
		huh.NewNote().Title("Kind").Description(p.draft.Kind),
	)).WithShowHelp(true)
}

func (p BackendsPage) forwardToForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if p.form == nil {
		return p, nil
	}
	updated, cmd := p.form.Update(msg)
	if f, ok := updated.(*huh.Form); ok {
		p.form = f
	}
	if p.form != nil && p.form.State == huh.StateCompleted {
		return p.commitForm(cmd)
	}
	return p, cmd
}

func (p BackendsPage) commitForm(formCmd tea.Cmd) (tea.Model, tea.Cmd) {
	if p.draft == nil {
		p.form = nil
		p.formMode = formModeNone
		return p, formCmd
	}
	d := *p.draft
	mode := p.formMode
	p.form = nil
	p.formMode = formModeNone
	p.draft = nil

	var err error
	var b domain.Backend
	switch mode {
	case formModeAdd:
		b, err = p.manager.AddBackend(context.Background(), d.Name, d.Executable, domain.BackendKind(d.Kind))
		if err == nil && (d.Description != "" || d.Tags != "") {
			b, err = p.manager.UpdateBackend(b.ID, domain.Backend{Name: b.Name, Executable: b.Executable, Description: d.Description, Tags: parseTags(d.Tags)})
		}
	case formModeEdit:
		b, err = p.manager.UpdateBackend(d.ID, domain.Backend{Name: d.Name, Executable: d.Executable, Description: d.Description, Tags: parseTags(d.Tags)})
	}

	var fc tea.Cmd
	if err != nil {
		p, fc = p.withFlash("save backend failed: " + err.Error())
		return p, tea.Batch(formCmd, fc)
	}
	p, fc = p.withFlash("saved backend " + b.ID)
	return p, tea.Batch(formCmd, p.loadCmd(), fc)
}

func (p BackendsPage) askDeleteSelected() (tea.Model, tea.Cmd) {
	b, ok := p.selectedBackend()
	if !ok {
		return p, nil
	}
	p.deleteConfirm = components.NewConfirm(
		"Delete backend "+b.Name+"?",
		b.ID,
		func(payload any) tea.Cmd {
			id, _ := payload.(string)
			return func() tea.Msg { return backendDeleteConfirmedMsg{id: id} }
		},
	)
	return p, p.deleteConfirm.Init()
}

func (p BackendsPage) performDelete(id string) (tea.Model, tea.Cmd) {
	if p.manager == nil {
		p, fc := p.withFlash("backend manager not available")
		return p, fc
	}
	var fc tea.Cmd
	if err := p.manager.DeleteBackend(id); err != nil {
		p, fc = p.withFlash("delete failed: " + err.Error())
	} else {
		p, fc = p.withFlash("deleted " + id)
	}
	p.deleteConfirm = components.Confirm{}
	return p, tea.Batch(p.loadCmd(), fc)
}

func (p BackendsPage) setDefaultSelected() (tea.Model, tea.Cmd) {
	b, ok := p.selectedBackend()
	if !ok {
		return p, nil
	}
	if err := p.manager.SetDefaultBackend(b.ID); err != nil {
		p, fc := p.withFlash("set default failed: " + err.Error())
		return p, fc
	}
	p.defaultBackendID = b.ID
	p, fc := p.withFlash("default backend " + b.ID)
	return p, fc
}

func (p BackendsPage) refreshSelected() (tea.Model, tea.Cmd) {
	b, ok := p.selectedBackend()
	if !ok {
		return p, nil
	}
	if err := p.manager.RefreshSchema(b.ID); err != nil {
		p, fc := p.withFlash("refresh schema failed: " + err.Error())
		return p, fc
	}
	p, fc := p.withFlash("schema refreshed " + b.ID)
	return p, fc
}

func (p BackendsPage) selectedBackend() (domain.Backend, bool) {
	item, ok := p.list.SelectedItem().(backendItem)
	if !ok {
		return domain.Backend{}, false
	}
	return item.backend, true
}

func (p BackendsPage) kindOptions() []huh.Option[string] {
	kinds := p.sortedKinds()
	opts := make([]huh.Option[string], 0, len(kinds))
	for _, kind := range kinds {
		opts = append(opts, huh.NewOption(string(kind), string(kind)))
	}
	return opts
}

func (p BackendsPage) sortedKinds() []domain.BackendKind {
	if p.manager == nil {
		return nil
	}
	kinds := make([]domain.BackendKind, 0, len(p.manager.Generators()))
	for kind := range p.manager.Generators() {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	return kinds
}

func (p BackendsPage) hasKind(kind domain.BackendKind) bool {
	if p.manager == nil {
		return false
	}
	_, ok := p.manager.Generators()[kind]
	return ok
}

func required(name string) func(string) error {
	return func(v string) error {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("%s is required", name)
		}
		return nil
	}
}

func parseTags(s string) []string {
	parts := strings.Split(s, ",")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			tags = append(tags, part)
		}
	}
	return tags
}

func formatBackendTime(t time.Time) string {
	if t.IsZero() {
		return "(unknown)"
	}
	return t.Format(time.RFC3339)
}
