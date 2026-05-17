package pages

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// HTTPProxyController is the subset of *httpproxy.Server consumed by the
// Server tab. Factored as an interface so tests can swap in a fake without
// pulling the real listener.
type HTTPProxyController interface {
	Start(context.Context) error
	Stop(context.Context) error
	Status() httpproxy.Status
}

type pendingAction string

const (
	pendingNone pendingAction = ""
	pendingStart pendingAction = "start"
	pendingStop  pendingAction = "stop"
)

// ServerPage renders the HTTP proxy status and lets the user toggle the
// listener on/off without leaving the TUI.
type ServerPage struct {
	srv     HTTPProxyController
	status  httpproxy.Status
	flash   components.Flash
	pending pendingAction
	width   int
	height  int
}

// NewServerPage constructs the Server tab page wired to srv.
func NewServerPage(srv HTTPProxyController) *ServerPage {
	return &ServerPage{srv: srv, flash: components.NewFlash("server")}
}

type serverTickMsg struct{}

type serverActionResultMsg struct {
	action string
	err    error
}

func (p *ServerPage) Init() tea.Cmd {
	return p.tick()
}

func (p *ServerPage) tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return serverTickMsg{} })
}

func (p *ServerPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = m.Width, m.Height
		return p, nil
	case serverTickMsg:
		if p.srv != nil {
			p.status = p.srv.Status()
		}
		return p, p.tick()
	case components.FlashClearMsg:
		p.flash, _ = p.flash.Update(m)
		return p, nil
	case serverActionResultMsg:
		p.pending = pendingNone
		msg := m.action + " ok"
		if m.err != nil {
			msg = fmt.Sprintf("%s failed: %v", m.action, m.err)
		}
		p.flash, cmd = p.flash.Set(msg)
		if p.srv != nil {
			p.status = p.srv.Status()
		}
		return p, cmd
	case tea.KeyMsg:
		switch m.String() {
		case "s":
			if p.pending != pendingNone {
				return p, nil
			}
			p.pending = pendingStart
			return p, p.startCmd()
		case "x":
			if p.pending != pendingNone {
				return p, nil
			}
			p.pending = pendingStop
			return p, p.stopCmd()
		case "r":
			if p.srv != nil {
				p.status = p.srv.Status()
			}
			return p, nil
		}
	}
	return p, nil
}

func (p *ServerPage) startCmd() tea.Cmd {
	srv := p.srv
	return func() tea.Msg {
		if srv == nil {
			return serverActionResultMsg{action: "start", err: fmt.Errorf("server not configured")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return serverActionResultMsg{action: "start", err: srv.Start(ctx)}
	}
}

func (p *ServerPage) stopCmd() tea.Cmd {
	srv := p.srv
	return func() tea.Msg {
		if srv == nil {
			return serverActionResultMsg{action: "stop", err: fmt.Errorf("server not configured")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return serverActionResultMsg{action: "stop", err: srv.Stop(ctx)}
	}
}

func (p *ServerPage) View() string {
	var b strings.Builder
	b.WriteString(theme.Title.Render("HTTP Proxy Server"))
	b.WriteString("\n\n")

	statusLine := theme.Error.Render("○ STOPPED")
	if p.status.Running {
		statusLine = theme.OK.Render("● RUNNING")
	}
	b.WriteString("Status:   ")
	b.WriteString(statusLine)
	b.WriteString("\n")

	addr := p.status.Addr
	if addr == "" {
		addr = theme.Subtitle.Render("—")
	}
	b.WriteString("Bind:     " + addr + "\n")

	profile := p.status.LoadedProfileID
	if profile == "" {
		profile = theme.Subtitle.Render("(no model loaded)")
	} else {
		profile = fmt.Sprintf("%s  pid=%d  port=%d",
			profile, p.status.LoadedPID, p.status.LoadedPort)
	}
	b.WriteString("Profile:  " + profile + "\n")

	if !p.status.LastSwapAt.IsZero() {
		b.WriteString(fmt.Sprintf("Last swap: %s ago (%dms)\n",
			truncateDuration(time.Since(p.status.LastSwapAt)),
			p.status.LastSwapDur.Milliseconds()))
	}

	if p.status.InflightRequests > 0 {
		b.WriteString(fmt.Sprintf("Inflight: %d\n", p.status.InflightRequests))
	}

	if p.status.LastError != "" {
		b.WriteString("\n")
		b.WriteString(theme.Error.Render("Last error: " + p.status.LastError))
		if !p.status.LastErrorAt.IsZero() {
			b.WriteString(theme.Subtitle.Render(
				fmt.Sprintf("  (%s ago)", truncateDuration(time.Since(p.status.LastErrorAt)))))
		}
		b.WriteString("\n")
	}

	if p.pending != pendingNone {
		b.WriteString("\n")
		text := "Starting…"
		if p.pending == pendingStop {
			text = "Stopping…"
		}
		b.WriteString(theme.Subtitle.Render(text))
		b.WriteString("\n")
	} else if p.srv == nil {
		b.WriteString("\n")
		b.WriteString(theme.Subtitle.Render("Server not configured"))
		b.WriteString("\n")
	} else if !p.status.Running {
		b.WriteString("\n")
		b.WriteString(theme.Subtitle.Render("Press [s] to start the HTTP proxy"))
		b.WriteString("\n")
	}

	if v := p.flash.View(); v != "" {
		b.WriteString("\n")
		b.WriteString(v)
		b.WriteString("\n")
	}

	body := b.String()
	if p.width == 0 || p.height == 0 {
		return body
	}
	return lipgloss.Place(p.width, p.height, lipgloss.Left, lipgloss.Top, body)
}

// Hints publishes page-local keybindings. RootModel concatenates these with
// globalHints into the status bar.
func (p *ServerPage) Hints() string {
	return "[s] start  [x] stop  [r] refresh"
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
