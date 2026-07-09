package pages

import (
	"fmt"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// benchCol describes one column of a benchmark table. w is the fixed display
// width in cells; exactly one column per table may set w==0 to mark it as the
// flexible column that absorbs whatever width remains. prio orders shedding:
// when the fixed widths + gaps overflow the available width, columns are dropped
// highest-prio-number first (prio 0 columns are essential and never dropped).
// right right-aligns the cell.
type benchCol struct {
	title string
	w     int
	prio  int
	right bool
}

// fitColumns returns the subset of cols that fit within width. It drops the
// highest-prio-number droppable columns (prio > 0, never the flexible column)
// until the fixed widths plus 2-space inter-column gaps fit, then resolves the
// flexible (w==0) column to whatever width remains, floored at 10. Input order
// is preserved. width<=0 returns cols unchanged (unsized — callers render full).
func fitColumns(width int, cols []benchCol) []benchCol {
	if width <= 0 || len(cols) == 0 {
		return cols
	}
	kept := make([]benchCol, len(cols))
	copy(kept, cols)

	// remainingFor reports the width left for a flexible column (or the slack for
	// a fixed-only table) and whether the set fits.
	remainingFor := func(cs []benchCol) (int, bool) {
		if len(cs) == 0 {
			return width, true
		}
		fixed, hasFlex := 0, false
		for _, c := range cs {
			if c.w == 0 {
				hasFlex = true
			} else {
				fixed += c.w
			}
		}
		remaining := width - fixed - 2*(len(cs)-1)
		if hasFlex {
			return remaining, remaining >= 10
		}
		return remaining, remaining >= 0
	}

	for {
		if _, ok := remainingFor(kept); ok {
			break
		}
		// Drop the highest-prio-number droppable column (rightmost on a tie).
		drop := -1
		for i, c := range kept {
			if c.w == 0 || c.prio <= 0 {
				continue
			}
			if drop == -1 || c.prio >= kept[drop].prio {
				drop = i
			}
		}
		if drop == -1 {
			break // nothing left to shed; the final clamp is the safety net
		}
		kept = append(kept[:drop], kept[drop+1:]...)
	}

	remaining, _ := remainingFor(kept)
	for i := range kept {
		if kept[i].w == 0 {
			kept[i].w = max(10, remaining)
		}
	}
	return kept
}

// renderCells lays cells into cols, padding/truncating each to its resolved
// width (rune-aware) and right-aligning the columns that ask for it. Cells are
// joined with a 2-space gap. Missing cells render blank; extra cells are
// ignored. A flexible column left unresolved (w==0) falls back to width 10.
func renderCells(cols []benchCol, cells []string) string {
	parts := make([]string, 0, len(cols))
	for i, c := range cols {
		var cell string
		if i < len(cells) {
			cell = cells[i]
		}
		w := c.w
		if w <= 0 {
			w = 10
		}
		cell = theme.TruncateRuneWidth(cell, w, "…")
		if c.right {
			cell = padLeftRW(cell, w)
		} else {
			cell = theme.PadRuneWidth(cell, w)
		}
		parts = append(parts, cell)
	}
	return strings.Join(parts, "  ")
}

// renderHeader renders the column titles as a header row through renderCells.
func renderHeader(cols []benchCol) string {
	titles := make([]string, len(cols))
	for i, c := range cols {
		titles[i] = c.title
	}
	return renderCells(cols, titles)
}

// padLeftRW left-pads s with spaces to the target display width (right-align),
// counting cells rune-aware. Returns s unchanged when already wide enough.
func padLeftRW(s string, w int) string {
	gap := w - theme.RuneWidth(s)
	if gap <= 0 {
		return s
	}
	return strings.Repeat(" ", gap) + s
}

// visibleWindow returns the [start,end) slice of total rows to display so the
// cursor stays visible within an h-row budget. When rows are clipped above or
// below, one row of the budget is reserved for each "↑/↓ N more" marker (the
// caller renders them). total<=h returns the whole range.
func visibleWindow(cursor, total, h int) (int, int) {
	if h <= 0 || total <= 0 {
		return 0, 0
	}
	if total <= h {
		return 0, total
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= total {
		cursor = total - 1
	}
	markers := 1 // total>h ⇒ at least one marker is always needed
	var start, end int
	for range 4 {
		vis := max(1, h-markers)
		start = cursor - vis/2
		if start < 0 {
			start = 0
		}
		end = start + vis
		if end > total {
			end = total
			start = max(0, end-vis)
		}
		next := 0
		if start > 0 {
			next++
		}
		if end < total {
			next++
		}
		if next == markers {
			break
		}
		markers = max(1, next)
	}
	return start, end
}

// windowedRows slices rows to a cursor-following window fitting h lines, adding
// dim "↑ N more" / "↓ N more" markers (counted against h) when rows are clipped.
// h<=0 or rows already fitting returns rows unchanged.
func windowedRows(rows []string, cursor, h int) []string {
	if h <= 0 || len(rows) <= h {
		return rows
	}
	start, end := visibleWindow(cursor, len(rows), h)
	out := make([]string, 0, h)
	if start > 0 {
		out = append(out, theme.Subtitle.Render(fmt.Sprintf("↑ %d more", start)))
	}
	out = append(out, rows[start:end]...)
	if end < len(rows) {
		out = append(out, theme.Subtitle.Render(fmt.Sprintf("↓ %d more", len(rows)-end)))
	}
	return out
}

// splitViewLines flattens parts into individual display lines (splitting any
// embedded newlines) so row-budget math counts real rows, not logical sections.
func splitViewLines(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			out = append(out, "")
			continue
		}
		out = append(out, strings.Split(p, "\n")...)
	}
	return out
}

