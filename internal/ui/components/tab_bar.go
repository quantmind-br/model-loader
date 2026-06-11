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
	// Badges holds an optional attention glyph per tab ("" = none). A badge
	// is folded into the label before styling so all downstream width and
	// truncation math stays untouched.
	Badges []string
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
	opts = applyTabBarDefaults(opts)
	labels := renderTabLabels(opts)
	strip := strings.Join(labels, " │ ")
	if fitsFully(strip, opts.AvailableWidth) {
		return strip
	}
	return renderTruncated(opts, labels)
}

// applyTabBarDefaults fills missing TabBarOptions fields with package
// defaults (separator, colors, padding) and returns the result.
func applyTabBarDefaults(opts TabBarOptions) TabBarOptions {
	// Under NO_COLOR prefer plain ASCII arrows: the ‹/› glyphs are easy to
	// miss (and may render poorly) on the terminals that disable color.
	left, right := "‹", "›"
	if theme.NoColor() {
		left, right = "<", ">"
	}
	if opts.LeftIndicator == "" {
		opts.LeftIndicator = left
	}
	if opts.RightIndicator == "" {
		opts.RightIndicator = right
	}
	return opts
}

// renderTabLabels produces the styled label for each tab in opts.Labels in
// order. The active tab gets the active style; the rest get the inactive
// style.
func renderTabLabels(opts TabBarOptions) []string {
	labels := make([]string, len(opts.Labels))
	for i, label := range opts.Labels {
		// Fold the badge in before any styling/bracketing so the active tab's
		// NO_COLOR brackets enclose it and width math sees the final text.
		if i < len(opts.Badges) && opts.Badges[i] != "" {
			label += " " + opts.Badges[i]
		}
		if i == opts.ActiveIndex {
			if theme.NoColor() {
				// Color can't mark the active tab; brackets do. Wrapped
				// before styling so downstream width math (lipgloss.Width
				// over rendered labels) stays correct.
				label = "[" + label + "]"
			}
			labels[i] = theme.TabActive.Render(label)
		} else {
			labels[i] = theme.TabInactive.Render(label)
		}
	}
	return labels
}

// fitsFully reports whether the joined strip plus minimum margins fits
// within the given width.
func fitsFully(strip string, width int) bool {
	return lipgloss.Width(strip) <= width
}

// renderTruncated produces a horizontally truncated strip centered on the
// active tab with left/right indicators when content is clipped.
func renderTruncated(opts TabBarOptions, labels []string) string {
	sep := " │ "
	sepWidth := lipgloss.Width(sep)

	// Compute the width of each styled label.
	widths := make([]int, len(labels))
	for i, label := range labels {
		widths[i] = lipgloss.Width(label)
	}

	// Compute the start position of each tab in the full strip.
	positions := make([]int, len(labels))
	cursor := 0
	for i := range labels {
		positions[i] = cursor
		cursor += widths[i]
		if i < len(labels)-1 {
			cursor += sepWidth
		}
	}
	totalWidth := cursor

	// Bound the active index defensively.
	activeIdx := opts.ActiveIndex
	if activeIdx < 0 {
		activeIdx = 0
	}
	if activeIdx >= len(labels) {
		activeIdx = len(labels) - 1
	}
	activeStart := positions[activeIdx]
	activeEnd := activeStart + widths[activeIdx]

	effectiveWidth := opts.AvailableWidth - 2
	if effectiveWidth < 1 {
		effectiveWidth = 1
	}

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
	for i := range labels {
		tabStart := positions[i]
		tabEnd := tabStart + widths[i]
		if tabEnd <= offset || tabStart >= offset+effectiveWidth {
			continue
		}
		visibleParts = append(visibleParts, labels[i])
	}

	// Safety net: always include the active tab even if the math goes sideways
	// (e.g. a single label wider than the viewport).
	if len(visibleParts) == 0 {
		visibleParts = append(visibleParts, labels[activeIdx])
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
		b.WriteString(opts.LeftIndicator)
	} else {
		b.WriteString(" ")
	}
	b.WriteString(visibleStrip)
	if showRight {
		b.WriteString(opts.RightIndicator)
	} else {
		b.WriteString(" ")
	}
	return b.String()
}
