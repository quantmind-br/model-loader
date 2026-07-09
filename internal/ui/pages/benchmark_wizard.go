package pages

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// openWizard starts the unified run wizard at the profile-pick step. It
// replaces the old two-screen profile→mode flow with a single bvWizard view
// that the user can move forward AND back through.
func (p BenchmarkPage) openWizard() (tea.Model, tea.Cmd) {
	if p.runner == nil {
		var cmd tea.Cmd
		p, cmd = p.withFlashError("benchmark engine unavailable")
		return p, cmd
	}
	profiles, err := p.store.List()
	if err != nil {
		var cmd tea.Cmd
		p, cmd = p.withFlashError("load profiles: " + err.Error())
		return p, cmd
	}
	if len(profiles) == 0 {
		var cmd tea.Cmd
		p, cmd = p.withFlashError("no profiles — create one in the Profiles tab")
		return p, cmd
	}
	p.profiles = profiles
	p.profCursor = 0
	p.filter = ""
	p.filterMode = false
	p.wizStep = wizProfile
	p.view = bvWizard
	return p, nil
}

// selectedProfile resolves the profile under the (filtered) profCursor in the
// wizard's profile step; ok=false when the cursor is out of range.
func (p BenchmarkPage) selectedProfile() (string, string, bool) {
	f := p.filteredProfiles()
	if p.profCursor < 0 || p.profCursor >= len(f) {
		return "", "", false
	}
	return f[p.profCursor].ID, f[p.profCursor].Name, true
}

// viewWizard renders the current wizard step: a filterable profile list, a
// mode grid grouped by category, or a run-review summary.
func (p BenchmarkPage) viewWizard() string {
	switch p.wizStep {
	case wizProfile:
		return p.viewWizardProfile()
	case wizMode:
		return p.viewWizardMode()
	default: // wizReview
		return p.viewWizardReview()
	}
}

func (p BenchmarkPage) viewWizardProfile() string {
	title := theme.Title.Render("Run benchmark — pick a profile") + "  " + theme.Subtitle.Render("(1/3)")
	filtered := p.filteredProfiles()
	if len(filtered) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, title,
			components.EmptyState("No profiles match", "Adjust the filter or create one in the Profiles tab"))
	}
	rows := make([]string, 0, len(filtered))
	for i, prof := range filtered {
		nameW := 28
		modelW := 40
		if p.width > 0 {
			nameW = min(28, max(12, (p.width-4)*2/5))
			modelW = min(40, max(12, p.width-4-nameW))
		}
		line := fmt.Sprintf("%-*s  %s", nameW, truncate(prof.Name, nameW), truncate(prof.Model, modelW))
		if i == p.profCursor {
			if theme.NoColor() {
				line = "> " + line
			} else {
				line = theme.Selected.Render(line)
			}
		} else {
			line = "  " + line
		}
		rows = append(rows, line)
	}
	parts := []string{title}
	if fl := components.FilterLine(p.filterMode, p.filter); fl != "" {
		parts = append(parts, fl)
	}
	parts = append(parts, strings.Join(rows, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (p BenchmarkPage) viewWizardMode() string {
	title := theme.Title.Render("Run benchmark — pick a mode") + "  " + theme.Subtitle.Render("(2/3)")
	rows := make([]string, 0, len(benchModes)+4)
	var lastCat benchmark.Category
	for i, m := range benchModes {
		if c, ok := benchmark.CategoryOf(m); ok && c != lastCat {
			rows = append(rows, theme.Subtitle.Render(string(c)))
			lastCat = c
		}
		// Card: title + description on the first line; prerequisites (if any)
		// on a second indented line so external dependencies are visible before
		// committing to a run.
		desc := modeDescription(m)
		if p.width > 0 {
			desc = truncate(modeDescription(m), max(12, p.width-26))
		}
		line := fmt.Sprintf("%-22s  %s", m.Title(), desc)
		if pr := modePrereq(m); pr != "" {
			prLine := pr
			if p.width > 0 {
				prLine = truncate(pr, max(12, p.width-8))
			}
			line += "\n    " + theme.Warn.Render("⚠ "+prLine)
		}
		if i == p.modeCursor {
			if theme.NoColor() {
				line = "> " + strings.ReplaceAll(line, "\n", "\n> ")
			} else {
				line = theme.Selected.Render(line)
			}
		} else {
			line = "  " + line
		}
		rows = append(rows, line)
	}
	return lipgloss.JoinVertical(lipgloss.Left, title,
		theme.Subtitle.Render("profile: "+p.runningName), strings.Join(rows, "\n"))
}

func (p BenchmarkPage) viewWizardReview() string {
	title := theme.Title.Render("Run benchmark — review") + "  " + theme.Subtitle.Render("(3/3)")
	mode := benchModes[p.modeCursor]
	count := 0
	if p.runner != nil {
		count = p.runner.CountForMode(mode)
	}
	lines := []string{
		fmt.Sprintf("profile:  %s", p.runningName),
		fmt.Sprintf("mode:     %s (%s)", mode.Title(), modeCategoryLabel(mode)),
	}
	if count > 0 {
		lines = append(lines, fmt.Sprintf("items:    %d problems", count))
	}
	if pr := modePrereq(mode); pr != "" {
		lines = append(lines, theme.Warn.Render("note:     "+pr))
	}
	lines = append(lines, "", "Description:", "  "+truncate(modeDescription(mode), max(12, p.width-4)),
		"", theme.Subtitle.Render("[enter] start run   [esc] back to mode"))
	return lipgloss.JoinVertical(lipgloss.Left, title, strings.Join(lines, "\n"))
}

// keyWizard dispatches a key to the current wizard step, supporting forward
// AND backward navigation (the main UX win over the old two-screen flow).
func (p BenchmarkPage) keyWizard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch p.wizStep {
	case wizProfile:
		return p.keyWizardProfile(msg)
	case wizMode:
		return p.keyWizardMode(msg)
	default: // wizReview
		return p.keyWizardReview(msg)
	}
}

func (p BenchmarkPage) keyWizardProfile(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if p.filterMode {
		np, cmd, handled := p.keyProfileFilter(msg)
		if handled {
			return np, cmd
		}
		// enter in filter mode falls through: exits the filter then selects.
		p = np
	}
	switch msg.String() {
	case "esc":
		p.view = bvDashboard
	case "/":
		p.filterMode = true
	case "up", "k":
		if p.profCursor > 0 {
			p.profCursor--
		}
	case "down", "j":
		if p.profCursor < len(p.filteredProfiles())-1 {
			p.profCursor++
		}
	case "g", "home":
		p.profCursor = 0
	case "G", "end":
		if n := len(p.filteredProfiles()); n > 0 {
			p.profCursor = n - 1
		}
	case "enter":
		if id, name, ok := p.selectedProfile(); ok {
			p.selectedProfileID = id
			p.runningName = name
			p.modeCursor = 0
			p.wizStep = wizMode
		}
	}
	return p, nil
}

func (p BenchmarkPage) keyWizardMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Back, not cancel: return to the profile step (keep the selection).
		p.wizStep = wizProfile
	case "up", "k":
		if p.modeCursor > 0 {
			p.modeCursor--
		}
	case "down", "j":
		if p.modeCursor < len(benchModes)-1 {
			p.modeCursor++
		}
	case "enter":
		p.wizStep = wizReview
	}
	return p, nil
}

