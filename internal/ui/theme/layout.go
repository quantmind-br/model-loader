package theme

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
