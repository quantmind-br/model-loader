package benchmark

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ProblemScore is the scorer verdict for a single problem.
type ProblemScore struct {
	Resolved bool
	Score    float64 // 0..1
	Detail   string
	// Raw holds the judge's full raw replies (one per sample) for transcript
	// debugging. Empty for non-judge scorers.
	Raw []string
}

// Scorer grades a model's raw response for a problem.
type Scorer interface {
	Score(ctx context.Context, p Problem, response string) (ProblemScore, error)
}

// judgeScorer is the only scoring mode: a reference-guided LLM judge with a
// structured rubric. To fight single-run noise it samples the judge `samples`
// times and aggregates by median score + majority resolved (self-consistency).
type judgeScorer struct {
	doer    httpDoer
	base    string
	apiKey  string
	model   string
	maxTok  int
	samples int
	// activity, when set, is a per-second token heartbeat forwarded to the run
	// feed so a slow judge endpoint still shows liveness.
	activity func(int)
}

// judgeSystem instructs the judge to score against a reference patch using a
// weighted rubric and to emit a single strict JSON object (constrained output
// reduces drift). Functional equivalence is favored over syntactic identity.
const judgeSystem = `You are a strict code-review judge. You are given a bug report, a REFERENCE patch ` +
	`that is known to fix it, and a CANDIDATE answer from another model. Judge whether the candidate ` +
	`would actually fix the bug, comparing for FUNCTIONAL EQUIVALENCE to the reference (not byte-for-byte ` +
	`identity; different but correct fixes are fine).

Score these weighted criteria, each 0..1:
- localization (0.30): does it target the correct file(s) and function(s) as the reference?
- correctness (0.45): would the change actually fix the reported bug, like the reference does?
- completeness (0.15): does it cover the cases the reference covers, without breaking others?
- maintainability (0.10): does it avoid new dependencies, dead code, or changes likely to break existing tests?

Reply with ONLY a JSON object, no prose:
{"localization":<0..1>,"correctness":<0..1>,"completeness":<0..1>,"maintainability":<0..1>,"score":<weighted 0..1>,"resolved":<bool>,"rationale":"<=160 chars"}
resolved must be true only when the candidate is functionally equivalent to the reference fix.`

func (j judgeScorer) Score(ctx context.Context, p Problem, response string) (ProblemScore, error) {
	candidate, ok := ExtractDiff(response)
	if !ok {
		candidate = response // let the judge see the raw answer when no diff block
	}
	user := buildJudgeUser(p, candidate)

	n := max(j.samples, 1)
	scores := make([]float64, 0, n)
	raws := make([]string, 0, n)
	resolvedVotes := 0
	var lastRationale string
	for i := range n {
		// Sample 0 is deterministic (temp 0); extra samples add a little
		// temperature so self-consistency aggregation has something to average.
		temp := 0.0
		if i > 0 {
			temp = 0.3
		}
		v, raw, err := j.callOnce(ctx, user, temp)
		if raw != "" {
			raws = append(raws, raw)
		}
		if err != nil {
			if len(scores) == 0 {
				return ProblemScore{Raw: raws}, err
			}
			break // degrade gracefully to the samples we already have
		}
		scores = append(scores, v.Score)
		if v.Resolved {
			resolvedVotes++
		}
		lastRationale = v.Rationale
	}

	score := median(scores)
	resolved := resolvedVotes*2 > len(scores) // strict majority
	detail := fmt.Sprintf("judge x%d median=%.2f resolved-votes=%d/%d", len(scores), score, resolvedVotes, len(scores))
	if lastRationale != "" {
		detail += " — " + lastRationale
	}
	return ProblemScore{Resolved: resolved, Score: score, Detail: detail, Raw: raws}, nil
}

func buildJudgeUser(p Problem, candidate string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Bug report (%s)\n%s\n\n", p.RepoName, p.Statement)
	fmt.Fprintf(&b, "# Reference patch (known-good)\n```diff\n%s\n```\n\n", p.GoldenPatch)
	fmt.Fprintf(&b, "# Candidate answer\n```diff\n%s\n```\n", candidate)
	return b.String()
}

type judgeVerdict struct {
	Score     float64
	Resolved  bool
	Rationale string
}

func (j judgeScorer) callOnce(ctx context.Context, user string, temp float64) (judgeVerdict, string, error) {
	res, err := Complete(ctx, j.doer, j.base, j.apiKey, ChatRequest{
		Model:       j.model,
		Temperature: new(temp),
		MaxTokens:   j.maxTok,
		OnDelta:     j.activity,
		Messages: []ChatMessage{
			{Role: "system", Content: judgeSystem},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return judgeVerdict{}, "", fmt.Errorf("judge request: %w", err)
	}
	v, err := parseJudgeVerdict(res.Content)
	if err != nil {
		return judgeVerdict{}, res.Content, fmt.Errorf("parse judge verdict: %w", err)
	}
	return v, res.Content, nil
}

func parseJudgeVerdict(content string) (judgeVerdict, error) {
	v, err := parseVerdict(content)
	if err != nil {
		return judgeVerdict{}, err
	}
	return judgeVerdict{Score: v.Score, Resolved: v.Resolved, Rationale: v.Rationale}, nil
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return (s[mid-1] + s[mid]) / 2
}