func (p BenchmarkPage) keyWizardReview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.wizStep = wizMode
	case "enter":
		return p.startRun()
	}
	return p, nil
}

// modeDescription returns a short, human description of a scoring mode. It is
// the single source for mode help text (replacing the ad-hoc map that used to
// live in the old mode picker).
func modeDescription(m benchmark.Mode) string {
	switch m {
	case benchmark.ModeJudge:
		return "SWE-bench Lite; reference-guided LLM judge, median of N samples (needs benchmark.judge config)"
	case benchmark.ModeLongContext:
		return "needle retrieval in a long prompt — objective diagnostic of KV-cache-quant decay"
	case benchmark.ModeLlamaBench:
		return "throughput probe (llama-bench style): TTFT + tokens/s on fixed-size prompts"
	case benchmark.ModeMathBench:
		return "math reasoning (GSM8K): exact numeric match, accuracy under quantization"
	case benchmark.ModeCodeGenBench:
		return "code generation (HumanEval): sandboxed Pass@1; needs python3 on PATH"
	case benchmark.ModeInstBench:
		return "instruction following: structured-format, refusal of disallowed prompts, and answer consistency"
	case benchmark.ModeMMLUBench:
		return "factual knowledge (MMLU): multiple-choice exact-match across STEM/humanities/social/other"
	case benchmark.ModeRagasBench:
		return "RAG quality (synthetic): grader scores faithfulness, answer relevancy, and context precision"
	case benchmark.ModeSummaryBench:
		return "multi-doc summarization: fact coverage + grader-scored coherence"
	case benchmark.ModeTerminalBench:
		return "agentic terminal tasks via the external Terminal-Bench harness; needs the `tb` CLI + Docker (long-running)"
	case benchmark.ModeSweBenchPro:
		return "agentic SWE tasks via the external SWE-bench Pro harness; needs a cloned harness + Docker + python (long-running)"
	case benchmark.ModeDeepSWE:
		return "agentic SWE tasks via the external DeepSWE (Pier) harness; needs `pier` CLI + Docker + task corpus (long-running)"
	default:
		return string(m)
	}
}

// modePrereq returns a short prerequisite note for modes with external
// requirements (Docker, CLIs, python), empty for self-contained modes.
func modePrereq(m benchmark.Mode) string {
	switch m {
	case benchmark.ModeCodeGenBench:
		return "needs python3 on PATH for sandboxed execution"
	case benchmark.ModeTerminalBench:
		return "needs the `tb` CLI + Docker (long-running)"
	case benchmark.ModeSweBenchPro:
		return "needs a cloned SWE-bench Pro harness + Docker + python (long-running)"
	case benchmark.ModeDeepSWE:
		return "needs the `pier` CLI + Docker + a task corpus (long-running)"
	case benchmark.ModeJudge, benchmark.ModeRagasBench, benchmark.ModeSummaryBench:
		return "graded by an LLM judge (needs benchmark.judge config)"
	default:
		return ""
	}
}

// modeCategoryLabel returns the human category name for a mode ("Quality",
// "Speed", …), falling back to "Benchmark" if the mode is unregistered.
func modeCategoryLabel(m benchmark.Mode) string {
	if c, ok := benchmark.CategoryOf(m); ok {
		return string(c)
	}
	return "Benchmark"
}
