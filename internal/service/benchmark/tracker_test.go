package benchmark

import (
	"testing"
	"time"
)

// base is a fixed clock so staleness / activity-time assertions are deterministic.
var trackerBase = time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

func TestRunFeed_ItemLifecycle(t *testing.T) {
	f := newRunFeed(nil, FeedSnapshot{Mode: ModeMMLUBench, Total: 3, StartedAt: trackerBase})
	f.Emit(Progress{Index: 1, Total: 3, ProblemID: "p1", ProblemName: "n1", Phase: "infer", At: trackerBase.Add(time.Second)})

	snap := f.Snapshot()
	if snap.Current == nil || snap.Current.ID != "p1" {
		t.Fatalf("Current not set to p1: %+v", snap.Current)
	}
	if snap.Current.Phase != "infer" {
		t.Errorf("Current.Phase = %q, want infer", snap.Current.Phase)
	}
	if snap.Phase != "infer" {
		t.Errorf("snap.Phase = %q, want infer", snap.Phase)
	}

	f.Emit(Progress{Index: 1, Total: 3, ProblemID: "p1", ProblemName: "n1", Phase: "item_done",
		Outcome: "pass", Score: 1, ItemMs: 1500, At: trackerBase.Add(2 * time.Second)})
	snap = f.Snapshot()
	if snap.Current != nil {
		t.Errorf("Current not cleared after item_done: %+v", snap.Current)
	}
	if snap.Pass != 1 || snap.Fail != 0 || snap.Errored != 0 {
		t.Errorf("tallies = %d/%d/%d, want 1/0/0", snap.Pass, snap.Fail, snap.Errored)
	}
	if len(snap.Done) != 1 || snap.Done[0].Outcome != "pass" {
		t.Fatalf("Done = %+v, want one pass", snap.Done)
	}
	if snap.Done[0].Elapsed != 1500*time.Millisecond {
		t.Errorf("Done[0].Elapsed = %v, want 1.5s (from ItemMs)", snap.Done[0].Elapsed)
	}
}

func TestRunFeed_Tallies(t *testing.T) {
	f := newRunFeed(nil, FeedSnapshot{Mode: ModeMathBench, StartedAt: trackerBase})
	for i, oc := range []string{"pass", "fail", "error", "pass"} {
		at := trackerBase.Add(time.Duration(i) * time.Second)
		f.Emit(Progress{Index: i + 1, ProblemID: "p", Phase: "item_done", Outcome: oc, At: at})
	}
	snap := f.Snapshot()
	if snap.Pass != 2 || snap.Fail != 1 || snap.Errored != 1 {
		t.Errorf("tallies = %d/%d/%d, want 2/1/1", snap.Pass, snap.Fail, snap.Errored)
	}
}

func TestRunFeed_ActivityRingCap(t *testing.T) {
	f := newRunFeed(nil, FeedSnapshot{Mode: ModeMMLUBench, StartedAt: trackerBase})
	const n = activityRingCap + 50
	for i := range n {
		f.Emit(Progress{Phase: "activity", Detail: itoaLine(i), At: trackerBase.Add(time.Duration(i) * time.Millisecond)})
	}
	snap := f.Snapshot()
	if len(snap.Activity) != activityRingCap {
		t.Fatalf("ring len = %d, want %d", len(snap.Activity), activityRingCap)
	}
	// Newest at the tail; oldest survivors dropped.
	if got := snap.Activity[len(snap.Activity)-1].Text; got != itoaLine(n-1) {
		t.Errorf("last entry = %q, want %q", got, itoaLine(n-1))
	}
	if got := snap.Activity[0].Text; got != itoaLine(n-activityRingCap) {
		t.Errorf("first entry = %q, want %q", got, itoaLine(n-activityRingCap))
	}
}

func TestRunFeed_LastActivityVsLastItemDone(t *testing.T) {
	f := newRunFeed(nil, FeedSnapshot{Mode: ModeMMLUBench, StartedAt: trackerBase})
	// A harness/stream activity line bumps LastActivity but NOT LastItemDone.
	act := trackerBase.Add(10 * time.Second)
	f.Emit(Progress{Phase: "activity", Detail: "harness line", At: act})
	snap := f.Snapshot()
	if !snap.LastActivity.Equal(act) {
		t.Errorf("LastActivity = %v, want %v", snap.LastActivity, act)
	}
	if !snap.LastItemDone.Equal(trackerBase) {
		t.Errorf("LastItemDone moved on activity: %v, want %v", snap.LastItemDone, trackerBase)
	}
	// An item_done bumps both.
	done := trackerBase.Add(20 * time.Second)
	f.Emit(Progress{Index: 1, Phase: "item_done", Outcome: "pass", ProblemID: "p", At: done})
	snap = f.Snapshot()
	if !snap.LastItemDone.Equal(done) {
		t.Errorf("LastItemDone = %v, want %v", snap.LastItemDone, done)
	}
}

