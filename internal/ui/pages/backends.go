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
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/quantmind-br/model-loader/internal/service/configweb"
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
	New, Edit, Delete, Default, Refresh, Probe key.Binding
}

func defaultBackendsKeys() backendsKeyMap {
	return backendsKeyMap{
		New:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		Edit:    key.NewBinding(key.WithKeys("enter", "e"), key.WithHelp("enter/e", "edit")),
		Delete:  key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "del")),
		Default: key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "default")),
		Refresh: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh schema")),
		Probe:   key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "probe")),
	}
}

// BackendsPage is the master-detail page for backend catalog CRUD.
type BackendsPage struct {
	manager      *backendschema.Manager
	catalogStore backendcatalog.Store
	schemaStore  backendcatalog.SchemaStore
	list         list.Model
	keys         backendsKeyMap

	width  int
	height int

	form     *huh.Form
	formMode formMode
	draft    *backendDraft

	deleteConfirm  components.Confirm
	refreshConfirm components.Confirm

	flash        components.Flash
	spinnerModel spinner.Model

	pendingRefresh bool
	pendingProbe   bool
	probeStartTime time.Time

	defaultBackendID string

	prober       backendProberIface
	probeEpoch   int
	probeCh      <-chan backendcatalog.ProbeEvent
	probeResults map[string]backendProbeResult

	webEditing bool
	webURL     string
	webSession *configweb.Session
}

type backendsLoadedMsg struct {
	backends  []domain.Backend
	defaultID string
	err       error
}

type backendDeleteConfirmedMsg struct{ id string }
type backendRefreshConfirmedMsg struct{ id string }

// backendsReloadMsg is dispatched by Reload() when the root activates this
// tab. Reload has a value receiver and cannot mutate state, so the stale
// probe-result clearing happens in Update when this message arrives
// (mirrors the modelsReloadMsg pattern).
type backendsReloadMsg struct{}

type backendProberIface interface {
	Probe(context.Context) (<-chan backendcatalog.ProbeEvent, error)
}

type backendProbeResult struct {
	status  backendcatalog.ProbeStatus
	detail  string
	latency time.Duration
}

type probeEventMsg struct {
	event backendcatalog.ProbeEvent
	epoch int
}

// NewBackendsPage constructs the page wired to a backendschema.Manager.
func NewBackendsPage(manager *backendschema.Manager) BackendsPage {
	delegate := list.NewDefaultDelegate()
	l := list.New(nil, delegate, 0, 0)
	l.Title = "Backends"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	return BackendsPage{
		manager:      manager,
		list:         l,
		keys:         defaultBackendsKeys(),
		flash:        components.NewFlash("backends"),
		spinnerModel: components.NewLoadingSpinner(),
	}
}

// WithProber wires a backend prober for health checks.
func (p BackendsPage) WithProber(prober backendProberIface) BackendsPage {
	p.prober = prober
	return p
}

func (p BackendsPage) WithStores(catalog backendcatalog.Store, schema backendcatalog.SchemaStore) BackendsPage {
	p.catalogStore = catalog
	p.schemaStore = schema
	return p
}

func (p BackendsPage) Init() tea.Cmd {
	return tea.Batch(p.loadCmd(), p.spinnerModel.Tick)
}

// Reload refreshes the backend list on tab focus. It emits backendsReloadMsg
// so Update can drop stale probe results (probes reflect a point in time;
// a revisited tab should not present old health data as current).
func (p BackendsPage) Reload() tea.Cmd {
	return func() tea.Msg { return backendsReloadMsg{} }
}

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
		leftWidth, _ := theme.SplitTwoPanes(p.width)
		p.list.SetSize(leftWidth, m.Height)
		return p, nil
	case components.FlashClearMsg:
		p.flash, _ = p.flash.Update(m)
		return p, nil
	case backendsLoadedMsg:
		return p.handleLoaded(m)
	case backendsReloadMsg:
		p.probeResults = make(map[string]backendProbeResult)
		return p, p.loadCmd()
	case backendDeleteConfirmedMsg:
		return p.performDelete(m.id)
	case backendRefreshConfirmedMsg:
		return p.performRefresh(m.id)
	case probeEventMsg:
		return p.handleProbeEvent(m)
	case spinner.TickMsg:
		return p.handleSpinnerTick(m)
	case backendWebEditStartedMsg:
		p.webSession = m.session
		p.webURL = m.url
		return p, waitForBackendWebEdit(m.session)
	case backendWebEditDoneMsg:
		p.webEditing = false
		p.webSession = nil
		p.webURL = ""
		var fc tea.Cmd
		if m.err != nil {
			p, fc = p.withFlashError("backend edit failed: " + m.err.Error())
		} else if m.saved {
			p, fc = p.withFlash("saved backend " + m.backendID)
		} else {
			p, fc = p.withFlash("backend edit cancelled")
		}
		return p, tea.Batch(p.loadCmd(), fc)
	case backendWebEditFailedMsg:
		p.webEditing = false
		p.webSession = nil
		p.webURL = ""
		p, fc := p.withFlashError("backend edit failed: " + m.err.Error())
		return p, fc
	case tea.KeyMsg:
		return p.handleKey(m)
	}
	return p.forwardNonKey(msg)
}

