package benchmark

import (
	"fmt"
	"sync"
	"time"
)

// activityRingCap bounds the in-memory live-activity ring. The full durable
// record for agentic modes is the teed harness log (BR7); for local modes it is
// the transcript. The ring is a live-view convenience, never persisted.
const activityRingCap = 200

// Staleness thresholds — the single place to tune hang detection for every
// surface (TUI/CLI/web). Agentic modes spend minutes per Docker step, so their
// quiet/stalled windows are far wider than the local single-turn modes.
const (
	agenticQuietAfter   = 3 * time.Minute
	agenticStalledAfter = 9 * time.Minute
	localQuietAfter     = 45 * time.Second
	localStalledAfter   = 2*time.Minute + 15*time.Second
)

// ItemState is the live state of one benchmark item (running or finished).
type ItemState struct {
	ID, Name  string
	Phase     string // "infer" | "score"
	Outcome   string // "" while running; "pass"|"fail"|"error" when done
	Score     float64
	StartedAt time.Time
	Elapsed   time.Duration
}

// ActivityEntry is one line of the live activity ring. Kind selects the render
// glyph; Outcome is set only for finished-item entries (Kind "item") so a
// renderer can pick pass/fail/error glyphs without re-deriving them.
type ActivityEntry struct {
	At      time.Time
	Kind    string // "phase" | "item" | "harness" | "stream"
	Text    string
	Outcome string // item entries only: "" (start) | "pass" | "fail" | "error"
}

// FeedSnapshot is a point-in-time copy of the authoritative run state. All three
// surfaces render from a snapshot; the lossy Progress channel is only a wake-up.
type FeedSnapshot struct {
	RunID, ProfileID, ProfileName string
	Mode         Mode
	Phase        string // last engine phase seen ("launch"|"infer"|"score"|"done")
	Index, Total int
	StartedAt    time.Time
	LastActivity time.Time     // bumped by EVERY event (incl. harness lines, stream heartbeats)
	LastItemDone time.Time     // bumped only by item_done / index-advancing poller counts
	StallTimeout time.Duration // agentic kill threshold (tb/deep-swe); 0 = no kill watchdog
	Current      *ItemState
	Done         []ItemState
	Pass, Fail, Errored int
	Activity     []ActivityEntry // ring, newest last, cap activityRingCap
}

// RunFeed is the authoritative, mutex-guarded live-run model. Every progress
// emission updates the snapshot + activity ring; the caller-visible channel gets
// a lossy forward whose drops no longer matter (the snapshot is the truth).
type RunFeed struct {
	mu   sync.Mutex
	snap FeedSnapshot
	out  chan<- Progress
}

// newRunFeed builds a feed seeded with the run identity. out is the caller's
// progress channel (may be nil); it receives lossy wake-up forwards.
func newRunFeed(out chan<- Progress, snap FeedSnapshot) *RunFeed {
	if snap.StartedAt.IsZero() {
		snap.StartedAt = time.Now()
	}
	snap.LastActivity = snap.StartedAt
	snap.LastItemDone = snap.StartedAt
	return &RunFeed{out: out, snap: snap}
}

// Emit stamps the event time (when zero), folds it into the snapshot/ring under
// the mutex, then lossy-forwards it to the caller's channel.
//
// Activity-line contract (Phase == "activity"): harness output lines are emitted
// with ProblemName == "" (Kind "harness"); token-stream heartbeats set
// ProblemName (Kind "stream"). Kind is derived here, keeping the wire struct
// free of a presentation field.
func (f *RunFeed) Emit(p Progress) {
	if f == nil {
		return
	}
	f.mu.Lock()
	if p.At.IsZero() {
		p.At = time.Now()
	}
	f.applyLocked(p)
	f.mu.Unlock()

	// Lossy forward: the wake-up stream may drop; the snapshot is authoritative.
	if f.out != nil {
		select {
		case f.out <- p:
		default:
		}
	}
}

func (f *RunFeed) applyLocked(p Progress) {
	s := &f.snap
	s.LastActivity = p.At
	// Any event advancing the item index is forward progress for the kill-clock:
	// agentic pollers report a growing Index with no item_done event.
	if p.Index > s.Index {
		s.Index = p.Index
		s.LastItemDone = p.At
	}
	if p.Total > 0 {
		s.Total = p.Total
	}

	switch p.Phase {
	case "launch":
		s.Phase = "launch"
		f.addActivityLocked(p.At, "phase", "launching backend", "")
	case "infer", "score":
		s.Phase = p.Phase
		f.startOrUpdateCurrentLocked(p)
	case "item_done":
		f.closeCurrentLocked(p)
	case "activity":
		kind := "harness"
		if p.ProblemName != "" {
			kind = "stream"
		}
		f.addActivityLocked(p.At, kind, p.Detail, "")
	case "done":
		s.Phase = "done"
		f.addActivityLocked(p.At, "phase", "aggregating results", "")
	}
}

