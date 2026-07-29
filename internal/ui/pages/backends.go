// Package pages holds tab page implementations.
package pages

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/quantmind-br/model-loader/internal/service/configweb"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

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
	probeCancel  context.CancelFunc

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

// NewBackendsPage constructs the page wired to a backendschema.Manager.
func NewBackendsPage(manager *backendschema.Manager) BackendsPage {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)
	l := list.New(nil, delegate, 0, 0)
	l.Title = "Backends"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(true)
	return BackendsPage{
		manager:      manager,
		list:         l,
		keys:         defaultBackendsKeys(),
		flash:        components.NewFlash("backends"),
		spinnerModel: components.NewLoadingSpinner(),
	}
}

func (p BackendsPage) WithStores(catalog backendcatalog.Store, schema backendcatalog.SchemaStore) BackendsPage {
	p.catalogStore = catalog
	p.schemaStore = schema
	return p
}

func (p BackendsPage) Init() tea.Cmd {
	// Spinner tick is armed only when probe/refresh work begins (audit N-P5);
	// an always-on tick loop wastes wakeups while the tab is idle.
	return p.loadCmd()
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
		mode, listW, _ := theme.ResponsiveSplit(p.width)
		listH := m.Height - 2
		if mode == theme.LayoutStacked {
			listH = (m.Height - 2) / 2
		}
		p.list.SetSize(listW, max(3, listH-1))
		return p, nil
	case components.FlashClearMsg:
		p.flash, _ = p.flash.Update(m)
		return p, nil
	case backendsLoadedMsg:
		return p.handleLoaded(m)
	case backendsReloadMsg:
		// Drop stale probe state wholesale: bumping the epoch makes any
		// in-flight probe's late events no-ops (same guard startProbe uses),
		// so a probe started before the tab switch can't repopulate the
		// freshly cleared results with a partial set.
		p.probeResults = make(map[string]backendProbeResult)
		p.probeEpoch++
		p.probeCh = nil
		p.pendingProbe = false
		if p.probeCancel != nil {
			p.probeCancel() // stop the producer goroutine (audit N-C14)
			p.probeCancel = nil
		}
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
		return p.handleWebEditStarted(m)
	case backendWebEditDoneMsg:
		return p.handleWebEditDone(m)
	case backendWebEditFailedMsg:
		return p.handleWebEditFailed(m)
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
		return p.handleWebEditKey(msg)
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
		model, cmd := p.askRefreshSelected()
		return model, tea.Batch(cmd, p.spinnerModel.Tick)
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

func (p BackendsPage) Hints() string {
	switch {
	case p.webEditing:
		return "editing in browser…  [esc] cancel"
	case p.form != nil:
		return "[enter] submit  [esc] cancel"
	case p.refreshConfirm.Active():
		return components.ConfirmHints
	case p.deleteConfirm.Active():
		return components.ConfirmHints
	case p.list.FilterState() == list.Filtering:
		return filteringHints
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
