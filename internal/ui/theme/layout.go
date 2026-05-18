package theme

import "github.com/charmbracelet/lipgloss"

const (
	TabBarHeight    = 1
	StatusBarHeight = 1
	PageGutter      = 2
	PanePaddingX    = 1
	PanePaddingY    = 0
	MinPaneWidth    = 20
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
