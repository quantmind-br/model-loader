package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

func sampleProfiles() []domain.Profile {
	return []domain.Profile{
		{
			ID:   "alpha",
			Name: "Alpha 7B",
			Tags: []string{"chat", "small"},
			Launch: domain.LaunchConfig{
				BackendID: "cpu",
			},
		},
		{
			ID:   "bravo",
			Name: "Bravo 13B",
			Tags: []string{"code"},
			Launch: domain.LaunchConfig{
				BackendID: "cuda",
			},
		},
		{
			ID:   "charlie",
			Name: "Charlie 70B",
			Tags: []string{"reasoning"},
			Launch: domain.LaunchConfig{
				BackendID: "cuda",
			},
		},
	}
}

func TestProfilePicker_StartsSortedByName(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	if got := len(p.filtered); got != 3 {
		t.Fatalf("filtered = %d, want 3", got)
	}
	wantOrder := []string{"alpha", "bravo", "charlie"}
	for i, want := range wantOrder {
		if got := p.filtered[i].id; got != want {
			t.Errorf("filtered[%d].id = %q, want %q", i, got, want)
		}
	}
}

func TestProfilePicker_DownArrowAdvancesCursor(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if p.cursor != 1 {
		t.Errorf("cursor after Down = %d, want 1", p.cursor)
	}
}

func TestProfilePicker_UpArrowDoesNotUnderflow(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyUp})
	if p.cursor != 0 {
		t.Errorf("cursor after Up at top = %d, want 0", p.cursor)
	}
}

func TestProfilePicker_JAndKNavigate(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if p.cursor != 1 {
		t.Errorf("cursor after j = %d, want 1", p.cursor)
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if p.cursor != 0 {
		t.Errorf("cursor after k = %d, want 0", p.cursor)
	}
}

func TestProfilePicker_HomeAndEnd(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	p.cursor = 1
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if p.cursor != 2 {
		t.Errorf("cursor after End = %d, want 2", p.cursor)
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyHome})
	if p.cursor != 0 {
		t.Errorf("cursor after Home = %d, want 0", p.cursor)
	}
}

