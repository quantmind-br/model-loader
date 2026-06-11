package pages

import (
	"context"
	"io"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// procMgrIface is the slice of processmgr.Manager that ServerPage needs.
// The TUI process never launches instances itself anymore — the detached
// proxy process does — so this surface is observe + orphan cleanup only.
type procMgrIface interface {
	List() []domain.RunningInstance
	Kill(pid int) error
	TailLogs(pid int) (io.ReadCloser, error)
	History() []domain.ExitedInstance
	// RefreshFromDisk re-reads instances.json without writing it back —
	// the TUI is an observer; the proxy process owns the registry.
	RefreshFromDisk() error
}

// serverProxyController is the slice of *proxysupervisor.Supervisor the
// Server page drives: the proxy panel lifecycle (Start/Stop/Status) plus
// the backend swap endpoints used by restart and kill-loaded.
type serverProxyController interface {
	components.HTTPProxyController // Start, Stop, Status
	Load(ctx context.Context, profileID string) (httpproxy.Status, error)
	Unload(ctx context.Context, force bool) (httpproxy.Status, error)
	BaseURL() string
	EnsureRunning(context.Context) error
}

// profileStoreIface is the subset of profilestore.Store used by ServerPage
// to implement `r` (real restart). nil -> `r` falls back to kill-only mode.
type profileStoreIface interface {
	Get(id string) (domain.Profile, error)
}

type ServerPage struct {
	pm                 procMgrIface
	mm                 monitor.Manager
	ps                 profileStoreIface // injected for `r` real restart (slice 6 / Task 4)
	pendingSelectPID   int               // set by ServerSelectPIDMsg, consumed after the next refresh
	tbl                table.Model
	subs               map[int]*subState
	chans              map[int]<-chan monitor.MonitorEvent
	subView            SubViewKind
	history            []domain.ExitedInstance
	paused             bool
	periodicTickActive bool
	width              int
	height             int
	flash              components.Flash
	restartConfirm     components.Confirm
	killConfirm        components.Confirm

	historyChart *components.HistoryChart
	metricsDir   string
	proxy        *components.ProxyPanel
	proxyCtl     serverProxyController
}

const (
	colPID          = 8
	colPort         = 6
	colProfile      = 18
	colUptime       = 10
	colVRAM         = 12
	colTokensPerSec = 10
)

func NewServerPage(pm procMgrIface, mm monitor.Manager, ps profileStoreIface) *ServerPage {
	cols := []table.Column{
		{Title: "PID", Width: colPID},
		{Title: "Port", Width: colPort},
		{Title: "Profile", Width: colProfile},
		{Title: "Uptime", Width: colUptime},
		{Title: "VRAM", Width: colVRAM},
		{Title: "Tokens/s", Width: colTokensPerSec},
	}
	t := table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(8))
	return &ServerPage{
		pm:    pm,
		mm:    mm,
		ps:    ps,
		tbl:   t,
		subs:  map[int]*subState{},
		chans: map[int]<-chan monitor.MonitorEvent{},
		flash: components.NewFlash("monitor"),
	}
}

func (p *ServerPage) SetSize(w, h int) {
	p.width, p.height = w, h
	p.tbl.SetWidth(w)
}

func (p *ServerPage) WithMetricsDir(dir string) *ServerPage {
	p.metricsDir = dir
	return p
}

func (p *ServerPage) WithProxy(srv serverProxyController) *ServerPage {
	p.proxyCtl = srv
	p.proxy = components.NewProxyPanel(srv)
	return p
}

func (p *ServerPage) Init() tea.Cmd {
	p.periodicTickActive = true
	cmds := []tea.Cmd{p.refreshInstancesCmd(), p.periodicTickCmd()}
	if p.proxy != nil {
		cmds = append(cmds, p.proxy.Init())
	}
	return tea.Batch(cmds...)
}

// IsCapturingInput tells the root model when the page owns global keys.
// The history chart claims input so its 1/2/3/4 time-window keys reach the
// page instead of triggering the global tab switch (ROUTE-01).
func (p *ServerPage) IsCapturingInput() bool {
	return CaptureAny(
		func() bool { return p.killConfirm.Active() },
		func() bool { return p.restartConfirm.Active() },
		func() bool { return p.historyChart != nil },
	)
}

func (p *ServerPage) View() string {
	var body string
	if p.historyChart != nil {
		body = p.renderTable() + "\n" + p.historyChart.View()
	} else if len(p.tbl.Rows()) == 0 {
		header := theme.Title.Render("Running instances")
		if p.flash.Message() != "" {
			header = p.flash.View() + "\n" + header
		}
		body = header + "\n" + components.EmptyState("No instances running", "Switch to Profiles [1] to start one")
	} else {
		body = p.renderTable() + "\n" + p.renderStatusLine() + "\n" + p.renderSubViewBody()
	}

	if p.proxy != nil {
		proxyView := p.proxy.View()
		if proxyView != "" {
			body = proxyView + "\n" + body
		}
	}
	return body
}

func (p *ServerPage) OverlayView() Overlay {
	// Route confirms through the centered Modal box (the shared overlay path
	// used by Profiles, Backends, and Benchmark) so the dialog renders as a
	// full-canvas opaque frame instead of a bare huh form. The bare form left
	// page text bleeding around its short button row when composited by
	// components.Overlay (RENDER-01/02/03).
	if p.killConfirm.Active() {
		content := components.Modal("Kill instance", p.killConfirm.View(), p.width, p.height)
		return Overlay{Content: content, Width: p.width, Height: p.height, Active: true}
	}
	if p.restartConfirm.Active() {
		content := components.Modal("Restart instance", p.restartConfirm.View(), p.width, p.height)
		return Overlay{Content: content, Width: p.width, Height: p.height, Active: true}
	}
	return Overlay{}
}

// StatusMessage implements ui.StatusMessageProvider: the page's primary
// flash (restart/kill/unload feedback) also lands in the always-visible
// status bar, level included. The proxy panel keeps its own in-panel flash;
// the page-level flash is the one surfaced here.
func (p *ServerPage) StatusMessage() (string, components.StatusLevel) {
	return p.flash.Current()
}

// Hints implements ui.HintProvider for the Server tab.
func (p *ServerPage) Hints() string {
	var hints string
	if p.killConfirm.Active() || p.restartConfirm.Active() {
		hints = "[←→] choose  [enter] confirm  [esc] cancel"
	} else if p.historyChart != nil {
		hints = "[1] 1h  [2] 6h  [3] 24h  [4] 7d  [esc] close"
	} else {
		hints = "[v] cycle view  [Space] pause  [K] kill  [R] restart  [h] history"
	}
	if p.proxy != nil {
		if ph := p.proxy.Hints(); ph != "" {
			if hints != "" {
				hints = ph + "  " + hints
			} else {
				hints = ph
			}
		}
	}
	return hints
}