func (p BackendsPage) handleLoaded(msg backendsLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		p, fc := p.withFlashError("load backends failed: " + msg.err.Error())
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
	if p.webEditing {
		if msg.String() == "esc" {
			p.cleanupBackendWebEdit()
			p.webEditing = false
			p.webSession = nil
			p.webURL = ""
			p, fc := p.withFlash("backend edit cancelled")
			return p, fc
		}
		return p, nil
	}
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
	if p.refreshConfirm.Active() {
		var cmd tea.Cmd
		p.refreshConfirm, cmd = p.refreshConfirm.Update(msg)
		return p, cmd
	}
	if p.deleteConfirm.Active() {
		var cmd tea.Cmd
		p.deleteConfirm, cmd = p.deleteConfirm.Update(msg)
		return p, cmd
	}
	return p.updateList(msg)
}

func (p BackendsPage) forwardNonKey(msg tea.Msg) (tea.Model, tea.Cmd) {
	if p.webEditing {
		return p, nil
	}
	if p.form != nil {
		return p.forwardToForm(msg)
	}
	if p.refreshConfirm.Active() {
		var cmd tea.Cmd
		p.refreshConfirm, cmd = p.refreshConfirm.Update(msg)
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

func (p BackendsPage) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if p.list.FilterState() == list.Filtering {
		updated, cmd := p.list.Update(msg)
		p.list = updated
		return p, cmd
	}
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
		if p.pendingRefresh {
			return p, nil
		}
		p.pendingRefresh = true
		return p.askRefreshSelected()
	case key.Matches(msg, p.keys.Probe):
		if p.pendingProbe {
			return p, nil
		}
		return p.askProbeAll()
	}
	updated, cmd := p.list.Update(msg)
	p.list = updated
	return p, cmd
}

func (p BackendsPage) renderWebEditModal() string {
	return "\n  Editing backend in browser…\n\n  " + p.webURL + "\n\n  Save or cancel on the page. (esc cancels)\n"
}

func (p BackendsPage) View() string {
	if p.webEditing {
		return p.renderWebEditModal()
	}
	if p.form != nil {
		return components.Modal("Backend", p.form.View(), p.width, p.height)
	}

	leftWidth, rightWidth := theme.SplitTwoPanes(p.width)
	leftContent := p.list.View()
	if len(p.list.Items()) == 0 {
		leftContent = components.EmptyState("No backends yet", "Press [n] to add one")
	}
	left := lipgloss.NewStyle().Width(leftWidth).Render(leftContent)
	rightContent := p.detailView()
	if p.pendingRefresh {
		rightContent = components.LoadingLine(p.spinnerModel, "Refreshing schema", 0) + "\n" + rightContent
	}
	if p.pendingProbe {
		rightContent = components.LoadingLine(p.spinnerModel, "Probing backends", 0) + "\n" + rightContent
	}
	right := lipgloss.NewStyle().Width(rightWidth).Render(rightContent)
	leftH := len(strings.Split(left, "\n"))
	rightH := len(strings.Split(right, "\n"))
	divH := leftH
	if rightH > divH {
		divH = rightH
	}
	divLine := lipgloss.NewStyle().Foreground(theme.ColorDim).Render("│")
	divider := strings.Repeat(divLine+"\n", divH-1) + divLine
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)

	if v := p.flash.View(); v != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, body, v)
	}
	return body
}

// OverlayView routes the delete/refresh confirms through the shared centered
// Modal overlay (the same path Profiles/Server/Benchmark use) instead of
// returning the Modal straight from View(). Returning it from View() let
// theme.ClampBody re-wrap the already-bordered box, which overflowed 80
// columns and doubled the line spacing (RENDER-01).
func (p BackendsPage) OverlayView() Overlay {
	if p.refreshConfirm.Active() {
		content := components.Modal("Refresh schema", p.refreshConfirm.View(), p.width, p.height)
		return Overlay{Content: content, Width: p.width, Height: p.height, Active: true}
	}
	if p.deleteConfirm.Active() {
		content := components.Modal("Delete backend", p.deleteConfirm.View(), p.width, p.height)
		return Overlay{Content: content, Width: p.width, Height: p.height, Active: true}
	}
	return Overlay{}
}

