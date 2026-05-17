package components

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeSearcher struct {
	results []SearchResult
	err     error
}

func (f *fakeSearcher) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}

func TestHFSearchPicker_InitReturnsNilAndActivates(t *testing.T) {
	p := NewHFSearchPicker(&fakeSearcher{}, 80, 24)
	if p.IsActive() {
		t.Fatal("picker should not be active before Init()")
	}
	cmd := p.Init()
	if cmd != nil {
		t.Errorf("Init() returned non-nil cmd: %v", cmd)
	}
	if !p.IsActive() {
		t.Error("picker should be active after Init()")
	}
}

func TestHFSearchPicker_EscClosesPicker(t *testing.T) {
	p := NewHFSearchPicker(&fakeSearcher{}, 80, 24)
	p.Init()
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if p.IsActive() {
		t.Error("picker should not be active after esc")
	}
}

func TestHFSearchPicker_UpDownMovesCursor(t *testing.T) {
	p := NewHFSearchPicker(&fakeSearcher{}, 80, 24)
	p.Init()
	p.results = []ResultItem{
		{SearchResult: SearchResult{ID: "a"}, Index: 0},
		{SearchResult: SearchResult{ID: "b"}, Index: 1},
		{SearchResult: SearchResult{ID: "c"}, Index: 2},
	}

	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if p.cursor != 1 {
		t.Errorf("cursor after Down = %d, want 1", p.cursor)
	}
	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if p.cursor != 2 {
		t.Errorf("cursor after second Down = %d, want 2", p.cursor)
	}
	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if p.cursor != 2 {
		t.Errorf("cursor should clamp at end; got = %d, want 2", p.cursor)
	}

	p.Update(tea.KeyMsg{Type: tea.KeyUp})
	if p.cursor != 1 {
		t.Errorf("cursor after Up = %d, want 1", p.cursor)
	}
	p.Update(tea.KeyMsg{Type: tea.KeyUp})
	p.Update(tea.KeyMsg{Type: tea.KeyUp})
	if p.cursor != 0 {
		t.Errorf("cursor should clamp at 0; got = %d, want 0", p.cursor)
	}
}

func TestHFSearchPicker_GTogglesGGUFOnly(t *testing.T) {
	p := NewHFSearchPicker(&fakeSearcher{}, 80, 24)
	p.Init()
	if p.ggufOnly {
		t.Fatal("ggufOnly should default false")
	}
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if !p.ggufOnly {
		t.Error("ggufOnly should be true after first 'g'")
	}
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if p.ggufOnly {
		t.Error("ggufOnly should be false after second 'g'")
	}
}

func TestHFSearchPicker_PrintableRuneAppendsToQuery(t *testing.T) {
	p := NewHFSearchPicker(&fakeSearcher{}, 80, 24)
	p.Init()
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if p.query != "llama" {
		t.Errorf("query = %q, want %q", p.query, "llama")
	}
}

func TestHFSearchPicker_BackspaceRemovesLastRune(t *testing.T) {
	p := NewHFSearchPicker(&fakeSearcher{}, 80, 24)
	p.Init()
	p.query = "abc"
	p.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if p.query != "ab" {
		t.Errorf("query after backspace = %q, want %q", p.query, "ab")
	}
}

func TestHFSearchPicker_BackspaceOnEmptyIsNoOp(t *testing.T) {
	p := NewHFSearchPicker(&fakeSearcher{}, 80, 24)
	p.Init()
	cmd := p.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if cmd != nil {
		t.Errorf("backspace on empty query should return nil cmd; got %v", cmd)
	}
	if p.query != "" {
		t.Errorf("query should remain empty; got %q", p.query)
	}
}

func TestHFSearchPicker_ViewRendersSearchPrompt(t *testing.T) {
	p := NewHFSearchPicker(&fakeSearcher{}, 80, 24)
	p.Init()
	out := p.View()
	if !strings.Contains(out, "Search:") {
		t.Errorf("view missing 'Search:' prompt; got:\n%s", out)
	}
	if !strings.Contains(out, "esc") {
		t.Errorf("view should mention 'esc' hint; got:\n%s", out)
	}
}

