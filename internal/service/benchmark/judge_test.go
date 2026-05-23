package benchmark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
