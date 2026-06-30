package components

import (
	"strings"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// MetricBar renders frac (0..1) as a width-column horizontal bar. Uses '█'
// for filled and '░' for empty, degrading to '#'/'-' under NO_COLOR. frac is
// clamped to [0,1]; width<=0 returns "".
func MetricBar(frac float64, width int) string {
	if width <= 0 {
		return ""
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	full, empty := '█', '░'
	if theme.NoColor() {
		full, empty = '#', '-'
	}
	filled := int(frac*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	return strings.Repeat(string(full), filled) + strings.Repeat(string(empty), width-filled)
}
