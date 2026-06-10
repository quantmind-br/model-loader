package benchmark

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed data/ragas_curated.json
var ragasDataset []byte

// RagasProblem is one synthetic RAG scenario: the model must answer Question
// using only Documents; the grader scores the answer against GroundTruth /
// ExpectedContext.
type RagasProblem struct {
	ID              string   `json:"id"`
	Documents       []string `json:"documents"`
	Question        string   `json:"question"`
	GroundTruth     string   `json:"groundTruth"`
	ExpectedContext string   `json:"expectedContext"`
}

// loadRagasProblems decodes the embedded synthetic RAG scenarios.
func loadRagasProblems() ([]RagasProblem, error) {
	var ps []RagasProblem
	if err := json.Unmarshal(ragasDataset, &ps); err != nil {
		return nil, fmt.Errorf("decode ragas dataset: %w", err)
	}
	if len(ps) == 0 {
		return nil, fmt.Errorf("ragas dataset is empty")
	}
	return ps, nil
}

const ragasPassThreshold = 0.7

// buildRagasPrompt renders the documents as a numbered context block followed
// by the question and a strict instruction to answer only from the documents.
func buildRagasPrompt(p RagasProblem) string {
	var b strings.Builder
	b.WriteString("Answer the question using ONLY the documents below. " +
		"If the documents do not contain the answer, say so. Do not add outside facts.\n\n")
	for i, d := range p.Documents {
		fmt.Fprintf(&b, "[Doc %d] %s\n", i+1, d)
	}
	fmt.Fprintf(&b, "\nQuestion: %s\n", p.Question)
	return b.String()
}

func ragasDetail(faith, rel, prec float64, judgedBy string) string {
	return fmt.Sprintf("faithfulness=%.2f relevancy=%.2f precision=%.2f (%s)", faith, rel, prec, judgedBy)
}

// runRagas answers one scenario and grades it on three criteria.
func (r *Runner) runRagas(ctx context.Context, base, model string, g grader, p RagasProblem) (ProblemResult, ProblemTranscript) {
	name := truncateQuestion(p.Question)
	res := ProblemResult{ProblemID: p.ID, ProblemName: name}
	tr := ProblemTranscript{ProblemID: p.ID, ProblemName: name}

	docs := strings.Join(p.Documents, "\n")
	reqCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
		Model:       model,
		Temperature: 0,
		MaxTokens:   r.cfg.MaxTokens,
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a careful retrieval-augmented assistant. Answer only from the provided documents."},
			{Role: "user", Content: buildRagasPrompt(p)},
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

	grade := func(criterion, guidance, gctx, question string) gradeResult {
		gCtx, gCancel := context.WithTimeout(ctx, r.cfg.Timeout)
		defer gCancel()
		gr, gErr := g.Grade(gCtx, gradeRequest{
			Criterion: criterion, Guidance: guidance,
			Question: question, Context: gctx, Answer: comp.Content,
		})
		if gErr != nil {
			tr.JudgeRaw = append(tr.JudgeRaw, criterion+": "+gErr.Error())
			return gradeResult{}
		}
		tr.JudgeRaw = append(tr.JudgeRaw, gr.Raw)
		return gr
	}

	faith := grade("faithfulness", "every claim is supported by the documents; no invented facts", docs, p.Question)
	rel := grade("answer relevancy", "the answer directly and completely addresses the question", "", p.Question)
	prec := grade("context precision", "the answer matches the ground-truth answer drawn from the relevant document",
		"Ground truth: "+p.GroundTruth+"\nRelevant document: "+p.ExpectedContext, p.Question)

	mean := (faith.Score + rel.Score + prec.Score) / 3
	res.Score = mean
	res.Resolved = mean >= ragasPassThreshold
	judgedBy := "self"
	for _, gr := range []gradeResult{faith, rel, prec} {
		if gr.JudgedBy != "" {
			judgedBy = gr.JudgedBy
			break
		}
	}
	res.SubScores = map[string]float64{
		"faithfulness": faith.Score,
		"relevancy":    rel.Score,
		"precision":    prec.Score,
	}
	res.JudgedBy = judgedBy
	res.Detail = ragasDetail(faith.Score, rel.Score, prec.Score, judgedBy)
	return res, tr
}

type ragasHandler struct{}

func (ragasHandler) Mode() Mode                      { return ModeRagasBench }
func (ragasHandler) Category() Category              { return CatQuality }
func (ragasHandler) Count(r *Runner) int             { return len(r.ragasProblems) }
func (ragasHandler) Prepare(*Runner) (Scorer, error) { return nil, nil }

// Finalize averages each grader criterion across all answered problems,
// reading the structured SubScores set by runRagas.
func (ragasHandler) Finalize(agg *Aggregate, problems []ProblemResult) {
	var sf, sr, sp float64
	n := 0
	for _, pr := range problems {
		if pr.SubScores == nil {
			continue
		}
		sf += pr.SubScores["faithfulness"]
		sr += pr.SubScores["relevancy"]
		sp += pr.SubScores["precision"]
		n++
	}
	if n == 0 {
		return
	}
	agg.RagasFaithfulness = sf / float64(n)
	agg.RagasRelevancy = sr / float64(n)
	agg.RagasPrecision = sp / float64(n)
}

func (ragasHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	g := r.graderFor(base, model)
	var results []ProblemResult
	var transcripts []ProblemTranscript
	for i, p := range r.ragasProblems {
		select {
		case <-ctx.Done():
			return results, transcripts, ctx.Err()
		default:
		}
		send(progress, Progress{Index: i + 1, Total: len(r.ragasProblems), ProblemID: p.ID, ProblemName: truncateQuestion(p.Question), Phase: "infer"})
		pr, tr := r.runRagas(ctx, base, model, g, p)
		results = append(results, pr)
		if r.cfg.SaveTranscripts {
			transcripts = append(transcripts, tr)
		}
		send(progress, Progress{Index: i + 1, Total: len(r.ragasProblems), ProblemID: p.ID, ProblemName: truncateQuestion(p.Question), Phase: "score"})
	}
	return results, transcripts, nil
}

func init() {
	registerHandler(ragasHandler{})
}