func (p BackendsPage) detailView() string {
	if len(p.list.Items()) == 0 {
		return components.EmptyState("No backends yet", "Press [n] to add one")
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
	probeLine := ""
	if r, ok := p.probeResults[sel.ID]; ok {
		statusStyle := theme.Subtitle
		switch r.status {
		case backendcatalog.ProbeStatusOK:
			statusStyle = theme.OK
		case backendcatalog.ProbeStatusErr:
			statusStyle = theme.Error
		}
		probeLine = "\nProbe:       " + statusStyle.Render(string(r.status))
		if r.latency > 0 {
			probeLine += " (" + r.latency.String() + ")"
		}
	}
	labelStyle := lipgloss.NewStyle().Width(13).Foreground(theme.ColorDim)

	row := func(label, value string) string {
		return lipgloss.JoinHorizontal(lipgloss.Top, labelStyle.Render(label), value)
	}

	var b strings.Builder
	if p.defaultBackendID == "" {
		b.WriteString(components.EmptyState("No default backend set", "Press [D] to set a backend as default") + "\n\n")
	}
	b.WriteString(theme.Title.Render(sel.Name) + defaultMark + "\n")
	b.WriteString(theme.Subtitle.Render(string(sel.Kind)) + "\n\n")
	b.WriteString(row("ID:", sel.ID) + "\n")
	b.WriteString(row("Kind:", string(sel.Kind)) + "\n")
	b.WriteString(row("Executable:", sel.Executable) + "\n")
	b.WriteString(row("SchemaRef:", sel.SchemaRef) + "\n")
	b.WriteString(row("Description:", desc) + "\n")
	b.WriteString(row("Tags:", tags) + "\n")
	b.WriteString(row("Created:", formatBackendTime(sel.Meta.CreatedAt)) + "\n")
	b.WriteString(row("Updated:", formatBackendTime(sel.Meta.UpdatedAt)) + "\n")
	b.WriteString(probeLine)
	return b.String()
}

func (p BackendsPage) Hints() string {
	switch {
	case p.webEditing:
		return "editing in browser…  [esc] cancel"
	case p.form != nil:
		return "[enter] submit  [esc] cancel"
	case p.refreshConfirm.Active():
		return "[←→] choose  [enter] confirm  [esc] cancel"
	case p.deleteConfirm.Active():
		return "[←→] choose  [enter] confirm  [esc] cancel"
	default:
		hints := "[e] edit  [n] new  [X] del  [D] default  [R] refresh"
		if p.prober != nil {
			hints += "  [P] probe"
		}
		hints += "  [/] filter"
		return hints
	}
}

func (p BackendsPage) IsCapturingInput() bool {
	return CaptureAny(
		func() bool { return p.form != nil },
		func() bool { return p.refreshConfirm.Active() },
		func() bool { return p.deleteConfirm.Active() },
		func() bool { return p.list.FilterState() != list.Unfiltered },
		func() bool { return p.webEditing },
	)
}

func (p BackendsPage) withFlash(msg string) (BackendsPage, tea.Cmd) {
	var cmd tea.Cmd
	p.flash, cmd = flashSuccess(p.flash, msg)
	return p, cmd
}

func (p BackendsPage) withFlashError(msg string) (BackendsPage, tea.Cmd) {
	var cmd tea.Cmd
	p.flash, cmd = flashError(p.flash, msg)
	return p, cmd
}

// StatusMessage implements ui.StatusMessageProvider: the page's flash also
// lands in the always-visible status bar, level included.
func (p BackendsPage) StatusMessage() (string, components.StatusLevel) {
	return p.flash.Current()
}

func (p BackendsPage) startAdd() (tea.Model, tea.Cmd) {
	if p.manager == nil {
		p, fc := p.withFlashError("backend manager not available")
		return p, fc
	}
	if len(p.kindOptions()) == 0 {
		p, fc := p.withFlashError("no backend generators registered")
		return p, fc
	}
	opts := p.sortedKinds()
	kind := domain.BackendKindLlamaServer
	if !p.hasKind(kind) && len(opts) > 0 {
		kind = opts[0]
	}
	d := configweb.BackendDraft{
		IsNew: true,
		Kind:  kind,
	}
	p.webEditing = true
	return p, p.startBackendWebEdit(d)
}

func (p BackendsPage) startEditSelected() (tea.Model, tea.Cmd) {
	b, ok := p.selectedBackend()
	if !ok {
		return p, nil
	}
	d := configweb.BackendDraft{
		ID:          b.ID,
		OrigID:      b.ID,
		IsNew:       false,
		Name:        b.Name,
		Kind:        b.Kind,
		Executable:  b.Executable,
		Description: b.Description,
		Tags:        b.Tags,
	}
	p.webEditing = true
	return p, p.startBackendWebEdit(d)
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
		p, fc = p.withFlashError("save backend failed: " + err.Error())
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
		"Delete",
		"Cancel",
	)
	return p, p.deleteConfirm.Init()
}