// composeWindowed pins top and bottom sections, windows the scrollable middle
// around cursor to whatever height remains, and clamps the whole to the page
// budget. When the page is unsized (width/height<=0, as in unit tests) it joins
// everything untouched so substring assertions see the full content.
func (p BenchmarkPage) composeWindowed(top, scroll, bottom []string, cursor int) string {
	topL := splitViewLines(top)
	botL := splitViewLines(bottom)
	scrollL := splitViewLines(scroll)
	if p.width <= 0 || p.height <= 0 {
		all := append(append(append([]string{}, topL...), scrollL...), botL...)
		return strings.Join(all, "\n")
	}
	avail := max(1, p.height-len(topL)-len(botL))
	windowed := windowedRows(scrollL, cursor, avail)
	all := append(append(append([]string{}, topL...), windowed...), botL...)
	return p.clampBody(strings.Join(all, "\n"))
}

// clampBody fits s into the page's width×height (hard truncation, exact height)
// when the page is sized; unsized pages return s untouched.
func (p BenchmarkPage) clampBody(s string) string {
	if p.width <= 0 || p.height <= 0 {
		return s
	}
	return theme.ClampBody(s, p.width, p.height)
}

// rowLeftRight justifies left and right within width, filling the middle with
// spaces (min 1). Narrow widths truncate the left segment so right stays visible.
func rowLeftRight(left, right string, width int) string {
	if width <= 0 {
		return left + "  " + right
	}
	rw := theme.RuneWidth(right)
	if lw := theme.RuneWidth(left); lw+rw+1 > width {
		left = truncate(left, max(1, width-rw-1))
	}
	gap := max(1, width-theme.RuneWidth(left)-rw)
	return left + strings.Repeat(" ", gap) + right
}

// tailLines returns at most n trailing lines (newest-last input kept).
func tailLines(lines []string, n int) []string {
	if n <= 0 {
		return nil
	}
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}

// fmtDur renders a duration compactly: "9s", "3m04s", "1h07m" (seconds floor).
func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Truncate(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh%02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm%02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}
