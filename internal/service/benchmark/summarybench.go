package benchmark

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

//go:embed data/summary_curated.json
var summaryDataset []byte

// SummaryProblem is one multi-document summarization bundle: Documents are
// summarized; Facts are the key points a faithful summary must cover.
type SummaryProblem struct {
	ID        string   `json:"id"`
	Documents []string `json:"documents"`
	Facts     []string `json:"facts"`
}

func loadSummaryProblems() ([]SummaryProblem, error) {
	var ps []SummaryProblem
	if err := json.Unmarshal(summaryDataset, &ps); err != nil {
		return nil, fmt.Errorf("decode summary dataset: %w", err)
	}
	if len(ps) == 0 {
		return nil, fmt.Errorf("summary dataset is empty")
	}
	return ps, nil
}

const summaryPassThreshold = 0.7

// factWordRe extracts significant words (letters >=5 chars) from a fact.
var factWordRe = regexp.MustCompile(`[a-z]{5,}`)

// factNumRe extracts multi-digit numeric tokens (2+ digits) from a fact.
var factNumRe = regexp.MustCompile(`[0-9]{2,}`)

// factCoverage returns the fraction of facts whose salient terms mostly appear
// in the summary. Key terms are: content words of 5+ letters and multi-digit
// numeric tokens (year, count, quantity). A fact counts as covered when >= 60%
// of its salient terms are present (case-insensitive), tolerating paraphrase
// while requiring the concrete entities/numbers.
func factCoverage(summary string, facts []string) float64 {
	if len(facts) == 0 {
		return 0
	}
	low := strings.ToLower(summary)
	covered := 0
	for _, f := range facts {
		lf := strings.ToLower(f)
		words := factWordRe.FindAllString(lf, -1)
		nums := factNumRe.FindAllString(lf, -1)
		terms := append(words, nums...)
		if len(terms) == 0 {
			continue
		}
		hit := 0
		for _, t := range terms {
			if strings.Contains(low, t) {
				hit++
			}
		}
		if float64(hit)/float64(len(terms)) >= 0.6 {
			covered++
		}
	}
	return float64(covered) / float64(len(facts))
}

// buildSummaryPrompt renders the documents as a numbered block with a faithful-summary instruction.
func buildSummaryPrompt(p SummaryProblem) string {
	var b strings.Builder
	b.WriteString("Summarize the following documents in 3-5 sentences. " +
		"Cover the most important facts accurately; do not invent anything.\n\n")
	for i, d := range p.Documents {
		fmt.Fprintf(&b, "[Doc %d] %s\n", i+1, d)
	}
	return b.String()
}

func summaryDetail(coveredFacts, totalFacts int, coherence float64, judgedBy string) string {
	return fmt.Sprintf("coverage=%d/%d coherence=%.2f (%s)", coveredFacts, totalFacts, coherence, judgedBy)
}

// runSummary asks the model to summarize the bundle, then scores fact coverage (local) and coherence (grader).
func (r *Runner) runSummary(ctx context.Context, base, model string, g grader, p SummaryProblem) (ProblemResult, ProblemTranscript) {
	name := "summary " + p.ID
	res := ProblemResult{ProblemID: p.ID, ProblemName: name}
	tr := ProblemTranscript{ProblemID: p.ID, ProblemName: name}

	reqCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
		Model:       model,
		Temperature: 0,
		MaxTokens:   r.cfg.MaxTokens,
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a precise summarization assistant."},
			{Role: "user", Content: buildSummaryPrompt(p)},
		},
	})
	cancel()
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

	coverage := factCoverage(comp.Content, p.Facts)
	covered := int(coverage*float64(len(p.Facts)) + 0.5)

	gCtx, gCancel := context.WithTimeout(ctx, r.cfg.Timeout)
	gr, gErr := g.Grade(gCtx, gradeRequest{
		Criterion: "coherence",
		Guidance:  "the summary reads as a coherent whole and faithfully reflects the documents without contradictions or invented facts",
		Context:   strings.Join(p.Documents, "\n"),
		Answer:    comp.Content,
	})
	gCancel()
	coherence := 0.0
	judgedBy := "self"
	if gErr != nil {
		tr.JudgeRaw = append(tr.JudgeRaw, "coherence: "+gErr.Error())
	} else {
		coherence = gr.Score
		if gr.JudgedBy != "" {
			judgedBy = gr.JudgedBy
		}
		tr.JudgeRaw = append(tr.JudgeRaw, gr.Raw)
	}

	res.Score = (coverage + coherence) / 2
	res.Resolved = res.Score >= summaryPassThreshold
	res.SubScores = map[string]float64{"coverage": coverage, "coherence": coherence}
	res.JudgedBy = judgedBy
	res.Detail = summaryDetail(covered, len(p.Facts), coherence, judgedBy)
	return res, tr
}

type summaryHandler struct{}

func (summaryHandler) Mode() Mode                      { return ModeSummaryBench }
func (summaryHandler) Category() Category              { return CatQuality }
func (summaryHandler) Count(r *Runner) int             { return len(r.summaryProblems) }
func (summaryHandler) Prepare(*Runner) (Scorer, error) { return nil, nil }

// Finalize sets SummaryCoherence to the mean grader coherence sub-score across
// answered problems, reading the structured SubScores set by runSummary. (Fact
// coverage rides in SubScores too; the blended per-problem Score feeds the
// generic SolveRate/AvgScore.)
func (summaryHandler) Finalize(agg *Aggregate, problems []ProblemResult) {
	var sum float64
	n := 0
	for _, pr := range problems {
		if pr.SubScores == nil {
			continue
		}
		sum += pr.SubScores["coherence"]
		n++
	}
	if n == 0 {
		return
	}
	agg.SummaryCoherence = sum / float64(n)
}

func (summaryHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	g := r.graderFor(base, model)
	total := len(r.summaryProblems)
	return executeSerialBench(ctx, r, progress, total,
		func(i int) (string, string) {
			p := r.summaryProblems[i]
			return p.ID, "summary " + p.ID
		},
		func(i int) (ProblemResult, ProblemTranscript) {
			p := r.summaryProblems[i]
			pr, tr := r.runSummary(ctx, base, model, g, p)
			// Coherence grading happens inside runSummary; emit the "score" phase.
			send(progress, Progress{Index: i + 1, Total: total, ProblemID: p.ID, ProblemName: "summary " + p.ID, Phase: "score"})
			return pr, tr
		})
}

func init() {
	registerHandler(summaryHandler{})
}
