package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// sseJudgeServer streams a fixed verdict string as one SSE content delta, then
// [DONE]. When recordBody is non-nil it captures the last request body.
func sseJudgeServer(verdict string, recordBody *string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if recordBody != nil {
			b, _ := io.ReadAll(r.Body)
			*recordBody = string(b)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		chunk := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": verdict}}}}
		bs, _ := json.Marshal(chunk)
		_, _ = w.Write([]byte("data: " + string(bs) + "\n\n"))
		if fl != nil {
			fl.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

func TestJudgeScorer_ReferenceGuidedAndMedian(t *testing.T) {
	verdict := `{"localization":1,"correctness":1,"completeness":1,"score":0.9,"resolved":true,"rationale":"functionally equivalent"}`
	var body string
	srv := sseJudgeServer(verdict, &body)
	defer srv.Close()

	js := judgeScorer{doer: srv.Client(), base: srv.URL, model: "judge", maxTok: 256, samples: 3}
	p := Problem{RepoName: "acme/lib", Statement: "off-by-one in count", GoldenPatch: "diff --git a/c.py b/c.py\n+    return n + 1\n"}
	sc, err := js.Score(context.Background(), p, "```diff\ndiff --git a/c.py b/c.py\n+    return n + 1\n```")
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if !sc.Resolved || sc.Score < 0.89 || sc.Score > 0.91 {
		t.Errorf("score = %+v, want resolved with median 0.9", sc)
	}
	// reference-guided: the judge prompt must carry the gold patch + candidate.
	if !strings.Contains(body, "Reference patch") || !strings.Contains(body, "return n + 1") {
		t.Errorf("judge request not reference-guided; body=%q", body)
	}
}

func TestParseJudgeVerdict_ToleratesProse(t *testing.T) {
	v, err := parseJudgeVerdict("Sure, here is my verdict:\n{\"score\":0.4,\"resolved\":false,\"rationale\":\"wrong file\"} thanks")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if v.Resolved || v.Score != 0.4 {
		t.Errorf("verdict = %+v, want score 0.4 unresolved", v)
	}
}

func TestJudgeSystem_IncludesMaintainability(t *testing.T) {
	if !strings.Contains(judgeSystem, "maintainability") {
		t.Error("judge rubric should include the maintainability criterion")
	}
	for _, w := range []string{"0.30", "0.45", "0.15", "0.10"} {
		if !strings.Contains(judgeSystem, w) {
			t.Errorf("judge rubric missing weight %s", w)
		}
	}
}

func TestMedian(t *testing.T) {
	cases := []struct {
		in   []float64
		want float64
	}{
		{[]float64{0.2, 0.8, 0.5}, 0.5},
		{[]float64{1, 0}, 0.5},
		{[]float64{0.3}, 0.3},
		{nil, 0},
	}
	for _, c := range cases {
		if got := median(c.in); got != c.want {
			t.Errorf("median(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// fakeScorer adapts a func to the Scorer interface for pipeline tests.
type fakeScorer struct {
	fn func(ctx context.Context, p Problem, response string) (ProblemScore, error)
}

func (f fakeScorer) Score(ctx context.Context, p Problem, response string) (ProblemScore, error) {
	return f.fn(ctx, p, response)
}

// sseModelServer streams one fixed content chunk per request and reports each
// request through onRequest (called with the 1-based request count).
func sseModelServer(onRequest func(n int)) *httptest.Server {
	var calls atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		if onRequest != nil {
			onRequest(n)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"candidate answer\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

func TestJudgeExecute_OverlapsScoringWithNextInference(t *testing.T) {
	secondInferStarted := make(chan struct{})
	srv := sseModelServer(func(n int) {
		if n == 2 {
			close(secondInferStarted)
		}
	})
	defer srv.Close()

	overlapped := make(chan bool, 1)
	scorer := fakeScorer{fn: func(ctx context.Context, p Problem, response string) (ProblemScore, error) {
		if p.ID == "p1" {
			// Block the FIRST score until the SECOND inference has started; in a
			// serial pipeline this deadlocks, so the timeout below detects it.
			select {
			case <-secondInferStarted:
				overlapped <- true
			case <-time.After(5 * time.Second):
				overlapped <- false
			}
			return ProblemScore{Resolved: true, Score: 0.9, Detail: "judged p1"}, nil
		}
		return ProblemScore{Resolved: false, Score: 0.5, Detail: "judged " + p.ID}, nil
	}}

	r := &Runner{
		cfg:      Config{MaxTokens: 64, Timeout: 5 * time.Second, SaveTranscripts: true, Judge: JudgeEndpoint{Samples: 1}},
		problems: []Problem{{ID: "p1", Name: "p1"}, {ID: "p2", Name: "p2"}, {ID: "p3", Name: "p3"}},
	}
	results, trs, err := judgeHandler{}.Execute(context.Background(), r, srv.URL, "m", scorer, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !<-overlapped {
		t.Fatal("second inference did not start while first score was pending (no pipeline overlap)")
	}
	if len(results) != 3 || len(trs) != 3 {
		t.Fatalf("results/transcripts = %d/%d, want 3/3", len(results), len(trs))
	}
	for i, want := range []string{"p1", "p2", "p3"} {
		if results[i].ProblemID != want {
			t.Errorf("results[%d] = %q, want %q (dataset order must be preserved)", i, results[i].ProblemID, want)
		}
	}
	if !results[0].Resolved || results[0].Score != 0.9 {
		t.Errorf("results[0] = %+v, want the blocked score to land in slot 0", results[0])
	}
	if results[1].Detail != "judged p2" || results[2].Detail != "judged p3" {
		t.Errorf("later scores misplaced: %q / %q", results[1].Detail, results[2].Detail)
	}
}

func TestJudgeExecute_ScoringErrorIsPerProblem(t *testing.T) {
	srv := sseModelServer(nil)
	defer srv.Close()

	scorer := fakeScorer{fn: func(ctx context.Context, p Problem, response string) (ProblemScore, error) {
		if p.ID == "p1" {
			return ProblemScore{}, errors.New("judge endpoint down")
		}
		return ProblemScore{Resolved: true, Score: 1}, nil
	}}
	r := &Runner{
		cfg:      Config{MaxTokens: 64, Timeout: 5 * time.Second, Judge: JudgeEndpoint{Samples: 1}},
		problems: []Problem{{ID: "p1", Name: "p1"}, {ID: "p2", Name: "p2"}},
	}
	results, _, err := judgeHandler{}.Execute(context.Background(), r, srv.URL, "m", scorer, nil)
	if err != nil {
		t.Fatalf("Execute should not abort on a scoring error: %v", err)
	}
	if results[0].Err == "" {
		t.Error("p1 should carry the judge error")
	}
	if !results[1].Resolved {
		t.Error("p2 should still be scored")
	}
}

func TestJudgeExecute_CancelDoesNotHang(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := sseModelServer(func(n int) {
		if n == 2 {
			cancel() // cancel mid-run while p1 is still scoring
		}
	})
	defer srv.Close()

	scorer := fakeScorer{fn: func(sctx context.Context, p Problem, response string) (ProblemScore, error) {
		<-sctx.Done() // a cancelled run must unblock pending judges
		return ProblemScore{}, sctx.Err()
	}}
	r := &Runner{
		cfg:      Config{MaxTokens: 64, Timeout: 5 * time.Second, Judge: JudgeEndpoint{Samples: 1}},
		problems: []Problem{{ID: "p1", Name: "p1"}, {ID: "p2", Name: "p2"}, {ID: "p3", Name: "p3"}},
	}
	done := make(chan struct{})
	var results []ProblemResult
	go func() {
		results, _, _ = judgeHandler{}.Execute(ctx, r, srv.URL, "m", scorer, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Execute hung after cancellation")
	}
	if len(results) == 0 {
		t.Fatal("expected partial results from the cancelled run")
	}
}
