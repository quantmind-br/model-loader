package pages

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// renderDiscoverView renders the Hugging Face Discover section. When the
// search overlay or file picker is active it shows them inline; otherwise it
// shows a short landing prompt. The page header, section tabs and flash are
// added by View.
func (p ModelsPage) renderDiscoverView() string {
	if p.hfSearch != nil && p.hfSearch.IsActive() {
		return p.hfSearch.View()
	}
	if p.hfFilePicker != nil && p.hfFilePicker.IsActive() {
		return p.hfFilePicker.View()
	}
	if p.hfClient == nil {
		return theme.Subtitle.Render("Hugging Face search is not available (no HF client wired).")
	}
	lines := []string{
		theme.Subtitle.Render("Discover GGUF models on the Hugging Face Hub."),
		"",
		"Press [enter] or [s] to search. Selected files download into your first",
		"configured models path and show up in the Downloads section.",
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}
