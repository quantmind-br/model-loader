package benchmark

import (
	"context"
	"fmt"
	"strings"
)

// gradeRequest is the input to a grader: a single criterion plus the material
// needed to judge an answer against it.
type gradeRequest struct {
	Criterion string // e.g. "faithfulness", "answer relevancy", "coherence"
	Guidance  string // one-line description of what a passing answer looks like
	Question  string // the prompt/question posed to the model under test (optional)
	Context   string // reference material (docs, golden patch, expected facts)
	Answer    string // the model-under-test answer being graded
}

// gradeResult is a grader's verdict for one criterion.
type gradeResult struct {
	Score    float64 // 0..1
	Pass     bool
	Detail   string
	Raw      string // raw grader reply, for transcripts
	JudgedBy string // "external" or "self"
}

// grader scores free-form model output against a single criterion (0..1).
type grader interface {
	Grade(ctx context.Context, req gradeRequest) (gradeResult, error)
}

// graderSystem instructs the grader to score ONE criterion and emit strict JSON.
const graderSystem = `You are a strict evaluator. You are given a CRITERION, optional QUESTION and ` +
	`reference CONTEXT, and a candidate ANSWER from another model. Score how well the answer satisfies ` +
	`the criterion, from 0.0 (fails completely) to 1.0 (fully satisfies). Judge only the stated criterion.

Reply with ONLY a JSON object, no prose:
{"score":<0..1>,"pass":<bool>,"rationale":"<=160 chars"}
pass must be true only when the answer clearly satisfies the criterion.`

// llmGrader grades via an OpenAI-compatible chat endpoint. With base/model/apiKey
// pointing at an external judge it is the gold standard; pointed at the
// model-under-test's own server it is the zero-config self-judge (judgedBy=self).
type llmGrader struct {
	doer     httpDoer
	base     string
	apiKey   string
	model    string
	maxTok   int
	judgedBy string // "external" or "self"
}

func (g llmGrader) Grade(ctx context.Context, req gradeRequest) (gradeResult, error) {
	user := buildGraderUser(req)
	comp, err := Complete(ctx, g.doer, g.base, g.apiKey, ChatRequest{
		Model:       g.model,
		Temperature: 0,
		MaxTokens:   g.maxTok,
		Messages: []ChatMessage{
			{Role: "system", Content: graderSystem},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return gradeResult{JudgedBy: g.judgedBy}, fmt.Errorf("grader request: %w", err)
	}
	v, err := parseGraderVerdict(comp.Content)
	if err != nil {
		return gradeResult{Raw: comp.Content, JudgedBy: g.judgedBy}, fmt.Errorf("parse grader verdict: %w", err)
	}
	detail := fmt.Sprintf("%s=%.2f (%s)", req.Criterion, v.score, g.judgedBy)
	if v.rationale != "" {
		detail += " — " + v.rationale
	}
	return gradeResult{Score: v.score, Pass: v.pass, Detail: detail, Raw: comp.Content, JudgedBy: g.judgedBy}, nil
}

func buildGraderUser(req gradeRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Criterion\n%s", req.Criterion)
	if req.Guidance != "" {
		fmt.Fprintf(&b, " — %s", req.Guidance)
	}
	b.WriteString("\n\n")
	if req.Question != "" {
		fmt.Fprintf(&b, "# Question\n%s\n\n", req.Question)
	}
	if req.Context != "" {
		fmt.Fprintf(&b, "# Context\n%s\n\n", req.Context)
	}
	fmt.Fprintf(&b, "# Answer\n%s\n", req.Answer)
	return b.String()
}

type graderVerdict struct {
	score     float64
	pass      bool
	rationale string
}

func parseGraderVerdict(content string) (graderVerdict, error) {
	v, err := parseVerdict(content)
	if err != nil {
		return graderVerdict{}, err
	}
	return graderVerdict{score: v.Score, pass: v.Pass, rationale: v.Rationale}, nil
}
