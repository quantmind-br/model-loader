package benchmark

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

//go:embed data/mmlu_curated.json
var mmluDataset []byte

// loadMMLUProblems decodes the embedded curated MMLU subset.
func loadMMLUProblems() ([]MMLUProblem, error) {
	var ps []MMLUProblem
	if err := json.Unmarshal(mmluDataset, &ps); err != nil {
		return nil, fmt.Errorf("decode mmlu dataset: %w", err)
	}
	if len(ps) == 0 {
		return nil, fmt.Errorf("mmlu dataset is empty")
	}
	return ps, nil
}

// MMLUProblem is one curated MMLU multiple-choice item. Answer is the correct
// option letter ("A".."D"); Category is the super-category (STEM, Humanities,
// Social Sciences, Other) used for the per-category breakdown.
type MMLUProblem struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Choices  []string `json:"choices"`
	Answer   string   `json:"answer"`
	Category string   `json:"category"`
}

var (
	// mcAnswerRe prefers an explicit marker: "answer is B", "answer: B",
	// "option (C)", "choice = D". Case-insensitive.
	mcAnswerRe = regexp.MustCompile(`(?i)\b(?:answer|option|choice)\b\s*(?:is|:|=)?\s*\(?([A-Da-d])\)?\b`)
	// mcLetterRe is the fallback: the first standalone CAPITAL A-D. Uppercase
	// only, so the English article "a" and stray lowercase letters don't match.
	mcLetterRe = regexp.MustCompile(`\b([A-D])\b`)
)

// extractMCLetter pulls the model's chosen option letter ("A".."D") from a
// reply, stripping any <think> chain first. It prefers an explicit answer
// marker, then a single-letter reply, then the first standalone capital letter.
// Returns "" when no option letter can be found.
func extractMCLetter(response string) string {
	clean := strings.TrimSpace(thinkRe.ReplaceAllString(response, " "))
	if len(clean) == 1 {
		c := strings.ToUpper(clean)
		if c >= "A" && c <= "D" {
			return c
		}
	}
	if m := mcAnswerRe.FindStringSubmatch(clean); m != nil {
		return strings.ToUpper(m[1])
	}
	if m := mcLetterRe.FindStringSubmatch(clean); m != nil {
		return m[1]
	}
	return ""
}

// buildMMLUPrompt renders the question with lettered options, one per line.
func buildMMLUPrompt(p MMLUProblem) string {
	var b strings.Builder
	b.WriteString(p.Question)
	b.WriteString("\n\n")
	for i, c := range p.Choices {
		if i >= 4 {
			break
		}
		fmt.Fprintf(&b, "%c) %s\n", 'A'+i, c)
	}
	return b.String()
}

// runMMLUBench asks the model one multiple-choice question and scores an
// objective letter exact-match. Runs at the profile's launched sampling.
func (r *Runner) runMMLUBench(ctx context.Context, base, model string, p MMLUProblem) (ProblemResult, ProblemTranscript) {
	name := "[" + p.Category + "] " + truncateQuestion(p.Question)
	res := ProblemResult{ProblemID: p.ID, ProblemName: name}
	tr := ProblemTranscript{ProblemID: p.ID, ProblemName: name}

	reqCtx, cancel := context.WithTimeout(ctx, r.inferTimeout())
	defer cancel()
	comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
		Model:     model,
		MaxTokens: r.cfg.MaxTokens,
		OnDelta:   r.streamHeartbeat(res.ProblemID, res.ProblemName),
		Messages: []ChatMessage{
			{Role: "system", Content: "You are answering a multiple-choice question. Respond with ONLY the letter (A, B, C, or D) of the correct answer."},
			{Role: "user", Content: buildMMLUPrompt(p)},
		},
	})
	if err != nil {
		res.Err = err.Error()
		res.FailPhase = phaseInfer
		tr.Error = err.Error()
		return res, tr
	}
	res.TTFTms = comp.TTFT.Milliseconds()
	res.TotalMs = comp.Total.Milliseconds()
	res.TokensPerSecond = comp.TokensPerSecond
	res.DecodeTPS = comp.TokensPerSecond
	res.PromptProcessingTPS = comp.PromptProcessingTPS
	res.ServerTimings = comp.ServerTimings
	res.PromptTokens = comp.PromptTokens
	res.CompletionTokens = comp.CompletionTokens
	tr.ModelResponse = comp.Content

	got := extractMCLetter(comp.Content)
	res.Resolved = got != "" && got == p.Answer
	if res.Resolved {
		res.Score = 1
	}
	res.Category = p.Category
	res.Detail = fmt.Sprintf("category=%s expected %s got %q", p.Category, p.Answer, got)
	return res, tr
}

type mmluHandler struct{}

func (mmluHandler) Mode() Mode                      { return ModeMMLUBench }
func (mmluHandler) Category() Category              { return CatKnowledge }
func (mmluHandler) Count(r *Runner) int             { return r.capCount(len(r.mmluProblems)) }
func (mmluHandler) Prepare(*Runner) (Scorer, error) { return nil, nil }

// Finalize sets MMLUAccuracy over answered problems only (request errors are
// excluded from the denominator). Per-category accuracy is derivable from each
// ProblemResult.Category, so no extra aggregate field is needed.
func (mmluHandler) Finalize(agg *Aggregate, problems []ProblemResult) {
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
		agg.MMLUAccuracy = float64(solved) / float64(answered)
	}
}

func (mmluHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	return executeSerialBench(ctx, r, progress, r.capCount(len(r.mmluProblems)),
		func(i int) (string, string) {
			p := r.mmluProblems[i]
			return p.ID, p.Category
		},
		func(i int) (ProblemResult, ProblemTranscript) {
			return r.runMMLUBench(ctx, base, model, r.mmluProblems[i])
		})
}

func init() {
	registerHandler(mmluHandler{})
}