func (p BackendsPage) performDelete(id string) (tea.Model, tea.Cmd) {
	if p.manager == nil {
		p, fc := p.withFlashError("backend manager not available")
		return p, fc
	}
	var fc tea.Cmd
	if err := p.manager.DeleteBackend(id); err != nil {
		p, fc = p.withFlashError("delete failed: " + err.Error())
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
		p, fc := p.withFlashError("set default failed: " + err.Error())
		return p, fc
	}
	p.defaultBackendID = b.ID
	p, fc := p.withFlash("default backend " + b.ID)
	return p, fc
}

func (p BackendsPage) askRefreshSelected() (tea.Model, tea.Cmd) {
	b, ok := p.selectedBackend()
	if !ok {
		return p, nil
	}
	p.refreshConfirm = components.NewConfirm(
		"Refresh schema for "+b.Name+"?",
		b.ID,
		func(payload any) tea.Cmd {
			id, _ := payload.(string)
			return func() tea.Msg { return backendRefreshConfirmedMsg{id: id} }
		},
		"Refresh",
		"Cancel",
	)
	return p, p.refreshConfirm.Init()
}

func (p BackendsPage) performRefresh(id string) (tea.Model, tea.Cmd) {
	if p.manager == nil {
		p.pendingRefresh = false
		p, fc := p.withFlashError("backend manager not available")
		return p, fc
	}
	p.refreshConfirm = components.Confirm{}
	if err := p.manager.RefreshSchema(id); err != nil {
		p.pendingRefresh = false
		p, fc := p.withFlashError("refresh schema failed: " + err.Error())
		return p, fc
	}
	p.pendingRefresh = false
	p, fc := p.withFlash("schema refreshed " + id)
	return p, fc
}

func (p BackendsPage) askProbeAll() (tea.Model, tea.Cmd) {
	if p.prober == nil {
		p, fc := p.withFlashError("prober not available")
		return p, fc
	}
	p.pendingProbe = true
	p.probeStartTime = time.Now()
	p.probeEpoch++
	p.probeResults = make(map[string]backendProbeResult)
	ch, err := p.prober.Probe(context.Background())
	if err != nil {
		p.pendingProbe = false
		p, fc := p.withFlashError("probe failed: " + err.Error())
		return p, fc
	}
	p.probeCh = ch
	return p, p.readNextProbeEvent(p.probeEpoch)
}

func (p BackendsPage) readNextProbeEvent(epoch int) tea.Cmd {
	return func() tea.Msg {
		if p.probeCh == nil {
			return probeEventMsg{event: backendcatalog.ProbeEvent{Done: true}, epoch: epoch}
		}
		ev, ok := <-p.probeCh
		if !ok {
			return probeEventMsg{event: backendcatalog.ProbeEvent{Done: true}, epoch: epoch}
		}
		return probeEventMsg{event: ev, epoch: epoch}
	}
}

func (p BackendsPage) handleProbeEvent(m probeEventMsg) (tea.Model, tea.Cmd) {
	if m.epoch != p.probeEpoch {
		return p, nil
	}
	if m.event.Done {
		p.probeCh = nil
		p.pendingProbe = false
		p, fc := p.withFlash("probe complete")
		return p, fc
	}
	p.probeResults[m.event.BackendID] = backendProbeResult{
		status:  m.event.Status,
		detail:  m.event.Detail,
		latency: m.event.Latency,
	}
	return p, p.readNextProbeEvent(p.probeEpoch)
}

func (p BackendsPage) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if p.pendingProbe && time.Since(p.probeStartTime) > 3*time.Second {
		p.pendingProbe = false
		p.probeCh = nil
		p, fc := p.withFlashError("probe timed out")
		return p, fc
	}
	updated, cmd := p.spinnerModel.Update(msg)
	p.spinnerModel = updated
	return p, cmd
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
