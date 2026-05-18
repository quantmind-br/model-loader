package components

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// HTTPProxyController is the subset of *proxysupervisor.Supervisor consumed
// by ProxyPanel. Factored as an interface so tests can swap in a fake.
type HTTPProxyController interface {
	Start(context.Context) error
	Stop(context.Context) error
	Status() httpproxy.Status
}

type pendingAction string

const (
	pendingNone  pendingAction = ""
	pendingStart pendingAction = "start"
	pendingStop  pendingAction = "stop"
)

// ProxyTickMsg is delivered every 1 s by the panel's own tick to poll
// the proxy status.
type ProxyTickMsg struct{}

// ProxyActionResultMsg carries the outcome of a Start or Stop command.
type ProxyActionResultMsg struct {
	action string
	err    error
}

// ProxyPanel is a self-contained widget that renders the HTTP proxy status
// line(s) and owns the Start/Stop action state. It does not draw a frame —
// the owning page composes it above whatever content follows.
type ProxyPanel struct {
	srv    HTTPProxyController
	status httpproxy.Status
	pending pendingAction
	flash   Flash
	width   int
}

// NewProxyPanel constructs a ProxyPanel wired to srv. srv may be nil.
func NewProxyPanel(srv HTTPProxyController) *ProxyPanel {
	p := &ProxyPanel{srv: srv, flash: NewFlash("proxy")}
	if srv != nil {
		p.status = srv.Status()
	}
	return p
}

// Init schedules the 1-second status tick. The owning page batches this
// with its own commands.
func (p *ProxyPanel) Init() tea.Cmd {
	return p.tick()
}

func (p *ProxyPanel) tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return ProxyTickMsg{} })
}

// Update handles ProxyTickMsg, ProxyActionResultMsg, and the s / x keys.
// It returns (cmd, consumed). consumed=true means the key was claimed by
// the panel; the owning page must NOT also act on it.
func (p *ProxyPanel) Update(msg tea.Msg) (tea.Cmd, bool) {
	var cmd tea.Cmd
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = m.Width
		return nil, false
	case ProxyTickMsg:
		if p.srv != nil {
			p.status = p.srv.Status()
		}
		return p.tick(), false
	case FlashClearMsg:
		p.flash, _ = p.flash.Update(m)
		return nil, false
	case ProxyActionResultMsg:
		p.pending = pendingNone
		msg := m.action + " ok"
		if m.err != nil {
			msg = fmt.Sprintf("%s failed: %v", m.action, m.err)
		}
		p.flash, cmd = p.flash.Set(msg)
		if p.srv != nil {
			p.status = p.srv.Status()
		}
		return cmd, false
	case tea.KeyMsg:
		switch m.String() {
		case "s":
			if p.pending != pendingNone {
				return nil, true
			}
			p.pending = pendingStart
			return p.startCmd(), true
		case "x":
			if p.pending != pendingNone {
				return nil, true
			}
			p.pending = pendingStop
			return p.stopCmd(), true
		}
	}
	return nil, false
}

func (p *ProxyPanel) startCmd() tea.Cmd {
	srv := p.srv
	return func() tea.Msg {
		if srv == nil {
			return ProxyActionResultMsg{action: "start", err: fmt.Errorf("server not configured")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return ProxyActionResultMsg{action: "start", err: srv.Start(ctx)}
	}
}

func (p *ProxyPanel) stopCmd() tea.Cmd {
	srv := p.srv
	return func() tea.Msg {
		if srv == nil {
			return ProxyActionResultMsg{action: "stop", err: fmt.Errorf("server not configured")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return ProxyActionResultMsg{action: "stop", err: srv.Stop(ctx)}
	}
}

// View renders the compact panel (1–3 lines). Returns "" when srv is nil.
func (p *ProxyPanel) View() string {
	if p.srv == nil {
		return ""
	}

	var b strings.Builder

	// Line 1: status + addr + profile
	statusLine := theme.Error.Render("○ STOPPED")
	if p.status.Running {
		statusLine = theme.OK.Render("● RUNNING")
	}
	addr := p.status.Addr
	if addr == "" {
		addr = theme.Subtitle.Render("—")
	}
	profile := p.status.LoadedProfileID
	if profile == "" {
		profile = theme.Subtitle.Render("—")
	}
	b.WriteString(fmt.Sprintf("HTTP Proxy: %s  %s  profile=%s", statusLine, addr, profile))
	b.WriteString("\n")

	// Line 2: last swap, inflight, last error (conditional)
	var line2Parts []string
	if !p.status.LastSwapAt.IsZero() {
		line2Parts = append(line2Parts, fmt.Sprintf("Last swap: %s ago (%dms)",
			truncateDuration(time.Since(p.status.LastSwapAt)),
			p.status.LastSwapDur.Milliseconds()))
	}
	if p.status.InflightRequests > 0 {
		line2Parts = append(line2Parts, fmt.Sprintf("Inflight: %d", p.status.InflightRequests))
	}
	if p.status.LastError != "" {
		line2Parts = append(line2Parts, theme.Error.Render("⚠ Last error: "+p.status.LastError))
	}
	if len(line2Parts) > 0 {
		b.WriteString(strings.Join(line2Parts, "   "))
		b.WriteString("\n")
	}

	// Line 3: pending or flash
	if p.pending != pendingNone {
		text := "Starting…"
		if p.pending == pendingStop {
			text = "Stopping…"
		}
		b.WriteString(theme.Subtitle.Render(text))
		b.WriteString("\n")
	} else if v := p.flash.View(); v != "" {
		b.WriteString(v)
		b.WriteString("\n")
	}

	return b.String()
}

// SetWidth resizes the panel for proper truncation/alignment.
func (p *ProxyPanel) SetWidth(w int) {
	p.width = w
}

// Hints returns the proxy-only key hints for inclusion in the owning page's
// HintProvider output.
func (p *ProxyPanel) Hints() string {
	return "[s] start  [x] stop"
}

// Status is exposed for the owning page to read (e.g. for tests).
func (p *ProxyPanel) Status() httpproxy.Status {
	return p.status
}

// truncateDuration rounds a duration to the most useful unit for status display.
func truncateDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}
