package components

import (
	"strings"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

var sparkBars = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// asciiBars is the NO_COLOR fallback ramp: terminals that ask for color
// suppression are often the ones without good Unicode block support, so we
// degrade to a pure-ASCII 8-step ramp.
var asciiBars = []rune{'_', '.', ':', '-', '=', '+', '*', '#'}

// Sparkline renders values into width columns using an 8-step Unicode
// block ramp (▁..█), falling back to an ASCII ramp (_..#) under NO_COLOR.
// Empty input returns width spaces. Flat line uses the lowest bar.
func Sparkline(values []float64, width int) string {
	if width <= 0 {
		return ""
	}
	if len(values) == 0 {
		return strings.Repeat(" ", width)
	}
	bars := sparkBars
	if theme.NoColor() {
		bars = asciiBars
	}
	buckets := make([]float64, width)
	bucketCount := make([]int, width)
	for i, v := range values {
		idx := i * width / len(values)
		if idx >= width {
			idx = width - 1
		}
		buckets[idx] += v
		bucketCount[idx]++
	}
	for i := range buckets {
		if bucketCount[i] > 0 {
			buckets[i] /= float64(bucketCount[i])
		}
	}
	min, max := buckets[0], buckets[0]
	for _, v := range buckets {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	rng := max - min
	var b strings.Builder
	for _, v := range buckets {
		var idx int
		if rng == 0 {
			idx = 0
		} else {
			idx = int((v - min) / rng * float64(len(bars)-1))
			if idx < 0 {
				idx = 0
			}
			if idx >= len(bars) {
				idx = len(bars) - 1
			}
		}
		b.WriteRune(bars[idx])
	}
	return b.String()
}
