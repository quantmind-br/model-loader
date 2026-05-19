package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

const (
	TabBarHeight    = 1
	StatusBarHeight = 1
	PageGutter      = 2
	PanePaddingX    = 1
	PanePaddingY    = 0
	MinPaneWidth    = 20

	// Responsive breakpoints (in columns) for two-pane page layouts.
	// Below NarrowWidthThreshold the right pane stacks below the left.
	// Between Narrow and Wide a 60/40 split favors the master list.
	// Above WideWidthThreshold a balanced 50/50 split is used so the
	// detail pane can show more information.
	NarrowWidthThreshold = 100
	WideWidthThreshold   = 160
)

// LayoutMode describes how a two-pane page should arrange its content.
type LayoutMode int

const (
	// LayoutStacked stacks the right pane below the left at narrow widths.
	LayoutStacked LayoutMode = iota
	// LayoutSplit6040 puts the master list on the left at 60% width and
	// the detail pane on the right at 40%. Used for medium widths.
	LayoutSplit6040
	// LayoutSplit5050 evenly splits the two panes. Used for wide widths.
	LayoutSplit5050
)

// BodyHeight returns the usable body height after chrome (tab bar + status bar).
func BodyHeight(totalHeight int) int {
	if totalHeight <= TabBarHeight+StatusBarHeight {
		return 0
	}
	return totalHeight - TabBarHeight - StatusBarHeight
}

// ClampBody truncates/restricts the rendered string to the given width and height.
func ClampBody(s string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxWidth(width).
		MaxHeight(height).
		Render(s)
}

// SplitTwoPanes returns left/right widths for a two-pane layout with gutter.
func SplitTwoPanes(totalWidth int) (leftWidth int, rightWidth int) {
	if totalWidth <= PageGutter {
		return MinPaneWidth, MinPaneWidth
	}
	available := totalWidth - PageGutter
	leftWidth = available / 2
	rightWidth = available - leftWidth
	if leftWidth < MinPaneWidth {
		leftWidth = MinPaneWidth
	}
	if rightWidth < MinPaneWidth {
		rightWidth = MinPaneWidth
	}
	return leftWidth, rightWidth
}

// ResponsiveSplit returns the layout mode and pane widths for a master-detail
// page given the available terminal width. Callers should consult mode to decide
// whether to stack the detail pane below the master list (LayoutStacked) or to
// join them horizontally (LayoutSplit6040, LayoutSplit5050). In stacked mode
// both returned widths span (totalWidth - PageGutter) so each pane can render
// using the full row independently.
func ResponsiveSplit(totalWidth int) (mode LayoutMode, leftWidth int, rightWidth int) {
	if totalWidth <= PageGutter {
		return LayoutStacked, MinPaneWidth, MinPaneWidth
	}
	available := totalWidth - PageGutter
	if totalWidth < NarrowWidthThreshold {
		w := available
		if w < MinPaneWidth {
			w = MinPaneWidth
		}
		return LayoutStacked, w, w
	}
	if totalWidth <= WideWidthThreshold {
		leftWidth = available * 60 / 100
		rightWidth = available - leftWidth
		if leftWidth < MinPaneWidth {
			leftWidth = MinPaneWidth
		}
		if rightWidth < MinPaneWidth {
			rightWidth = MinPaneWidth
		}
		return LayoutSplit6040, leftWidth, rightWidth
	}
	leftWidth = available / 2
	rightWidth = available - leftWidth
	if leftWidth < MinPaneWidth {
		leftWidth = MinPaneWidth
	}
	if rightWidth < MinPaneWidth {
		rightWidth = MinPaneWidth
	}
	return LayoutSplit5050, leftWidth, rightWidth
}

// RuneWidth returns the visual display width of a string, accounting for
// wide characters (CJK, emoji) and combining marks.
func RuneWidth(s string) int {
	return runewidth.StringWidth(s)
}

// PadRuneWidth pads s with spaces on the right so its visual width reaches
// the target width. If s is already wider, it is returned unchanged.
func PadRuneWidth(s string, width int) string {
	w := RuneWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// TruncateRuneWidth clips s to the target visual width, appending tail when
// the input is wider. Delegates to runewidth.Truncate for accurate cell
// counting across CJK, emoji, and combining marks.
func TruncateRuneWidth(s string, width int, tail string) string {
	return runewidth.Truncate(s, width, tail)
}
