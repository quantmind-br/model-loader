package benchmark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGradeResult_Defaults(t *testing.T) {
	var r gradeResult
	if r.Score != 0 || r.Pass {
		t.Fatalf("zero gradeResult should be Score 0, Pass false, got %+v", r)
	}
}

func TestLLMGrader_ParsesScoreAndFlagsSource(t *testing.T) {
	reply := `{"score":0.8,"pass":true,"rationale":"answer is grounded in the context"}`
	body := "data: {\"choices\":[{\"delta\":{\"content\":" + jsonQuote(reply) + "}}]}\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":20}}\n" +
		"data: [DONE]\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	g := llmGrader{base: srv.URL, model: "m", maxTok: 256, judgedBy: "self"}
	got, err := g.Grade(context.Background(), gradeRequest{
		Criterion: "faithfulness", Guidance: "answer uses only the context",
		Question: "q", Context: "ctx", Answer: "a",
	})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if got.Score != 0.8 || !got.Pass {
		t.Errorf("got Score=%v Pass=%v, want 0.8/true", got.Score, got.Pass)
	}
	if got.JudgedBy != "self" {
		t.Errorf("JudgedBy = %q, want self", got.JudgedBy)
	}
	if got.Raw == "" {
		t.Error("Raw should carry the grader reply for transcripts")
	}
}

func TestLLMGrader_ClampsAndDefaults(t *testing.T) {
	reply := `{"score":1.7,"pass":false,"rationale":"x"}`
	body := "data: {\"choices\":[{\"delta\":{\"content\":" + jsonQuote(reply) + "}}]}\n" +
		"data: [DONE]\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	g := llmGrader{base: srv.URL, model: "m", maxTok: 256, judgedBy: "external"}
	got, err := g.Grade(context.Background(), gradeRequest{Criterion: "c", Answer: "a"})
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if got.Score != 1.0 {
		t.Errorf("Score = %v, want clamped to 1.0", got.Score)
	}
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
