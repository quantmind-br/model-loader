package components

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// HFSearcher is the minimal interface needed to search the Hugging Face Hub.
type HFSearcher interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

// SearchResult mirrors hfhub.SearchResult so the component package does not
// need to import internal/service/hfhub.
type SearchResult struct {
	ID            string
	Author        string
	ModelID       string
	Tags          []string
	Downloads     int
	Likes         int
	LastModified  time.Time
	LibraryName   string
	PipelineTag   string
}

// ResultItem is the local representation used by the picker.
type ResultItem struct {
	SearchResult
	Index int
}

// HFSearchPicker is an overlay for searching Hugging Face models.
type HFSearchPicker struct {
	searcher HFSearcher
	query    string
	results  []ResultItem
	cursor   int
	ggufOnly bool
	width    int
	height   int
	searching bool
	err      error
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
	return nil
}

// Update implements tea.Model.
func (p *HFSearchPicker) Update(msg tea.Msg) tea.Cmd {
	return nil
}

// View implements tea.Model.
func (p *HFSearchPicker) View() string {
	return "HF Search (not implemented)"
}

// Selected returns the currently selected result.
func (p *HFSearchPicker) Selected() (ResultItem, bool) {
	return ResultItem{}, false
}

// SetSize updates the picker dimensions.
func (p *HFSearchPicker) SetSize(w, h int) {
	p.width = w
	p.height = h
}

// IsActive reports whether the picker is open.
func (p *HFSearchPicker) IsActive() bool {
	return true
}
