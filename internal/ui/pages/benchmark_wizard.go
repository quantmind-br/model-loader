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

// wizardPairWidths allocates the content width of a two-field wizard row
// (profile name/model, or mode title/description) across the space left after
// the selected-row prefix ("  ") and inter-column gap ("  ") are reserved. When
// both values fit, each keeps its natural rune width; otherwise left starts at
// leftPercent of the available width and any field needing fewer cells lends its
// spare to the other, capped at that other field's natural width. Content is
// truncated only when the combined natural widths cannot fit. totalWidth<=0
// returns natural widths so unsized pages expose full content.
func wizardPairWidths(left, right string, totalWidth, leftPercent int) (leftWidth, rightWidth int) {
	lw := theme.RuneWidth(left)
	rw := theme.RuneWidth(right)
	if totalWidth <= 0 {
		return lw, rw
	}
	available := max(2, totalWidth-4)
	if lw+rw <= available {
		return lw, rw
	}
	leftWidth = max(1, available*leftPercent/100)
	rightWidth = available - leftWidth
	switch {
	case lw < leftWidth:
		rightWidth = min(rw, rightWidth+(leftWidth-lw))
		leftWidth = lw
	case rw < rightWidth:
		leftWidth = min(lw, leftWidth+(rightWidth-rw))
		rightWidth = rw
	}
	return leftWidth, rightWidth
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
		nameW, modelW := wizardPairWidths(prof.Name, prof.Model, p.width, 40)
		line := fmt.Sprintf("%-*s  %s", nameW, truncate(prof.Name, nameW), truncate(prof.Model, modelW))
		if i == p.profCursor {
			if theme.NoColor() {
				line = "> " + line
			} else {
				line = theme.Selected.Render("> " + line)
			}
		} else {
			line = "  " + line
		}
		rows = append(rows, line)
	}
	top := []string{title}
	if fl := components.FilterLine(p.filterMode, p.filter); fl != "" {
		top = append(top, fl)
	}
	return p.composeWindowed(top, rows, nil, p.profCursor)
}

func (p BenchmarkPage) viewWizardMode() string {
	title := theme.Title.Render("Run benchmark — pick a mode") + "  " + theme.Subtitle.Render("(2/3)")
	rows := make([]string, 0, len(benchModes)+4)
	// cursorLine is the physical-line index (within the flattened row list) of
	// the selected mode. Category headers mean row index != physical-line
	// index, so we track it explicitly and hand it to composeWindowed —
	// otherwise the window can't follow modeCursor and agentic modes clip
	// below the fold with no marker (UIUX-021).
	cursorLine, physical := 0, 0
	var lastCat benchmark.Category
	for i, m := range benchModes {
		if c, ok := benchmark.CategoryOf(m); ok && c != lastCat {
			rows = append(rows, theme.Subtitle.Render(string(c)))
			physical++
			lastCat = c
		}
		// Row: benchmark name plus one short description sentence. Operational
		// prerequisites stay on the review step so the picker remains scannable.
		titleW, descW := wizardPairWidths(m.Title(), modeDescription(m), p.width, 40)
		line := fmt.Sprintf("%-*s  %s", titleW, truncate(m.Title(), titleW), truncate(modeDescription(m), descW))
		if i == p.modeCursor {
			cursorLine = physical
			if theme.NoColor() {
				line = "> " + strings.ReplaceAll(line, "\n", "\n> ")
			} else {
				line = theme.Selected.Render("> " + line)
			}
		} else {
			line = "  " + line
		}
		rows = append(rows, line)
		physical += 1 + strings.Count(line, "\n")
	}
	top := []string{title, theme.Subtitle.Render("profile: " + p.runningName)}
	return p.composeWindowed(top, rows, nil, cursorLine)
}

func (p BenchmarkPage) viewWizardReview() string {
	title := theme.Title.Render("Run benchmark — review") + "  " + theme.Subtitle.Render("(3/3)")
	mode := benchModes[p.modeCursor]
	count := 0
	if p.runner != nil {
		count = p.runner.CountForMode(mode)
	}
	fit := func(prefix, val string) string {
		if p.width > 0 {
			val = truncate(val, max(1, p.width-theme.RuneWidth(prefix)))
		}
		return prefix + val
	}
	lines := []string{
		fit("profile:  ", p.runningName),
		fit("mode:     ", fmt.Sprintf("%s (%s)", mode.Title(), modeCategoryLabel(mode))),
	}
	if count > 0 {
		lines = append(lines, fit("items:    ", fmt.Sprintf("%d problems", count)))
	}
	if pr := modePrereq(mode); pr != "" {
		if p.width > 0 {
			pr = truncate(pr, max(1, p.width-theme.RuneWidth("note:     ")))
		}
		lines = append(lines, theme.Warn.Render("note:     "+pr))
	}
	lines = append(lines, "", "Description:", fit("  ", modeDescription(mode)))
	return p.clampBody(lipgloss.JoinVertical(lipgloss.Left, title, strings.Join(lines, "\n")))
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
		return "Rates SWE-bench Lite patch quality with an LLM judge."
	case benchmark.ModeLongContext:
		return "Measures long-context retrieval through needle recall."
	case benchmark.ModeLlamaBench:
		return "Measures prompt latency and tokens per second."
	case benchmark.ModeMathBench:
		return "Measures GSM8K math accuracy by exact numeric answer."
	case benchmark.ModeCodeGenBench:
		return "Measures HumanEval code generation with executable tests."
	case benchmark.ModeInstBench:
		return "Measures format following, refusals, and answer consistency."
	case benchmark.ModeMMLUBench:
		return "Measures MMLU factual knowledge with multiple choice."
	case benchmark.ModeRagasBench:
		return "Measures RAG faithfulness, relevance, and context precision."
	case benchmark.ModeSummaryBench:
		return "Measures multi-document summary fact coverage and coherence."
	case benchmark.ModeTerminalBench:
		return "Measures agentic terminal task completion."
	case benchmark.ModeSweBenchPro:
		return "Measures agentic SWE task patch success."
	case benchmark.ModeDeepSWE:
		return "Measures DeepSWE task completion through Pier."
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
