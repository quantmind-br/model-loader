package components

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// FilterLine renders the shared one-line filter indicator used by the
// Models library, the model picker, and the Benchmark profile picker.
//
// While the user is typing (active) it shows a live prompt with a block
// cursor: the "filter: " prefix in theme.Subtitle and the text + "█"
// cursor plain, so the prompt stays legible under NO_COLOR. When a filter
// is applied but not being edited it shows the quoted filter text. Empty
// string when there is nothing to show, so callers can join it directly.
func FilterLine(active bool, filter string) string {
	if active {
		return theme.Subtitle.Render("filter: ") + filter + "█"
	}
	if filter != "" {
		return theme.Subtitle.Render(fmt.Sprintf("filter: %q", filter))
	}
	return ""
}
