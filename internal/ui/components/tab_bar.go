package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// TabBarOptions configures the tab bar rendering.
type TabBarOptions struct {
	Labels         []string
	ActiveIndex    int
	AvailableWidth int
	LeftIndicator  string // default "‹"
	RightIndicator string // default "›"
}

// TabBar renders a horizontally-scrollable tab strip. The active tab is always
// fully visible. When labels overflow AvailableWidth, side indicators show
// which directions have clipped tabs.
//
// Behavior:
//   - When all tab labels fit within AvailableWidth, the strip is rendered
//     normally with no indicators or scrolling.
//   - When labels overflow, one column on each side is reserved for the
//     indicator characters. The viewport offset is computed so the active
//     tab is fully visible, attempting to center it when possible.
//   - Labels are never truncated, abbreviated, or wrapped. The strip always
//     renders on exactly one row.
func TabBar(opts TabBarOptions) string {
	if opts.AvailableWidth <= 0 || len(opts.Labels) == 0 {
		return ""
	}

	leftInd := opts.LeftIndicator
	if leftInd == "" {
		leftInd = "‹"
	}
	rightInd := opts.RightIndicator
	if rightInd == "" {
		rightInd = "›"
	}

	sep := " │ "
	sepWidth := lipgloss.Width(sep)

	// Pre-render each tab label and compute widths.
	rendered := make([]string, len(opts.Labels))
	widths := make([]int, len(opts.Labels))
	for i, label := range opts.Labels {
		if i == opts.ActiveIndex {
			rendered[i] = theme.TabActive.Render(label)
		} else {
			rendered[i] = theme.TabInactive.Render(label)
		}
		widths[i] = lipgloss.Width(rendered[i])
	}

	// Compute total width including separators.
	totalWidth := 0
	for i, w := range widths {
		totalWidth += w
		if i < len(widths)-1 {
			totalWidth += sepWidth
		}
	}

	// If everything fits, render normally with separators.
	if totalWidth <= opts.AvailableWidth {
		parts := make([]string, 0, 2*len(opts.Labels))
		for i := range opts.Labels {
			if i > 0 {
				parts = append(parts, sep)
			}
			parts = append(parts, rendered[i])
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	}

	// Overflow mode: reserve 1 column on each side for indicators.
	effectiveWidth := opts.AvailableWidth - 2
	if effectiveWidth < 1 {
		effectiveWidth = 1
	}

	// Compute the start position of each tab in the full strip.
	positions := make([]int, len(opts.Labels))
	cursor := 0
	for i := range opts.Labels {
		positions[i] = cursor
		cursor += widths[i]
		if i < len(opts.Labels)-1 {
			cursor += sepWidth
		}
	}

	// Bound the active index defensively.
	activeIdx := opts.ActiveIndex
	if activeIdx < 0 {
		activeIdx = 0
	}
	if activeIdx >= len(opts.Labels) {
		activeIdx = len(opts.Labels) - 1
	}
	activeStart := positions[activeIdx]
	activeEnd := activeStart + widths[activeIdx]

	// Center the active tab when possible.
	offset := activeStart - (effectiveWidth-widths[activeIdx])/2
	if offset < 0 {
		offset = 0
	}
	// Ensure active tab is fully visible on the right.
	if activeEnd > offset+effectiveWidth {
		offset = activeEnd - effectiveWidth
	}
	// Clamp offset to the valid range.
	maxOffset := totalWidth - effectiveWidth
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}

	showLeft := offset > 0
	showRight := offset+effectiveWidth < totalWidth

	// Build the visible strip by taking tabs that overlap the viewport.
	var visibleParts []string
	for i := range opts.Labels {
		tabStart := positions[i]
		tabEnd := tabStart + widths[i]
		if tabEnd <= offset || tabStart >= offset+effectiveWidth {
			continue
		}
		visibleParts = append(visibleParts, rendered[i])
	}

	// Safety net: always include the active tab even if the math goes sideways
	// (e.g. a single label wider than the viewport).
	if len(visibleParts) == 0 {
		visibleParts = append(visibleParts, rendered[activeIdx])
	}

	visibleStrip := lipgloss.JoinHorizontal(lipgloss.Top, visibleParts...)
	visibleW := lipgloss.Width(visibleStrip)
	if visibleW > effectiveWidth {
		visibleStrip = lipgloss.NewStyle().MaxWidth(effectiveWidth).Render(visibleStrip)
	} else if visibleW < effectiveWidth {
		visibleStrip = visibleStrip + strings.Repeat(" ", effectiveWidth-visibleW)
	}

	var b strings.Builder
	if showLeft {
		b.WriteString(leftInd)
	} else {
		b.WriteString(" ")
	}
	b.WriteString(visibleStrip)
	if showRight {
		b.WriteString(rightInd)
	} else {
		b.WriteString(" ")
	}
	return b.String()
}
