package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

// SGR color codes for the live renderer. Raw constants (no lipgloss in the CLI
// package); applied only when writing to a TTY with NO_COLOR unset.
const (
	sgrReset  = "\x1b[0m"
	sgrGreen  = "\x1b[32m"
	sgrRed    = "\x1b[31m"
	sgrYellow = "\x1b[33m"
	sgrDim    = "\x1b[2m"
)

// benchLive renders a benchmark run's progress to a writer (stderr).
//
// It prints permanent, chronological lines derived by diffing feed snapshots:
// item-done lines are lossless (the feed's Done slice never shrinks, so a
// high-water mark recovers every finish even though the wake-up channel is
// lossy); item-start, phase, and staleness-transition lines are best-effort.
// On a TTY it also repaints a transient status block below the permanent lines
// (progress bar, tallies, now-line, recent activity, staleness/watchdog), moved
// up and cleared before each repaint. Non-TTY output is pure lines, zero ANSI.
type benchLive struct {
	w       io.Writer
	feed    func() *benchmark.RunFeed
	verbose bool
	tty     bool
	color   bool
	width   int

	lastDone       int    // count of Done items already printed (high-water mark)
	lastPhase      string // last snap.Phase we emitted a line for
	curID          string // id of the current item we printed a start line for
	curStartedAt   time.Time
	lastActivityAt time.Time // newest activity timestamp already printed (verbose)
	staleLevel     benchmark.StaleLevel
	liveLines      int // rows in the last painted live block (TTY)
}

// newBenchLive builds a renderer for w. TTY/color/width are probed from the
// writer's file descriptor, so a bytes.Buffer (tests) is always plain.
func newBenchLive(w io.Writer, feed func() *benchmark.RunFeed, verbose bool) *benchLive {
	l := &benchLive{w: w, feed: feed, verbose: verbose, width: 80, staleLevel: benchmark.StaleOK}
	if f, ok := w.(interface{ Fd() uintptr }); ok {
		fd := int(f.Fd())
		if term.IsTerminal(fd) {
			l.tty = true
			l.color = os.Getenv("NO_COLOR") == ""
			if wd, _, err := term.GetSize(fd); err == nil && wd > 0 {
				l.width = wd
			}
		}
	}
	return l
}

// run drives the renderer until progress closes. Every wake-up event and every
// 1s tick triggers a poll; the ticker keeps the elapsed clock and staleness
// footer live even when the run is quiet.
func (l *benchLive) run(progress <-chan benchmark.Progress) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case _, ok := <-progress:
			if !ok {
				l.flush()
				return
			}
			l.poll()
		case <-ticker.C:
			l.poll()
		}
	}
}

// poll renders any new permanent lines and repaints the live block.
func (l *benchLive) poll() {
	snap := l.feed().Snapshot()
	if snap.StartedAt.IsZero() {
		return // no run started yet
	}
	l.clearLive()
	l.renderPermanent(snap)
	l.renderLive(snap)
}

// flush renders the final permanent lines and clears the live block so the
// caller's summary is the last thing on screen.
func (l *benchLive) flush() {
	l.clearLive()
	snap := l.feed().Snapshot()
	if snap.StartedAt.IsZero() {
		return
	}
	l.renderPermanent(snap)
}

// clearLive erases the previously painted live block (TTY only) by moving the
// cursor up over it and clearing to the end of the screen.
func (l *benchLive) clearLive() {
	if !l.tty || l.liveLines == 0 {
		return
	}
	fmt.Fprintf(l.w, "\x1b[%dA\x1b[0J", l.liveLines)
	l.liveLines = 0
}

