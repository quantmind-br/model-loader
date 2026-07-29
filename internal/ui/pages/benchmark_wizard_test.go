package pages

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// stubStore is a minimal profilestore.Store double for the wizard tests. It
// serves a fixed profile list; the other methods are unused by the wizard.
type stubStore struct{ ps []domain.Profile }

func (s stubStore) List() ([]domain.Profile, error) { return s.ps, nil }
func (s stubStore) ListWithDiagnostics() ([]domain.Profile, []profilestore.ListDiagnostic, error) {
	return s.ps, nil, nil
}
func (s stubStore) Get(string) (domain.Profile, error)               { return domain.Profile{}, nil }
func (s stubStore) Create(domain.Profile) error                      { return nil }
func (s stubStore) Save(domain.Profile) error                        { return nil }
func (s stubStore) Delete(string) error                              { return nil }
func (s stubStore) Duplicate(string, string) (domain.Profile, error) { return domain.Profile{}, nil }
func (s stubStore) Rename(string, domain.Profile) error              { return nil }

// TestWizard_ForwardNavigation walks the wizard forward: profile enter → mode
// enter → review, asserting each step transition and the profile selection.
func TestWizard_ForwardNavigation(t *testing.T) {
	page := NewBenchmarkPage(stubStore{}, &fakeBStore{}, nil, t.TempDir())
	page.profiles = []domain.Profile{
		{ID: "p1", Name: "Alpha", Model: "/m/a.gguf"},
		{ID: "p2", Name: "Beta", Model: "/m/b.gguf"},
	}
	page.view = bvWizard
	page.wizStep = wizProfile

	// enter on profile step → mode step, capturing the selection.
	m, _ := page.keyWizard(tea.KeyMsg{Type: tea.KeyEnter})
	page = m.(BenchmarkPage)
	if page.wizStep != wizMode {
		t.Fatalf("after profile enter: wizStep=%v, want wizMode", page.wizStep)
	}
	if page.selectedProfileID != "p1" || page.runningName != "Alpha" {
		t.Fatalf("profile selection = %q/%q, want p1/Alpha", page.selectedProfileID, page.runningName)
	}

	// enter on mode step → review step.
	m, _ = page.keyWizard(tea.KeyMsg{Type: tea.KeyEnter})
	page = m.(BenchmarkPage)
	if page.wizStep != wizReview {
		t.Fatalf("after mode enter: wizStep=%v, want wizReview", page.wizStep)
	}
}

