// Playground modal is currently inert and unwired — input has no cursor
// handling and p.response is never populated by a real stream. The dead
// Ctrl+P toggle and playground fields were removed from root.go (DEAD-01);
// to revive this, add streaming wiring here and re-introduce a keybinding
// (and overlay) in the root model.
package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

type PlaygroundModal struct {
	active           bool
	instances        []domain.RunningInstance
	selectedInstance int
	input            string
	response         string
	temperature      float64
	topP             float64
	maxTokens        int
	history          []playgroundMsg
	streaming        bool
	width            int
	height           int
}

type playgroundMsg struct {
	role    string
	content string
}

func NewPlaygroundModal(instances []domain.RunningInstance) PlaygroundModal {
	return PlaygroundModal{
		active:      true,
		instances:   instances,
		temperature: 0.7,
		topP:        0.9,
		maxTokens:   2048,
	}
}

func (p PlaygroundModal) Active() bool           { return p.active }
func (p PlaygroundModal) IsCapturingInput() bool { return p.active }

func (p PlaygroundModal) Init() tea.Cmd { return nil }

func (p PlaygroundModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if !p.active {
		return p, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "ctrl+p":
			p.active = false
			return p, nil
		case "ctrl+c":
			p.active = false
			p.streaming = false
			return p, nil
		case "esc":
			if p.streaming {
				p.streaming = false
				return p, nil
			}
			p.active = false
			return p, nil
		case "c":
			p.history = nil
			p.response = ""
			p.input = ""
			return p, nil
		case "tab":
			if len(p.instances) > 0 {
				p.selectedInstance = (p.selectedInstance + 1) % len(p.instances)
			}
			return p, nil
		case "enter":
			if p.input != "" {
				p.history = append(p.history, playgroundMsg{role: "user", content: p.input})
				p.response = ""
				p.streaming = true
				p.input = ""
			}
			return p, nil
		case "backspace":
			if len(p.input) > 0 {
				p.input = p.input[:len(p.input)-1]
			}
			return p, nil
		default:
			if len(km.Runes) > 0 {
				p.input += string(km.Runes)
			}
			return p, nil
		}
	}
	return p, nil
}

func (p PlaygroundModal) View() string {
	if !p.active {
		return ""
	}
	var b strings.Builder
	b.WriteString(theme.Title.Render("Playground") + "\n\n")

	b.WriteString(theme.Subtitle.Render("Instance") + "\n")
	for i, inst := range p.instances {
		marker := "  "
		if i == p.selectedInstance {
			marker = lipgloss.NewStyle().Foreground(theme.ColorAccent).Render("> ")
		}
		b.WriteString(fmt.Sprintf("%s%s (pid=%d port=%d)\n", marker, inst.ProfileID, inst.PID, inst.Port))
	}
	b.WriteString("\n")

	b.WriteString(theme.Subtitle.Render("Parameters") + "\n")
	b.WriteString(fmt.Sprintf("temp=%.2f top_p=%.2f max_tokens=%d\n\n", p.temperature, p.topP, p.maxTokens))

	b.WriteString(theme.Subtitle.Render("Conversation") + "\n")
	for _, m := range p.history {
		b.WriteString(fmt.Sprintf("%s: %s\n", m.role, m.content))
	}
	if p.streaming {
		b.WriteString("assistant: " + p.response + "…\n")
	}
	b.WriteString("\n")

	b.WriteString(theme.Subtitle.Render("Input") + "\n")
	b.WriteString(p.input + "_\n\n")
	b.WriteString("[enter] send  [c] clear  [tab] instance  [esc] close")

	style := lipgloss.NewStyle().Width(p.width - 4).Height(p.height - 4).Padding(1)
	return modalBoxStyle().Render(style.Render(b.String()))
}
