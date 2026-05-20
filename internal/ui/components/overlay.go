package components

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Overlay places foreground on top of background at the center of the screen.
// Both strings are assumed to already be sized to width x height.
func Overlay(background, foreground string, width, height int) string {
	bgLines := strings.Split(background, "\n")
	fgLines := strings.Split(foreground, "\n")

	startY := (height - len(fgLines)) / 2
	if startY < 0 {
		startY = 0
	}

	var out []string
	for i := 0; i < height; i++ {
		if i >= startY && i-startY < len(fgLines) {
			fgLine := fgLines[i-startY]
			bgLine := ""
			if i < len(bgLines) {
				bgLine = bgLines[i]
			}
			out = append(out, overlayLine(bgLine, fgLine, width))
		} else if i < len(bgLines) {
			out = append(out, bgLines[i])
		} else {
			out = append(out, strings.Repeat(" ", width))
		}
	}
	return strings.Join(out, "\n")
}

func overlayLine(bg, fg string, width int) string {
	if width <= 0 {
		return fg
	}
	fgWidth := ansi.StringWidth(fg)
	startX := (width - fgWidth) / 2
	if startX < 0 {
		startX = 0
	}
	if startX == 0 && fgWidth >= width {
		return ansi.Truncate(fg, width, "")
	}

	left := ansi.Cut(bg, 0, startX)
	leftWidth := ansi.StringWidth(left)
	if leftWidth < startX {
		left += strings.Repeat(" ", startX-leftWidth)
	}

	mid := fg
	if fgWidth > width-startX {
		mid = ansi.Truncate(fg, width-startX, "")
	}

	right := ansi.Cut(bg, startX+fgWidth, ansi.StringWidth(bg))

	return left + mid + right
}
