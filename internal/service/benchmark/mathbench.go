package benchmark

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

//go:embed data/gsm8k_curated.json
var mathDataset []byte

// MathProblem is one curated math-reasoning item.
type MathProblem struct {
	ID         string `json:"id"`
	Question   string `json:"question"`
	Answer     string `json:"answer"`     // normalized numeric string (e.g. "42")
	Difficulty int    `json:"difficulty"` // 1..3 by reasoning-step count
}

// loadMathProblems decodes the embedded GSM8K subset.
func loadMathProblems() ([]MathProblem, error) {
	var ps []MathProblem
	if err := json.Unmarshal(mathDataset, &ps); err != nil {
		return nil, fmt.Errorf("decode math dataset: %w", err)
	}
	if len(ps) == 0 {
		return nil, fmt.Errorf("math dataset is empty")
	}
	return ps, nil
}

var (
	thinkRe  = regexp.MustCompile(`(?is)<think>.*?</think>`)
	hashRe   = regexp.MustCompile(`####\s*(-?[\d,]+(?:\.\d+)?)`)
	answerRe = regexp.MustCompile(`(?i)(?:final answer|the answer is|answer:)\s*\$?(-?[\d,]+(?:\.\d+)?)`)
	numberRe = regexp.MustCompile(`-?[\d,]+(?:\.\d+)?`)
)

// normalizeNumber canonicalizes a numeric string: strip $, commas, surrounding
// space; drop a trailing ".0"; lowercase non-numeric text so it still compares.
func normalizeNumber(s string) string {
	s = strings.TrimSpace(s)
	cleaned := strings.NewReplacer("$", "", ",", "", " ", "").Replace(s)
	if f, err := strconv.ParseFloat(cleaned, 64); err == nil {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strings.ToLower(s)
}

// extractFinalAnswer pulls the model's final numeric answer from a response,
// stripping any <think> chain first and preferring explicit markers (#### or
// "the answer is") over the last bare number.
func extractFinalAnswer(response string) string {
	clean := thinkRe.ReplaceAllString(response, " ")
	if m := hashRe.FindStringSubmatch(clean); m != nil {
		return normalizeNumber(m[1])
	}
	if m := answerRe.FindStringSubmatch(clean); m != nil {
		return normalizeNumber(m[1])
	}
	nums := numberRe.FindAllString(clean, -1)
	if len(nums) > 0 {
		return normalizeNumber(nums[len(nums)-1])
	}
	return ""
}

// matchAnswer reports whether the model response yields the expected answer
// after normalization.
func matchAnswer(expected, response string) bool {
	got := extractFinalAnswer(response)
	want := normalizeNumber(expected)
	return got != "" && got == want
}

// runMathBench asks the model one math question and scores an exact numeric
// match. Objective (no grader). Temperature 0 for determinism.
func (r *Runner) runMathBench(ctx context.Context, base, model string, p MathProblem) (ProblemResult, ProblemTranscript) {
	res := ProblemResult{ProblemID: p.ID, ProblemName: truncateQuestion(p.Question)}
	tr := ProblemTranscript{ProblemID: p.ID, ProblemName: res.ProblemName}

	reqCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()
	comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
		Model:       model,
		Temperature: 0,
		MaxTokens:   r.cfg.MaxTokens,
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a careful math solver. Reason step by step, then end with 'The answer is <number>'."},
			{Role: "user", Content: p.Question},
		},
	})
	if err != nil {
		res.Err = err.Error()
		tr.Error = err.Error()
		return res, tr
	}
	res.TTFTms = comp.TTFT.Milliseconds()
	res.TotalMs = comp.Total.Milliseconds()
	res.TokensPerSecond = comp.TokensPerSecond
	res.DecodeTPS = comp.TokensPerSecond
	res.PromptProcessingTPS = comp.PromptProcessingTPS
	res.PromptTokens = comp.PromptTokens
	res.CompletionTokens = comp.CompletionTokens
	tr.ModelResponse = comp.Content

	got := extractFinalAnswer(comp.Content)
	res.Resolved = got != "" && got == normalizeNumber(p.Answer)
	if res.Resolved {
		res.Score = 1
	}
	res.Difficulty = p.Difficulty
	res.Detail = fmt.Sprintf("expected %s, got %q (difficulty %d)", p.Answer, got, p.Difficulty)
	return res, tr
}

func truncateQuestion(q string) string {
	q = strings.ReplaceAll(q, "\n", " ")
	if len(q) > 60 {
		return q[:57] + "..."
	}
	return q
}

type mathHandler struct{}

func (mathHandler) Mode() Mode                      { return ModeMathBench }
func (mathHandler) Category() Category              { return CatQuality }
func (mathHandler) Count(r *Runner) int             { return len(r.mathProblems) }
func (mathHandler) Prepare(*Runner) (Scorer, error) { return nil, nil }

// Finalize sets MathAccuracy over answered problems only (request errors are
// excluded from the denominator, mirroring the generic quality rollup).
func (mathHandler) Finalize(agg *Aggregate, problems []ProblemResult) {
	answered, solved := 0, 0
	for _, p := range problems {
		if p.Err != "" {
			continue
		}
		answered++
		if p.Resolved {
			solved++
		}
	}
	if answered > 0 {
		agg.MathAccuracy = float64(solved) / float64(answered)
	}
}

func (mathHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	var results []ProblemResult
	var transcripts []ProblemTranscript
	for i, p := range r.mathProblems {
		select {
		case <-ctx.Done():
			return results, transcripts, ctx.Err()
		default:
		}
		send(progress, Progress{Index: i + 1, Total: len(r.mathProblems), ProblemID: p.ID, ProblemName: truncateQuestion(p.Question), Phase: "infer"})
		pr, tr := r.runMathBench(ctx, base, model, p)
		results = append(results, pr)
		if r.cfg.SaveTranscripts {
			transcripts = append(transcripts, tr)
		}
	}
	return results, transcripts, nil
}

func init() {
	registerHandler(mathHandler{})
}