func TestWizard_ProfileCursorKeepsGutterInColorMode(t *testing.T) {
	t.Cleanup(theme.RebuildStyles)
	t.Setenv("NO_COLOR", "")
	theme.RebuildStyles()

	page := NewBenchmarkPage(stubStore{}, &fakeBStore{}, nil, t.TempDir())
	page.profiles = []domain.Profile{
		{ID: "p1", Name: "Alpha", Model: "/m/a.gguf"},
		{ID: "p2", Name: "Bravo", Model: "/m/b.gguf"},
	}
	page.width = 80
	page.profCursor = 1

	lines := strings.Split(ansi.Strip(page.viewWizardProfile()), "\n")
	var rows []string
	for _, line := range lines {
		if strings.Contains(line, "Alpha") || strings.Contains(line, "Bravo") {
			rows = append(rows, line)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("wizard rows = %d, want 2: %q", len(rows), lines)
	}
	if !strings.HasPrefix(rows[1], "> ") {
		t.Fatalf("selected row = %q, want > gutter", rows[1])
	}
	for _, row := range rows {
		if got, want := lipgloss.Width(row), lipgloss.Width(rows[0]); got != want {
			t.Fatalf("row width = %d, want %d: %q", got, want, row)
		}
	}
}

// TestWizard_BackNavigation asserts esc moves backward without losing the
// profile selection — the main UX win over the old two-screen flow.
func TestWizard_BackNavigation(t *testing.T) {
	page := NewBenchmarkPage(stubStore{}, &fakeBStore{}, nil, t.TempDir())
	page.profiles = []domain.Profile{{ID: "p1", Name: "Alpha", Model: "/m/a.gguf"}}
	page.view = bvWizard
	page.selectedProfileID = "p1"
	page.runningName = "Alpha"
	page.wizStep = wizMode

	// esc on mode step → back to profile step, selection preserved.
	m, _ := page.keyWizard(tea.KeyMsg{Type: tea.KeyEsc})
	page = m.(BenchmarkPage)
	if page.wizStep != wizProfile {
		t.Fatalf("esc on mode: wizStep=%v, want wizProfile", page.wizStep)
	}
	if page.selectedProfileID != "p1" {
		t.Fatalf("esc should preserve profile selection, got %q", page.selectedProfileID)
	}

	// esc on profile step → back to the dashboard.
	page.wizStep = wizProfile
	m, _ = page.keyWizard(tea.KeyMsg{Type: tea.KeyEsc})
	page = m.(BenchmarkPage)
	if page.view != bvDashboard {
		t.Fatalf("esc on profile step: view=%v, want bvDashboard", page.view)
	}
}

// TestWizard_ReviewShowsProfileAndBack asserts the review body surfaces the
// selected profile name, no longer embeds the removed inline start hint, and
// that Root's shared status-bar footer supplies the wizReview shortcuts.
func TestWizard_ReviewShowsProfileAndBack(t *testing.T) {
	page := NewBenchmarkPage(stubStore{}, &fakeBStore{}, nil, t.TempDir())
	page.profiles = []domain.Profile{{ID: "p1", Name: "Alpha", Model: "/m/a.gguf"}}
	page.view = bvWizard
	page.wizStep = wizReview
	page.modeCursor = 0
	page.selectedProfileID = "p1"
	page.runningName = "Alpha"

	out := page.viewWizardReview()
	if !strings.Contains(out, "Alpha") {
		t.Fatalf("review should show selected profile name:\n%s", out)
	}
	if strings.Contains(out, "[enter] start run") {
		t.Fatalf("review body must not embed the removed start hint:\n%s", out)
	}
	if h := page.Hints(); h != "[enter] run  [esc] back" {
		t.Fatalf("wizReview hints = %q, want %q", h, "[enter] run  [esc] back")
	}
}

// TestWizard_ProfileFilter renders the profile step with an active filter and
// asserts the matching profile is still visible.
func TestWizard_ProfileFilterLine(t *testing.T) {
	page := NewBenchmarkPage(stubStore{}, &fakeBStore{}, nil, t.TempDir())
	page.profiles = []domain.Profile{{ID: "p1", Name: "Alpha", Model: "/m/a.gguf"}}
	page.view = bvWizard
	page.wizStep = wizProfile
	page.filterMode = true
	page.filter = "alp"
	out := page.viewWizard()
	if !strings.Contains(out, "Alpha") {
		t.Fatalf("filtered profile step should still show the matching profile:\n%s", out)
	}
}

// TestWizard_ModeCardsContainCategories asserts the mode step renders the
// canonical category headers.
func TestWizard_ModeCardsContainCategories(t *testing.T) {
	page := NewBenchmarkPage(stubStore{}, &fakeBStore{}, nil, t.TempDir())
	page.view = bvWizard
	page.wizStep = wizMode
	page.runningName = "Demo"
	out := page.viewWizard()
	for _, h := range []string{"Quality", "Speed", "Robustness", "Knowledge"} {
		if !strings.Contains(out, h) {
			t.Fatalf("wizard mode step missing category header %q:\n%s", h, out)
		}
	}
}

// TestWizard_ModeCardsAreCompact asserts the mode step shows name + one
// measurement sentence and no operational prerequisite clutter.
func TestWizard_ModeCardsAreCompact(t *testing.T) {
	page := NewBenchmarkPage(stubStore{}, &fakeBStore{}, nil, t.TempDir())
	page.view = bvWizard
	page.wizStep = wizMode
	page.runningName = "Demo"
	out := page.viewWizard()
	for _, want := range []string{
		"LLM judge (SWE-bench Lite)",
		"Rates SWE-bench Lite patch quality with an LLM judge.",
		"Agentic terminal tasks (Terminal-Bench)",
		"Measures agentic terminal task completion.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("wizard mode step missing %q:\n%s", want, out)
		}
	}
	for _, ban := range []string{"⚠", "needs ", "Docker", "benchmark.judge config"} {
		if strings.Contains(out, ban) {
			t.Fatalf("wizard mode step should not show %q:\n%s", ban, out)
		}
	}
}