func TestRunFeed_IndexAdvanceBumpsItemDone(t *testing.T) {
	f := newRunFeed(nil, FeedSnapshot{Mode: ModeTerminalBench, StartedAt: trackerBase})
	// Agentic poller reports a growing Index with a coarse infer event (no
	// item_done). It must still advance the kill clock.
	at := trackerBase.Add(30 * time.Second)
	f.Emit(Progress{Index: 5, Total: 10, ProblemID: "terminal-bench", ProblemName: "5/10", Phase: "infer", At: at})
	snap := f.Snapshot()
	if snap.Index != 5 {
		t.Errorf("Index = %d, want 5", snap.Index)
	}
	if !snap.LastItemDone.Equal(at) {
		t.Errorf("LastItemDone = %v, want %v (index advance)", snap.LastItemDone, at)
	}
}

func TestFeedSnapshot_StalenessLocal(t *testing.T) {
	snap := FeedSnapshot{Mode: ModeMMLUBench, StartedAt: trackerBase, LastActivity: trackerBase}
	tests := []struct {
		after time.Duration
		want  StaleLevel
	}{
		{10 * time.Second, StaleOK},
		{localQuietAfter, StaleQuiet},
		{localQuietAfter + time.Second, StaleQuiet},
		{localStalledAfter, StaleStalled},
		{5 * time.Minute, StaleStalled},
	}
	for _, tt := range tests {
		got, _ := snap.Staleness(trackerBase.Add(tt.after))
		if got != tt.want {
			t.Errorf("local staleness after %v = %v, want %v", tt.after, got, tt.want)
		}
	}
}

func TestFeedSnapshot_StalenessAgentic(t *testing.T) {
	snap := FeedSnapshot{Mode: ModeTerminalBench, StartedAt: trackerBase, LastActivity: trackerBase}
	tests := []struct {
		after time.Duration
		want  StaleLevel
	}{
		{2 * time.Minute, StaleOK}, // local would be Stalled here; agentic is fine
		{agenticQuietAfter, StaleQuiet},
		{agenticStalledAfter, StaleStalled},
	}
	for _, tt := range tests {
		got, _ := snap.Staleness(trackerBase.Add(tt.after))
		if got != tt.want {
			t.Errorf("agentic staleness after %v = %v, want %v", tt.after, got, tt.want)
		}
	}
}

func TestStalenessLabel(t *testing.T) {
	if got := StalenessLabel(StaleOK, 5*time.Second); got != "last activity 5s ago" {
		t.Errorf("OK label = %q", got)
	}
	if got := StalenessLabel(StaleQuiet, 90*time.Second); got != "quiet — last activity 1m30s ago" {
		t.Errorf("Quiet label = %q", got)
	}
	if got := StalenessLabel(StaleStalled, 4*time.Minute); got != "possibly hung — no activity for 4m0s" {
		t.Errorf("Stalled label = %q", got)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("hello", 10); got != "hello" {
		t.Errorf("no-trunc = %q", got)
	}
	if got := truncateRunes("hello", 3); got != "he…" {
		t.Errorf("trunc = %q, want he…", got)
	}
	// Multi-byte safety: never split a rune.
	if got := truncateRunes("héllo wörld", 4); got != "hél…" {
		t.Errorf("multibyte trunc = %q", got)
	}
}

// itoaLine builds a distinct, non-blank activity text for entry i.
func itoaLine(i int) string {
	return "line-" + timeKey(i)
}

func timeKey(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{digits[i%10]}, b...)
		i /= 10
	}
	return string(b)
}

func TestRunFeed_ItemDoneDetailPreserved(t *testing.T) {
	f := newRunFeed(nil, FeedSnapshot{Mode: ModeMathBench, StartedAt: trackerBase})
	f.Emit(Progress{Index: 1, ProblemID: "p1", ProblemName: "n1", Phase: "item_done",
		Outcome: "error", Detail: "boom", At: trackerBase.Add(time.Second)})
	snap := f.Snapshot()
	if len(snap.Done) != 1 {
		t.Fatalf("Done = %+v, want one item", snap.Done)
	}
	if snap.Done[0].Detail != "boom" {
		t.Errorf("Done[0].Detail = %q, want boom (item error text preserved)", snap.Done[0].Detail)
	}
	if snap.Done[0].Outcome != "error" {
		t.Errorf("Done[0].Outcome = %q, want error", snap.Done[0].Outcome)
	}
}
