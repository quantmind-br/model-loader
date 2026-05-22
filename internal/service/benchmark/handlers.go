package benchmark

import "context"

func init() {
	registerHandler(judgeHandler{})
	registerHandler(longContextHandler{})
	registerHandler(llamaBenchHandler{})
}

// --- judge (SWE-bench Lite) ---

type judgeHandler struct{}

func (judgeHandler) Mode() Mode                           { return ModeJudge }
func (judgeHandler) Category() Category                   { return CatQuality }
func (judgeHandler) Count(r *Runner) int                  { return len(r.problems) }
func (judgeHandler) Prepare(r *Runner) (Scorer, error)    { return r.newScorer(ModeJudge) }
func (judgeHandler) Finalize(*Aggregate, []ProblemResult) {}

func (judgeHandler) Execute(ctx context.Context, r *Runner, base, model string, scorer Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	var results []ProblemResult
	var transcripts []ProblemTranscript
	for i, p := range r.problems {
		select {
		case <-ctx.Done():
			return results, transcripts, ctx.Err()
		default:
		}
		send(progress, Progress{Index: i + 1, Total: len(r.problems), ProblemID: p.ID, ProblemName: p.Name, Phase: "infer"})
		pr, tr := r.runProblem(ctx, scorer, base, model, p)
		results = append(results, pr)
		if r.cfg.SaveTranscripts {
			transcripts = append(transcripts, tr)
		}
		send(progress, Progress{Index: i + 1, Total: len(r.problems), ProblemID: p.ID, ProblemName: p.Name, Phase: "score"})
	}
	return results, transcripts, nil
}

// --- long-context needle ---

type longContextHandler struct{}

func (longContextHandler) Mode() Mode                           { return ModeLongContext }
func (longContextHandler) Category() Category                   { return CatRobustness }
func (longContextHandler) Count(*Runner) int                    { return 1 }
func (longContextHandler) Prepare(*Runner) (Scorer, error)      { return nil, nil }
func (longContextHandler) Finalize(*Aggregate, []ProblemResult) {}

func (longContextHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	send(progress, Progress{Index: 1, Total: 1, ProblemID: "long-context-needle", ProblemName: "Long-context needle retrieval", Phase: "infer"})
	pr, tr := r.runLongContext(ctx, base, model)
	var transcripts []ProblemTranscript
	if r.cfg.SaveTranscripts {
		transcripts = append(transcripts, tr)
	}
	return []ProblemResult{pr}, transcripts, nil
}

// --- llama-bench throughput ---

type llamaBenchHandler struct{}

func (llamaBenchHandler) Mode() Mode                           { return ModeLlamaBench }
func (llamaBenchHandler) Category() Category                   { return CatSpeed }
func (llamaBenchHandler) Count(r *Runner) int                  { return len(r.presets) }
func (llamaBenchHandler) Prepare(*Runner) (Scorer, error)      { return nil, nil }
func (llamaBenchHandler) Finalize(*Aggregate, []ProblemResult) {}

func (llamaBenchHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	var results []ProblemResult
	var transcripts []ProblemTranscript
	for i, ps := range r.presets {
		select {
		case <-ctx.Done():
			return results, transcripts, ctx.Err()
		default:
		}
		send(progress, Progress{Index: i + 1, Total: len(r.presets), ProblemID: ps.id(), ProblemName: ps.name(), Phase: "infer"})
		pr, tr := r.runLlamaBench(ctx, base, model, ps)
		results = append(results, pr)
		if r.cfg.SaveTranscripts {
			transcripts = append(transcripts, tr)
		}
	}
	return results, transcripts, nil
}