// TestWizard_ReviewShowsDescription asserts the review screen surfaces the
// mode description so the user can confirm before launching.
func TestWizard_ReviewShowsDescription(t *testing.T) {
	page := NewBenchmarkPage(stubStore{}, &fakeBStore{}, nil, t.TempDir())
	page.view = bvWizard
	page.wizStep = wizReview
	page.modeCursor = 0
	page.selectedProfileID = "p1"
	page.runningName = "Alpha"
	page.width = 120
	out := page.viewWizardReview()
	if !strings.Contains(out, "Rates SWE-bench Lite patch quality with an LLM judge.") {
		t.Fatalf("review should show the mode description:\n%s", out)
	}
}

// wizardBudget asserts out fits within w columns and h rows. NO_COLOR keeps the
// output plain, so rune width equals display width.
func wizardBudget(t *testing.T, name, out string, w, h int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) > h {
		t.Fatalf("%s: %d lines exceed height %d", name, len(lines), h)
	}
	for i, ln := range lines {
		if lw := theme.RuneWidth(ln); lw > w {
			t.Fatalf("%s: line %d width %d exceeds %d: %q", name, i, lw, w, ln)
		}
	}
}

// TestWizard_ProfileReflowsAcrossResize drives one open profile step through a
// narrow→wide resize and asserts it reflows live: the selected final profile
// stays visible with a scroll marker when narrow, and its full name+model plus
// more rows appear when wide, each render honoring the width/height budget.
func TestWizard_ProfileReflowsAcrossResize(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	t.Cleanup(func() { t.Setenv("NO_COLOR", ""); theme.RebuildStyles() })

	profs := make([]domain.Profile, 30)
	for i := range profs {
		profs[i] = domain.Profile{
			ID:    fmt.Sprintf("p%02d", i),
			Name:  fmt.Sprintf("profile-PN%02d-really-long-display-name", i),
			Model: fmt.Sprintf("/models/organization/team/PM%02d/checkpoint-final-model.gguf", i),
		}
	}
	finalName := profs[29].Name
	finalModel := profs[29].Model

	page := NewBenchmarkPage(stubStore{ps: profs}, &fakeBStore{}, nil, t.TempDir())
	page.view = bvWizard
	page.wizStep = wizProfile
	page.profiles = profs
	page.profCursor = 29

	m, _ := page.Update(tea.WindowSizeMsg{Width: 48, Height: 10})
	page = m.(BenchmarkPage)
	narrow := page.viewWizardProfile()
	if !strings.Contains(narrow, "PN29") {
		t.Fatalf("narrow render must show the selected final profile:\n%s", narrow)
	}
	if !strings.Contains(narrow, "more") {
		t.Fatalf("narrow render must window the list with a more marker:\n%s", narrow)
	}
	wizardBudget(t, "profile-narrow", narrow, 48, 10)

	m, _ = page.Update(tea.WindowSizeMsg{Width: 160, Height: 28})
	page = m.(BenchmarkPage)
	if page.width != 160 || page.height != 28 {
		t.Fatalf("resize not stored: width=%d height=%d", page.width, page.height)
	}
	wide := page.viewWizardProfile()
	if !strings.Contains(wide, finalName) {
		t.Fatalf("wide render must show the full profile name:\n%s", wide)
	}
	if !strings.Contains(wide, finalModel) {
		t.Fatalf("wide render must show the full model path:\n%s", wide)
	}
	if strings.Count(wide, "PN") <= strings.Count(narrow, "PN") {
		t.Fatalf("wide render (%d PN) must show more profiles than narrow (%d PN)",
			strings.Count(wide, "PN"), strings.Count(narrow, "PN"))
	}
	wizardBudget(t, "profile-wide", wide, 160, 28)
}

