package benchmark

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuildRagasPrompt_IncludesDocsAndQuestion(t *testing.T) {
	p := RagasProblem{
		Documents: []string{"Doc A says X.", "Doc B says Y."},
		Question:  "What does Doc A say?",
	}
	got := buildRagasPrompt(p)
	for _, want := range []string{"Doc A says X.", "Doc B says Y.", "What does Doc A say?"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q\n%s", want, got)
		}
	}
}

func TestRagasDetail_RoundTrip(t *testing.T) {
	d := ragasDetail(0.8, 0.9, 0.75, "self")
	f, r, p, ok := parseRagasScores(d)
	if !ok {
		t.Fatalf("parseRagasScores failed on %q", d)
	}
	if math.Abs(f-0.8) > 1e-9 || math.Abs(r-0.9) > 1e-9 || math.Abs(p-0.75) > 1e-9 {
		t.Errorf("round-trip mismatch: got f=%v r=%v p=%v", f, r, p)
	}
}

func TestRagasFinalize_AveragesCriteria(t *testing.T) {
	problems := []ProblemResult{
		{Detail: ragasDetail(1.0, 0.8, 0.6, "self")},
		{Detail: ragasDetail(0.0, 0.4, 0.4, "self")},
	}
	var agg Aggregate
	ragasHandler{}.Finalize(&agg, problems)
	if math.Abs(agg.RagasFaithfulness-0.5) > 1e-9 {
		t.Errorf("RagasFaithfulness = %v, want 0.5", agg.RagasFaithfulness)
	}
	if math.Abs(agg.RagasRelevancy-0.6) > 1e-9 {
		t.Errorf("RagasRelevancy = %v, want 0.6", agg.RagasRelevancy)
	}
	if math.Abs(agg.RagasPrecision-0.5) > 1e-9 {
		t.Errorf("RagasPrecision = %v, want 0.5", agg.RagasPrecision)
	}
}

// sseRagasServer is an httptest stub that handles all /v1/chat/completions calls:
// - The first call (model answer) returns a simple content string.
// - Subsequent calls (grader) return a strict JSON verdict with score 0.9.
// It returns both the server and an atomic counter so callers can assert total hits.
func sseRagasServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/chat/completions") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)

		n := callCount.Add(1) // increment first; n==1 means first call
		var content string
		if n == 1 {
			content = "Doc A says X."
		} else {
			verdict := map[string]any{"score": 0.9, "pass": true, "rationale": "ok"}
			bs, _ := json.Marshal(verdict)
			content = string(bs)
		}

		chunk := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": content}}}}
		bs, _ := json.Marshal(chunk)
		_, _ = w.Write([]byte("data: " + string(bs) + "\n\n"))
		if fl != nil {
			fl.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	return srv, &callCount
}

func TestRunRagas_EndToEnd(t *testing.T) {
	srv, callCount := sseRagasServer(t)
	defer srv.Close()

	r := &Runner{cfg: Config{MaxTokens: 64, Timeout: 5 * time.Second}}
	g := r.graderFor(srv.URL, "stub-model")

	p := RagasProblem{
		ID:              "test-001",
		Documents:       []string{"Doc A says X.", "Doc B says Y."},
		Question:        "What does Doc A say?",
		GroundTruth:     "Doc A says X.",
		ExpectedContext: "Doc A says X.",
	}

	res, _ := r.runRagas(context.Background(), srv.URL, "stub-model", g, p)
	if res.Err != "" {
		t.Fatalf("runRagas error: %s", res.Err)
	}
	_, _, _, ok := parseRagasScores(res.Detail)
	if !ok {
		t.Errorf("parseRagasScores failed on Detail=%q", res.Detail)
	}
	if got := callCount.Load(); got != 4 {
		t.Errorf("stub call count = %d, want 4 (1 answer + 3 grader)", got)
	}
}