func (f *RunFeed) startOrUpdateCurrentLocked(p Progress) {
	s := &f.snap
	if s.Current == nil || s.Current.ID != p.ProblemID {
		s.Current = &ItemState{
			ID:        p.ProblemID,
			Name:      p.ProblemName,
			Phase:     p.Phase,
			StartedAt: p.At,
		}
		f.addActivityLocked(p.At, "item", itemLabel(p.ProblemID, p.ProblemName), "")
		return
	}
	// Same item: advance phase (infer→score) and elapsed; take a richer label if
	// a poller supplied one.
	s.Current.Phase = p.Phase
	s.Current.Elapsed = p.At.Sub(s.Current.StartedAt)
	if p.ProblemName != "" {
		s.Current.Name = p.ProblemName
	}
}

func (f *RunFeed) closeCurrentLocked(p Progress) {
	s := &f.snap
	item := ItemState{ID: p.ProblemID, Name: p.ProblemName, Outcome: p.Outcome, Score: p.Score}
	if s.Current != nil && s.Current.ID == p.ProblemID {
		item.StartedAt = s.Current.StartedAt
		item.Phase = s.Current.Phase
	}
	switch {
	case p.ItemMs > 0:
		item.Elapsed = time.Duration(p.ItemMs) * time.Millisecond
	case !item.StartedAt.IsZero():
		item.Elapsed = p.At.Sub(item.StartedAt)
	}
	s.Done = append(s.Done, item)
	switch p.Outcome {
	case "pass":
		s.Pass++
	case "error":
		s.Errored++
	default:
		s.Fail++
	}
	s.LastItemDone = p.At
	if s.Current != nil && s.Current.ID == p.ProblemID {
		s.Current = nil
	}
	f.addActivityLocked(p.At, "item", itemLabel(p.ProblemID, p.ProblemName), p.Outcome)
}

func (f *RunFeed) addActivityLocked(at time.Time, kind, text, outcome string) {
	if text == "" {
		return
	}
	s := &f.snap
	s.Activity = append(s.Activity, ActivityEntry{At: at, Kind: kind, Text: text, Outcome: outcome})
	if over := len(s.Activity) - activityRingCap; over > 0 {
		s.Activity = append(s.Activity[:0], s.Activity[over:]...)
	}
}

// Snapshot returns a deep copy of the current state, safe to read after the run
// ends (the feed survives so late renders show terminal state).
func (f *RunFeed) Snapshot() FeedSnapshot {
	if f == nil {
		return FeedSnapshot{}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	snap := f.snap
	if f.snap.Current != nil {
		cur := *f.snap.Current
		snap.Current = &cur
	}
	if len(f.snap.Done) > 0 {
		snap.Done = append([]ItemState(nil), f.snap.Done...)
	}
	if len(f.snap.Activity) > 0 {
		snap.Activity = append([]ActivityEntry(nil), f.snap.Activity...)
	}
	return snap
}

// StaleLevel classifies how long a run has gone without any observable activity.
type StaleLevel int

const (
	StaleOK StaleLevel = iota
	StaleQuiet
	StaleStalled
)

// Staleness reports the activity-silence level and the elapsed silence, measured
// from LastActivity (falling back to StartedAt before the first event).
func (s FeedSnapshot) Staleness(now time.Time) (StaleLevel, time.Duration) {
	base := s.LastActivity
	if base.IsZero() {
		base = s.StartedAt
	}
	since := now.Sub(base)
	quiet, stalled := stalenessThresholds(s.Mode)
	switch {
	case since >= stalled:
		return StaleStalled, since
	case since >= quiet:
		return StaleQuiet, since
	default:
		return StaleOK, since
	}
}

func stalenessThresholds(m Mode) (quiet, stalled time.Duration) {
	if isAgentic(m) {
		return agenticQuietAfter, agenticStalledAfter
	}
	return localQuietAfter, localStalledAfter
}

// StalenessLabel is the shared human wording for a staleness level, used
// verbatim by every surface so the vocabulary never drifts.
func StalenessLabel(l StaleLevel, since time.Duration) string {
	d := since.Truncate(time.Second)
	switch l {
	case StaleStalled:
		return fmt.Sprintf("possibly hung — no activity for %s", d)
	case StaleQuiet:
		return fmt.Sprintf("quiet — last activity %s ago", d)
	default:
		return fmt.Sprintf("last activity %s ago", d)
	}
}

// isAgentic reports whether a mode wraps an external Docker harness (wider
// staleness windows, kill watchdog for tb/deep-swe).
func isAgentic(m Mode) bool {
	switch m {
	case ModeTerminalBench, ModeSweBenchPro, ModeDeepSWE:
		return true
	}
	return false
}

// itemLabel prefers the human name, falling back to the id.
func itemLabel(id, name string) string {
	if name != "" {
		return name
	}
	return id
}

// truncateRunes clips s to at most max runes, appending an ellipsis when it cut.
// Rune-safe so multi-byte harness output never splits mid-character.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}