func TestHFSearchPicker_ViewRendersResults(t *testing.T) {
	p := NewHFSearchPicker(&fakeSearcher{}, 80, 24)
	p.Init()
	p.results = []ResultItem{
		{SearchResult: SearchResult{ID: "user/model-a", ModelID: "user/model-a"}, Index: 0},
		{SearchResult: SearchResult{ID: "user/model-b", ModelID: "user/model-b", Tags: []string{"gguf"}}, Index: 1},
	}
	out := p.View()
	if !strings.Contains(out, "user/model-a") {
		t.Errorf("view missing first result; got:\n%s", out)
	}
	if !strings.Contains(out, "user/model-b") {
		t.Errorf("view missing second result; got:\n%s", out)
	}
	if !strings.Contains(out, "[GGUF]") {
		t.Errorf("view missing GGUF tag marker; got:\n%s", out)
	}
}

func TestHFSearchPicker_ViewShowsNoResultsForQuery(t *testing.T) {
	p := NewHFSearchPicker(&fakeSearcher{}, 80, 24)
	p.Init()
	p.query = "nothingmatches"
	out := p.View()
	if !strings.Contains(out, "No results") {
		t.Errorf("view should show 'No results' for empty result set; got:\n%s", out)
	}
}

func TestHFSearchPicker_SelectedReturnsCurrentResult(t *testing.T) {
	p := NewHFSearchPicker(&fakeSearcher{}, 80, 24)
	p.Init()
	p.results = []ResultItem{
		{SearchResult: SearchResult{ID: "first"}, Index: 0},
		{SearchResult: SearchResult{ID: "second"}, Index: 1},
	}
	p.cursor = 1
	got, ok := p.Selected()
	if !ok {
		t.Fatal("Selected() reported ok=false with valid cursor")
	}
	if got.ID != "second" {
		t.Errorf("Selected().ID = %q, want %q", got.ID, "second")
	}
}

func TestHFSearchPicker_SelectedFalseWhenNoResults(t *testing.T) {
	p := NewHFSearchPicker(&fakeSearcher{}, 80, 24)
	p.Init()
	_, ok := p.Selected()
	if ok {
		t.Error("Selected() should return ok=false with empty results")
	}
}

func TestHFSearchPicker_FilterResultsHonorsGGUFOnly(t *testing.T) {
	p := NewHFSearchPicker(&fakeSearcher{}, 80, 24)
	p.Init()
	p.ggufOnly = true
	in := []ResultItem{
		{SearchResult: SearchResult{ID: "a", Tags: []string{"gguf"}}},
		{SearchResult: SearchResult{ID: "b", Tags: []string{"safetensors"}}},
		{SearchResult: SearchResult{ID: "c", Tags: []string{"GGUF"}}},
	}
	out := p.filterResults(in)
	if len(out) != 2 {
		t.Fatalf("filterResults = %d items, want 2 (gguf + GGUF case-insensitive)", len(out))
	}
}

func TestHFSearchPicker_SetSizeUpdatesDimensions(t *testing.T) {
	p := NewHFSearchPicker(&fakeSearcher{}, 80, 24)
	p.SetSize(120, 40)
	if p.width != 120 || p.height != 40 {
		t.Errorf("size = (%d,%d), want (120,40)", p.width, p.height)
	}
}

func TestHFSearchPicker_ResultItemHasGGUFTag(t *testing.T) {
	yes := ResultItem{SearchResult: SearchResult{Tags: []string{"gguf"}}}
	if !yes.HasGGUFTag() {
		t.Error("HasGGUFTag should return true for gguf tag")
	}
	no := ResultItem{SearchResult: SearchResult{Tags: []string{"safetensors"}}}
	if no.HasGGUFTag() {
		t.Error("HasGGUFTag should return false for non-gguf tags")
	}
}
