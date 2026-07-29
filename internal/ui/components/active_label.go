package components

import "github.com/quantmind-br/model-loader/internal/ui/theme"

// ActiveLabel renders one entry of a tab or sub-view strip.
func ActiveLabel(label string, active bool) string {
	if active {
		if theme.NoColor() {
			label = "[" + label + "]"
		}
		return theme.TabActive.Render(label)
	}
	return theme.TabInactive.Render(label)
}