// TestWizard_ModeReflowsAcrossResize drives one open mode step through a
// narrow→wide resize: the selected deep-swe row stays visible at both sizes and
// the list windows when narrow; when wide, the full title and complete
// description render with no scroll markers. Each render honors its budget.
func TestWizard_ModeReflowsAcrossResize(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	t.Cleanup(func() { t.Setenv("NO_COLOR", ""); theme.RebuildStyles() })

	target := -1
	for i, mode := range benchModes {
		if mode == benchmark.ModeDeepSWE {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("deep-swe not present in benchModes")
	}

	page := NewBenchmarkPage(stubStore{}, &fakeBStore{}, nil, t.TempDir())
	page.view = bvWizard
	page.wizStep = wizMode
	page.runningName = "Demo"
	page.modeCursor = target

	m, _ := page.Update(tea.WindowSizeMsg{Width: 48, Height: 12})
	page = m.(BenchmarkPage)
	narrow := page.viewWizardMode()
	if !strings.Contains(narrow, "> Agentic SWE") {
		t.Fatalf("narrow render must keep the selected mode row visible:\n%s", narrow)
	}
	if !strings.Contains(narrow, "more") {
		t.Fatalf("narrow render must window the mode list with a more marker:\n%s", narrow)
	}
	wizardBudget(t, "mode-narrow", narrow, 48, 12)

	m, _ = page.Update(tea.WindowSizeMsg{Width: 160, Height: 28})
	page = m.(BenchmarkPage)
	wide := page.viewWizardMode()
	if !strings.Contains(wide, "> "+benchmark.ModeDeepSWE.Title()) {
		t.Fatalf("wide render must show the full selected mode title:\n%s", wide)
	}
	if !strings.Contains(wide, modeDescription(benchmark.ModeDeepSWE)) {
		t.Fatalf("wide render must show the complete mode description:\n%s", wide)
	}
	if strings.Contains(wide, "↑ ") || strings.Contains(wide, "↓ ") {
		t.Fatalf("wide render must not window (no scroll markers):\n%s", wide)
	}
	wizardBudget(t, "mode-wide", wide, 160, 28)
}

// TestWizard_ReviewReflowsAcrossResize drives one open review step through a
// narrow→wide resize: fields expand horizontally (full profile value + mode
// description when wide), the removed inline start hint never appears, the
// shared footer supplies the shortcuts, and each render honors its budget.
func TestWizard_ReviewReflowsAcrossResize(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	t.Cleanup(func() { t.Setenv("NO_COLOR", ""); theme.RebuildStyles() })

	target := -1
	for i, mode := range benchModes {
		if mode == benchmark.ModeDeepSWE {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("deep-swe not present in benchModes")
	}

	longName := "extremely-long-selected-profile-display-name-for-reflow-checks"
	page := NewBenchmarkPage(stubStore{}, &fakeBStore{}, nil, t.TempDir())
	page.view = bvWizard
	page.wizStep = wizReview
	page.modeCursor = target
	page.selectedProfileID = "p1"
	page.runningName = longName

	m, _ := page.Update(tea.WindowSizeMsg{Width: 48, Height: 10})
	page = m.(BenchmarkPage)
	narrow := page.viewWizardReview()
	if strings.Contains(narrow, "[enter] start run") {
		t.Fatalf("review body must not embed the removed start hint:\n%s", narrow)
	}
	wizardBudget(t, "review-narrow", narrow, 48, 10)

	m, _ = page.Update(tea.WindowSizeMsg{Width: 160, Height: 28})
	page = m.(BenchmarkPage)
	wide := page.viewWizardReview()
	if !strings.Contains(wide, longName) {
		t.Fatalf("wide render must show the full profile value:\n%s", wide)
	}
	if !strings.Contains(wide, modeDescription(benchmark.ModeDeepSWE)) {
		t.Fatalf("wide render must show the complete mode description:\n%s", wide)
	}
	if strings.Contains(wide, "[enter] start run") {
		t.Fatalf("review body must not embed the removed start hint:\n%s", wide)
	}
	wizardBudget(t, "review-wide", wide, 160, 28)
	if h := page.Hints(); h != "[enter] run  [esc] back" {
		t.Fatalf("wizReview hints = %q, want %q", h, "[enter] run  [esc] back")
	}
}
