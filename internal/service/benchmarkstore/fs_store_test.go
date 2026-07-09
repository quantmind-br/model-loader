package benchmarkstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

func newRun(id, profileID string, started time.Time) benchmark.Run {
	return benchmark.Run{
		ID:          id,
		ProfileID:   profileID,
		ProfileName: profileID,
		Mode:        benchmark.ModeJudge,
		StartedAt:   started,
	}
}

func TestSaveListByProfileDelete(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	now := time.Now()
	must(t, s.Save(newRun("a-1", "alpha", now.Add(-2*time.Hour))))
	must(t, s.Save(newRun("a-2", "alpha", now)))
	must(t, s.Save(newRun("b-1", "beta", now.Add(-time.Hour))))

	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("List len = %d, want 3", len(all))
	}
	// newest first
	if all[0].ID != "a-2" {
		t.Errorf("List[0] = %q, want a-2 (newest)", all[0].ID)
	}

	alpha, err := s.ListByProfile("alpha")
	if err != nil {
		t.Fatalf("ListByProfile: %v", err)
	}
	if len(alpha) != 2 {
		t.Errorf("alpha runs = %d, want 2", len(alpha))
	}

	if err := s.Delete("a-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Delete("missing"); err != ErrNotFound {
		t.Errorf("Delete missing err = %v, want ErrNotFound", err)
	}
}

func TestList_SkipsCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	must(t, s.Save(newRun("ok-1", "alpha", time.Now())))
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("List len = %d, want 1 (corrupt skipped)", len(all))
	}
}

func TestList_MissingDir(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "does-not-exist"))
	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("List len = %d, want 0", len(all))
	}
}

func TestTranscriptSaveLoadAndListSkips(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	run := newRun("t-1", "alpha", time.Now())
	run.Transcript = []benchmark.ProblemTranscript{
		{ProblemID: "p1", ModelResponse: "raw answer", DiffFound: true, JudgeRaw: []string{`{"score":1}`}},
	}
	must(t, s.Save(run))

	// List must not surface the sidecar transcript file as a run.
	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("List len = %d, want 1 (transcript sidecar must be skipped)", len(all))
	}

	tr, err := s.LoadTranscript("t-1")
	if err != nil {
		t.Fatalf("LoadTranscript: %v", err)
	}
	if len(tr) != 1 || tr[0].ModelResponse != "raw answer" || !tr[0].DiffFound {
		t.Errorf("transcript = %+v, want the saved record", tr)
	}

	// Missing transcript -> ErrNotFound.
	if _, err := s.LoadTranscript("nope"); err != ErrNotFound {
		t.Errorf("LoadTranscript(missing) = %v, want ErrNotFound", err)
	}

	// Delete removes the sidecar too.
	must(t, s.Delete("t-1"))
	if _, err := s.LoadTranscript("t-1"); err != ErrNotFound {
		t.Errorf("transcript survived Delete: %v", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// BR10: run ids that could escape the runs dir (empty, "..", path separators)
// must be rejected at every entry point and never reach the filesystem.
func TestTraversalIDsRejected(t *testing.T) {
	parent := t.TempDir()
	runs := filepath.Join(parent, "runs")
	s := New(runs)

	// A sentinel sibling of the runs dir that a traversal id could target.
	sentinel := filepath.Join(parent, "sentinel.json")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
		t.Fatalf("seed sentinel: %v", err)
	}

	bad := []string{"", "..", "../sentinel", "../../foo", "a/b", `a\b`, "sub/../x"}
	for _, id := range bad {
		if _, err := s.Load(id); err != ErrNotFound {
			t.Errorf("Load(%q) = %v, want ErrNotFound", id, err)
		}
		if _, err := s.LoadTranscript(id); err != ErrNotFound {
			t.Errorf("LoadTranscript(%q) = %v, want ErrNotFound", id, err)
		}
		if err := s.Delete(id); err != ErrNotFound {
			t.Errorf("Delete(%q) = %v, want ErrNotFound", id, err)
		}
		if err := s.Save(benchmark.Run{ID: id}); err == nil {
			t.Errorf("Save(id=%q) = nil, want non-nil error", id)
		}
	}

	// The sentinel must be untouched: no traversal delete removed it and no
	// save overwrote it.
	b, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("traversal id escaped the runs dir (sentinel gone): %v", err)
	}
	if string(b) != "keep" {
		t.Fatalf("traversal id overwrote sentinel: %q", b)
	}
}
