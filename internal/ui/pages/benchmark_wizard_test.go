package pages

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
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

// TestWizard_ReviewShowsProfileAndBack asserts the review screen surfaces the
// selected profile name and the back hint. (Item count is only shown when a
// runner is present, which these nil-runner tests don't construct.)
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
	if !strings.Contains(out, "[enter] start run") {
		t.Fatalf("review should show the start hint:\n%s", out)
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

// TestWizard_ModeCardsShowPrereqs asserts modes with external dependencies
// (Docker, CLIs) surface their prerequisites inline on the mode step.
func TestWizard_ModeCardsShowPrereqs(t *testing.T) {
	page := NewBenchmarkPage(stubStore{}, &fakeBStore{}, nil, t.TempDir())
	page.view = bvWizard
	page.wizStep = wizMode
	page.runningName = "Demo"
	out := page.viewWizard()
	// terminal-bench needs the tb CLI + Docker.
	if !strings.Contains(out, "tb") || !strings.Contains(out, "Docker") {
		t.Fatalf("wizard mode step should show terminal-bench prereqs:\n%s", out)
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
	out := page.viewWizardReview()
	if !strings.Contains(out, "SWE-bench") {
		t.Fatalf("review should show the mode description:\n%s", out)
	}
}
