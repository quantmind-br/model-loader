package components

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// HFSearcher is the minimal interface needed to search the Hugging Face Hub.
type HFSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

// SearchResult mirrors hfhub.SearchResult so the component package does not
// need to import internal/service/hfhub.
type SearchResult struct {
	ID           string
	Author       string
	ModelID      string
	Tags         []string
	Downloads    int
	Likes        int
	LastModified time.Time
	LibraryName  string
	PipelineTag  string
}

// ResultItem is the local representation used by the picker.
type ResultItem struct {
	SearchResult
	Index int
}

// HFSearchResultMsg carries the async search response back into Update.
type HFSearchResultMsg struct {
	Epoch   int
	Results []ResultItem
	Err     error
}

type hfSearchResultMsg = HFSearchResultMsg

// HFSearchPicker is an overlay for searching Hugging Face models.
type HFSearchPicker struct {
	searcher  HFSearcher
	query     string
	results   []ResultItem
	cursor    int
	ggufOnly  bool
	width     int
	height    int
	searching bool
	err       error
	active    bool
	epoch     int
}

// NewHFSearchPicker creates a new search picker.
func NewHFSearchPicker(searcher HFSearcher, width, height int) *HFSearchPicker {
	return &HFSearchPicker{
		searcher: searcher,
		width:    width,
		height:   height,
	}
}

// Init implements tea.Model.
func (p *HFSearchPicker) Init() tea.Cmd {
	p.active = true
	return nil
}

// Update implements tea.Model.
func (p *HFSearchPicker) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.SetSize(msg.Width, msg.Height)
		return nil
	case tea.KeyMsg:
		return p.handleKey(msg)
	case hfSearchResultMsg:
		if msg.Epoch != p.epoch {
			return nil
		}
		p.searching = false
		if msg.Err != nil {
			p.err = msg.Err
			p.results = nil
			p.cursor = 0
			return nil
		}
		p.err = nil
		p.results = p.filterResults(msg.Results)
		p.cursor = 0
		return nil
	}
	return nil
}

func (p *HFSearchPicker) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		p.active = false
		p.searcher = nil
		return nil
	case "enter":
		if len(p.results) > 0 {
			p.active = false
			p.searcher = nil
		}
		return nil
	case "up":
		if p.cursor > 0 {
			p.cursor--
		}
		return nil
	case "down":
		if p.cursor < len(p.results)-1 {
			p.cursor++
		}
		return nil
	case "ctrl+g":
		p.ggufOnly = !p.ggufOnly
		p.cursor = 0
		return nil
	case "backspace":
		if len(p.query) > 0 {
			p.query = p.query[:len(p.query)-1]
			p.epoch++
			p.searching = true
			return p.debounceSearch()
		}
		return nil
	default:
		if len(msg.Runes) == 1 && msg.Runes[0] >= 32 {
			p.query += string(msg.Runes)
			p.epoch++
			p.searching = true
			return p.debounceSearch()
		}
		return nil
	}
}

func (p *HFSearchPicker) debounceSearch() tea.Cmd {
	epoch := p.epoch
	return tea.Tick(300*time.Millisecond, func(t time.Time) tea.Msg {
		if p.searcher == nil {
			return hfSearchResultMsg{Epoch: epoch, Results: nil, Err: nil}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		raw, err := p.searcher.Search(ctx, p.query, 20)
		if err != nil {
			return hfSearchResultMsg{Epoch: epoch, Err: err}
		}
		results := make([]ResultItem, len(raw))
		for i, r := range raw {
			results[i] = ResultItem{SearchResult: r, Index: i}
		}
		return hfSearchResultMsg{Epoch: epoch, Results: results}
	})
}

func (p *HFSearchPicker) filterResults(items []ResultItem) []ResultItem {
	if !p.ggufOnly {
		return items
	}
	filtered := make([]ResultItem, 0, len(items))
	for _, it := range items {
		if hasGGUFTag(it.Tags) {
			filtered = append(filtered, it)
		}
	}
	return filtered
}

func hasGGUFTag(tags []string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, "gguf") {
			return true
		}
	}
	return false
}

// View implements tea.Model.
func (p *HFSearchPicker) View() string {
	boxW := pickerBoxWidth(p.width)

	searchLine := fmt.Sprintf("Search: %s_", p.query)
	parts := []string{theme.Subtitle.Render(searchLine)}

	if p.err != nil {
		parts = append(parts, theme.Error.Render("error: "+p.err.Error()))
	}

	if p.searching {
		parts = append(parts, theme.Subtitle.Render("Searching..."))
	}

	resultLines := make([]string, 0, len(p.results))
	for i, r := range p.results {
		label := r.ModelID
		if label == "" {
			label = r.ID
		}
		if hasGGUFTag(r.Tags) {
			label += " [GGUF]"
		}
		line := truncatePath(label, boxW-4)
		if i == p.cursor {
			line = theme.Selected.Render(line)
			if theme.NoColor() {
				line = "> " + line
			}
		}
		resultLines = append(resultLines, line)
	}
	if len(resultLines) > 0 {
		parts = append(parts, strings.Join(resultLines, "\n"))
	} else if !p.searching && p.query != "" {
		parts = append(parts, theme.Subtitle.Render("No results"))
	}

	hint := "[↑↓] move  [enter] select  [esc] close  [ctrl+g] toggle GGUF-only"
	if p.ggufOnly {
		hint = "[↑↓] move  [enter] select  [esc] close  [ctrl+g] toggle GGUF-only (ON)"
	}
	parts = append(parts, theme.Subtitle.Render(hint))

	box := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return theme.Pane.Width(boxW).Render(box)
}

// Selected returns the currently selected result.
func (p *HFSearchPicker) Selected() (ResultItem, bool) {
	if p.cursor >= 0 && p.cursor < len(p.results) {
		return p.results[p.cursor], true
	}
	return ResultItem{}, false
}

// HasGGUFTag reports whether the selected search result is tagged with "gguf".
func (i ResultItem) HasGGUFTag() bool {
	return hasGGUFTag(i.Tags)
}

// SetSize updates the picker dimensions.
func (p *HFSearchPicker) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// IsActive reports whether the picker is open.
func (p *HFSearchPicker) IsActive() bool {
	return p.active
}