func TestProfilePicker_GAndCapitalG(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	p.cursor = 1
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if p.cursor != 2 {
		t.Errorf("cursor after G = %d, want 2", p.cursor)
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if p.cursor != 0 {
		t.Errorf("cursor after g = %d, want 0", p.cursor)
	}
}

func TestProfilePicker_PgDnAdvancesByTen(t *testing.T) {
	profiles := make([]domain.Profile, 0, 50)
	for i := range 50 {
		profiles = append(profiles, domain.Profile{ID: string(rune('a' + i%26)) + string(rune('0' + i%10)), Name: string(rune('a' + i))})
	}
	p := NewProfilePicker(profiles)
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if p.cursor != 10 {
		t.Errorf("PgDn cursor = %d, want 10", p.cursor)
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if p.cursor != 0 {
		t.Errorf("PgUp cursor = %d, want 0", p.cursor)
	}
}

func TestProfilePicker_FilterByName(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	p.filterMode = true
	for _, r := range "bravo" {
		p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if p.ActiveFilter() != "bravo" {
		t.Errorf("ActiveFilter = %q, want %q", p.ActiveFilter(), "bravo")
	}
	if len(p.filtered) != 1 || p.filtered[0].id != "bravo" {
		t.Fatalf("filtered = %+v, want only bravo", p.filtered)
	}
}

func TestProfilePicker_FilterByID(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	p.filter = "charlie"
	p.applyFilter()
	if len(p.filtered) != 1 || p.filtered[0].id != "charlie" {
		t.Fatalf("filter by id failed: %+v", p.filtered)
	}
}

func TestProfilePicker_FilterByTag(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	p.filter = "reasoning"
	p.applyFilter()
	if len(p.filtered) != 1 || p.filtered[0].id != "charlie" {
		t.Fatalf("filter by tag failed: %+v", p.filtered)
	}
}

func TestProfilePicker_FilterByBackend(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	p.filter = "cuda"
	p.applyFilter()
	if len(p.filtered) != 2 {
		t.Fatalf("filter by backend got %d, want 2", len(p.filtered))
	}
	ids := []string{p.filtered[0].id, p.filtered[1].id}
	if ids[0] != "bravo" || ids[1] != "charlie" {
		t.Errorf("filter by backend ids = %v, want [bravo charlie]", ids)
	}
}

func TestProfilePicker_FilterIsCaseInsensitive(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	p.filter = "BRAVO"
	p.applyFilter()
	if len(p.filtered) != 1 || p.filtered[0].id != "bravo" {
		t.Fatalf("case-insensitive filter failed: %+v", p.filtered)
	}
}

func TestProfilePicker_BackspaceShrinksFilter(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	p.filterMode = true
	p.filter = "bra"
	p.applyFilter()
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if p.filter != "br" {
		t.Errorf("filter after backspace = %q, want %q", p.filter, "br")
	}
}

func TestProfilePicker_SlashTogglesFilterMode(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	if p.filterMode {
		t.Fatal("filterMode should start false")
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !p.filterMode {
		t.Error("filterMode should be true after /")
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if p.filterMode {
		t.Error("filterMode should be false after second /")
	}
}

func TestProfilePicker_EnterEmitsProfilePickedMsg(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	p.cursor = 1
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected ProfilePickedMsg cmd, got nil")
	}
	msg := cmd()
	picked, ok := msg.(ProfilePickedMsg)
	if !ok {
		t.Fatalf("msg type = %T, want ProfilePickedMsg", msg)
	}
	if picked.ID != "bravo" {
		t.Errorf("picked.ID = %q, want %q", picked.ID, "bravo")
	}
}

func TestProfilePicker_EnterOnEmptyListIsNoOp(t *testing.T) {
	p := NewProfilePicker(nil)
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("Enter on empty picker should not emit a cmd; got %T", cmd())
	}
}

func TestProfilePicker_EnterAfterFilterUsesFilteredCursor(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	p.filter = "cuda"
	p.applyFilter()
	p.cursor = 1
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	picked := cmd().(ProfilePickedMsg)
	if picked.ID != "charlie" {
		t.Errorf("picked.ID = %q, want %q (second cuda profile)", picked.ID, "charlie")
	}
}

func TestProfilePicker_EscEmitsCancelled(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected ProfilePickerCancelledMsg cmd, got nil")
	}
	msg := cmd()
	if _, ok := msg.(ProfilePickerCancelledMsg); !ok {
		t.Fatalf("msg type = %T, want ProfilePickerCancelledMsg", msg)
	}
}

func TestProfilePicker_ViewRendersTitleAndRows(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	out := p.View()
	for _, want := range []string{"Pick a profile", "Alpha 7B", "Bravo 13B", "Charlie 70B"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q; got:\n%s", want, out)
		}
	}
}

func TestProfilePicker_ViewShowsFilterIndicator(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	p.filter = "bravo"
	p.applyFilter()
	out := p.View()
	if !strings.Contains(out, `filter: "bravo"`) {
		t.Errorf("view missing filter indicator; got:\n%s", out)
	}
}

func TestProfilePicker_ViewShowsSelectedPrefixUnderNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	defer func() {
		t.Setenv("NO_COLOR", "")
		theme.RebuildStyles()
	}()

	p := NewProfilePicker(sampleProfiles())
	p.cursor = 1
	out := p.View()
	if !strings.Contains(out, "> Bravo 13B") {
		t.Errorf("expected '> Bravo 13B' marker in view; got:\n%s", out)
	}
}

func TestProfilePicker_GInFilterModeAppendsToFilter(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	p.filterMode = true
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if p.filter != "g" {
		t.Errorf("filter = %q, want %q", p.filter, "g")
	}
}

func TestProfilePicker_ActiveFilterEmptyAtStart(t *testing.T) {
	p := NewProfilePicker(sampleProfiles())
	if p.ActiveFilter() != "" {
		t.Errorf("ActiveFilter at start = %q, want empty", p.ActiveFilter())
	}
}