// renderPermanent prints new chronological lines: phase transitions, finished
// items (lossless), the current item start, verbose activity, and staleness
// transitions.
func (l *benchLive) renderPermanent(snap benchmark.FeedSnapshot) {
	// 1. Phase transitions we surface as a line: launch and aggregating.
	if snap.Phase != l.lastPhase {
		switch snap.Phase {
		case "launch":
			l.printLine(l.dim("loading profile via proxy — waiting for backend health (large models can take minutes)…"))
		case "done":
			l.printLine(l.dim("aggregating…"))
		}
		l.lastPhase = snap.Phase
	}
	// 2. Finished items (lossless via the Done high-water mark). These completed
	// before the current item started, so they print first.
	for i := l.lastDone; i < len(snap.Done); i++ {
		l.printItemDone(snap, i, snap.Done[i])
	}
	l.lastDone = len(snap.Done)
	// 3. Current item start (best-effort, from a Current change).
	if snap.Current != nil && (snap.Current.ID != l.curID || !snap.Current.StartedAt.Equal(l.curStartedAt)) {
		l.curID = snap.Current.ID
		l.curStartedAt = snap.Current.StartedAt
		l.printItemStart(snap, *snap.Current)
	}
	// 4. Verbose harness/stream activity lines.
	if l.verbose && len(snap.Activity) > 0 {
		for _, e := range snap.Activity {
			if e.At.After(l.lastActivityAt) && (e.Kind == "harness" || e.Kind == "stream") {
				l.printLine(l.dim(e.At.Format("15:04:05") + " · " + e.Text))
			}
		}
		l.lastActivityAt = snap.Activity[len(snap.Activity)-1].At
	}
	// 5. Staleness transitions, printed once per level change.
	if lvl, since := snap.Staleness(time.Now()); lvl != l.staleLevel {
		l.staleLevel = lvl
		if lvl != benchmark.StaleOK {
			l.printLine(l.staleColor(lvl, benchmark.StalenessLabel(lvl, since)))
		}
	}
}

func (l *benchLive) printItemStart(snap benchmark.FeedSnapshot, it benchmark.ItemState) {
	ts := it.StartedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	line := fmt.Sprintf("%s %s %s %s", ts.Format("15:04:05"), l.col(sgrDim, "→"), progressCount(snap.Index, snap.Total), itemName(it))
	if it.Phase != "" {
		line += " " + it.Phase
	}
	l.printLine(line)
}

func (l *benchLive) printItemDone(snap benchmark.FeedSnapshot, i int, it benchmark.ItemState) {
	glyph, code := outcomeGlyph(it.Outcome)
	ts := it.StartedAt.Add(it.Elapsed)
	if it.StartedAt.IsZero() {
		ts = time.Now()
	}
	line := fmt.Sprintf("%s %s %s %s %s", ts.Format("15:04:05"), l.col(code, glyph), progressCount(i+1, snap.Total), itemName(it), it.Outcome)
	if it.Outcome == "error" {
		if it.Detail != "" {
			line += ": " + it.Detail
		}
	} else {
		line += fmt.Sprintf(" score=%.2f", it.Score)
	}
	if it.Elapsed > 0 {
		line += " (" + fmtDur(it.Elapsed) + ")"
	}
	l.printLine(line)
}

// renderLive repaints the transient status block (TTY only).
func (l *benchLive) renderLive(snap benchmark.FeedSnapshot) {
	if !l.tty {
		return
	}
	now := time.Now()
	lines := []string{l.liveProgressLine(snap)}
	if snap.Current != nil {
		lines = append(lines, l.liveNowLine(snap, now))
	}
	lines = append(lines, l.tailActivity(snap, 3)...)
	lines = append(lines, l.liveStatusLine(snap, now))

	for _, s := range lines {
		out := truncANSI(s, l.width)
		if l.color {
			out += sgrReset
		}
		fmt.Fprintln(l.w, out)
	}
	l.liveLines = len(lines)
}

func (l *benchLive) liveProgressLine(snap benchmark.FeedSnapshot) string {
	var b strings.Builder
	if snap.Total > 0 {
		barW := l.width / 2
		if barW > 40 {
			barW = 40
		}
		if barW < 10 {
			barW = 10
		}
		filled := int(float64(barW) * float64(snap.Index) / float64(snap.Total))
		if filled > barW {
			filled = barW
		}
		if filled < 0 {
			filled = 0
		}
		b.WriteString("[")
		b.WriteString(strings.Repeat("=", filled))
		b.WriteString(strings.Repeat(" ", barW-filled))
		b.WriteString("] ")
		fmt.Fprintf(&b, "%d/%d", snap.Index, snap.Total)
	} else {
		fmt.Fprintf(&b, "item %d", snap.Index)
	}
	b.WriteString("  ")
	b.WriteString(l.col(sgrGreen, fmt.Sprintf("✓%d", snap.Pass)))
	b.WriteString(" ")
	b.WriteString(l.col(sgrRed, fmt.Sprintf("✗%d", snap.Fail)))
	b.WriteString(" ")
	b.WriteString(l.col(sgrYellow, fmt.Sprintf("!%d", snap.Errored)))
	return b.String()
}

func (l *benchLive) liveNowLine(snap benchmark.FeedSnapshot, now time.Time) string {
	cur := snap.Current
	line := fmt.Sprintf("now: %s — %s %s", itemName(*cur), cur.Phase, fmtDur(now.Sub(cur.StartedAt)))
	if d := latestStream(snap); d != "" {
		line += " · " + d
	}
	return line
}

func (l *benchLive) tailActivity(snap benchmark.FeedSnapshot, n int) []string {
	if len(snap.Activity) == 0 {
		return nil
	}
	start := len(snap.Activity) - n
	if start < 0 {
		start = 0
	}
	out := make([]string, 0, n)
	for _, e := range snap.Activity[start:] {
		out = append(out, l.activityLine(e))
	}
	return out
}

func (l *benchLive) activityLine(e benchmark.ActivityEntry) string {
	var glyph string
	switch e.Kind {
	case "item":
		switch e.Outcome {
		case "pass":
			glyph = l.col(sgrGreen, "✓")
		case "fail":
			glyph = l.col(sgrRed, "✗")
		case "error":
			glyph = l.col(sgrYellow, "!")
		default:
			glyph = "→"
		}
	case "phase":
		glyph = l.col(sgrDim, "•")
	default: // harness / stream
		glyph = l.col(sgrDim, "·")
	}
	return fmt.Sprintf("%s %s %s", e.At.Format("15:04:05"), glyph, e.Text)
}

func (l *benchLive) liveStatusLine(snap benchmark.FeedSnapshot, now time.Time) string {
	lvl, since := snap.Staleness(now)
	s := l.staleColor(lvl, benchmark.StalenessLabel(lvl, since))
	if snap.StallTimeout > 0 {
		if remain := snap.StallTimeout - now.Sub(snap.LastItemDone); remain > 0 {
			s += l.col(sgrDim, fmt.Sprintf(" — watchdog kill in %s", fmtDur(remain)))
		}
	}
	return s
}

func (l *benchLive) printLine(s string) { fmt.Fprintln(l.w, s) }

// col wraps s in an SGR code when color is enabled.
func (l *benchLive) col(code, s string) string {
	if l.color && code != "" {
		return code + s + sgrReset
	}
	return s
}

func (l *benchLive) dim(s string) string { return l.col(sgrDim, s) }

func (l *benchLive) staleColor(lvl benchmark.StaleLevel, s string) string {
	switch lvl {
	case benchmark.StaleStalled:
		return l.col(sgrRed, s)
	case benchmark.StaleQuiet:
		return l.col(sgrYellow, s)
	default:
		return l.dim(s)
	}
}

// outcomeGlyph maps an item outcome to a glyph and its color code.
func outcomeGlyph(outcome string) (glyph, code string) {
	switch outcome {
	case "pass":
		return "✓", sgrGreen
	case "error":
		return "!", sgrYellow
	default:
		return "✗", sgrRed
	}
}

// progressCount renders "idx/total", dropping the denominator when the total is
// unknown (agentic harness modes).
func progressCount(idx, total int) string {
	if total > 0 {
		return fmt.Sprintf("%d/%d", idx, total)
	}
	return fmt.Sprintf("%d", idx)
}

// itemName prefers the human name, falling back to the id.
func itemName(it benchmark.ItemState) string {
	if it.Name != "" {
		return it.Name
	}
	return it.ID
}

// latestStream returns the newest token-stream heartbeat text, if any.
func latestStream(snap benchmark.FeedSnapshot) string {
	for i := len(snap.Activity) - 1; i >= 0; i-- {
		if snap.Activity[i].Kind == "stream" {
			return snap.Activity[i].Text
		}
	}
	return ""
}

// fmtDur renders a duration as a compact, zero-padded clock: "45s", "2m01s",
// "1h05m03s".
func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Truncate(time.Second)
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	s := int((d % time.Minute) / time.Second)
	switch {
	case h > 0:
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	case m > 0:
		return fmt.Sprintf("%dm%02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// truncANSI truncates s to at most max display cells, copying SGR escape
// sequences verbatim (they cost no width) so the live block never exceeds the
// terminal width and the cursor-up repaint math stays exact.
func truncANSI(s string, max int) string {
	if max <= 0 {
		return ""
	}
	var b strings.Builder
	w := 0
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == 0x1b { // ESC: copy the CSI sequence, ESC..final-byte, no width
			b.WriteRune(r)
			i++
			for i < len(runes) {
				b.WriteRune(runes[i])
				c := runes[i]
				if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
					break
				}
				i++
			}
			continue
		}
		rw := runewidth.RuneWidth(r)
		if w+rw > max {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String()
}
