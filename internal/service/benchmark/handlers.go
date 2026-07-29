package benchmark

import (
	"context"
	"sync"
)

func init() {
	registerHandler(judgeHandler{})
	registerHandler(longContextHandler{})
	registerHandler(llamaBenchHandler{})
}

// --- judge (SWE-bench Lite) ---

type judgeHandler struct{}

func (judgeHandler) Mode() Mode                           { return ModeJudge }
func (judgeHandler) Category() Category                   { return CatQuality }
func (judgeHandler) Count(r *Runner) int                  { return r.capCount(len(r.problems)) }
func (judgeHandler) Prepare(r *Runner) (Scorer, error)    { return r.newScorer(ModeJudge) }
func (judgeHandler) Finalize(*Aggregate, []ProblemResult) {}

// judgeScoreConcurrency caps in-flight judge calls so a slow judge endpoint
// doesn't pile up requests while inference keeps producing answers.
const judgeScoreConcurrency = 2

// Execute pipelines the judge run: inference stays strictly serial against
// the server under test (so speed metrics aren't polluted by concurrent
// load), while scoring — which talks to a separate judge endpoint — runs in
// background goroutines overlapped with the next problem's inference.
// Results are written by index, so ordering matches the dataset. A checkpoint
// (T7/BM3) is emitted from this serial loop after each item so a crash mid-run
// keeps what completed; mu makes the snapshot copy race-free against the
// in-flight scoring goroutines, and confining r.checkpoint to the loop keeps
// its throttle state single-writer.
func (judgeHandler) Execute(ctx context.Context, r *Runner, base, model string, scorer Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	total := r.capCount(len(r.problems))
	// Capacity MUST cover every append: scoring goroutines hold &results[idx],
	// so the backing array can never reallocate mid-run.
	results := make([]ProblemResult, 0, total)
	trs := make([]ProblemTranscript, 0, total)

	// mu guards the result elements shared with the scoring goroutines: their
	// in-place writes (scoreProblem) and the checkpoint snapshot copy.
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, judgeScoreConcurrency)
	// All scoring goroutines must finish before Execute returns: the caller
	// closes the progress channel right after Run returns, and the result
	// slices must be fully written.
	defer wg.Wait()

	// checkpoint persists the partial run so far (T7). Called only from this
	// serial loop, so r.checkpoint's throttle state stays single-writer; mu
	// makes the element copy race-free against the in-flight scoring goroutines.
	// In-flight (not-yet-scored) items appear with their pre-score state and are
	// finalized by a later checkpoint or the caller's final save.
	checkpoint := func() {
		if r.checkpoint == nil {
			return
		}
		mu.Lock()
		snap := append([]ProblemResult(nil), results...)
		mu.Unlock()
		r.checkpoint(snap)
	}

	done := false
	for i, p := range r.problems[:total] {
		select {
		case <-ctx.Done():
			done = true
		default:
		}
		if done {
			return results, transcripts(r, trs), ctx.Err()
		}
		send(progress, Progress{Index: i + 1, Total: total, ProblemID: p.ID, ProblemName: p.Name, Phase: "infer"})
		comp, pr, tr, scoreIt := r.inferProblem(ctx, base, model, p)
		results = append(results, pr)
		trs = append(trs, tr)
		if !scoreIt {
			send(progress, Progress{Index: i + 1, Total: total, ProblemID: p.ID, ProblemName: p.Name,
				Phase: "item_done", Outcome: outcomeOf(pr), Score: pr.Score, ItemMs: pr.TotalMs, Detail: pr.Err})
			checkpoint()
			continue
		}
		send(progress, Progress{Index: i + 1, Total: total, ProblemID: p.ID, ProblemName: p.Name, Phase: "score"})
		// Resolve the slot pointers BEFORE spawning: the goroutine must not read
		// the results/trs slice variables, which the loop keeps reassigning.
		resPtr, trPtr := &results[len(results)-1], &trs[len(trs)-1]
		wg.Add(1)
		go func(idx int, p Problem, content string) {
			defer wg.Done()
			// A cancelled run must unblock a goroutine waiting for a semaphore slot
			// instead of stranding it behind a slow judge that will never drain.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			// Distinct slots: each goroutine writes only its own problem, under mu.
			r.scoreProblem(ctx, scorer, p, content, resPtr, trPtr, &mu)
			// Emit the finish event from the scoring goroutine: the slot's final
			// outcome is only known after scoreProblem returns. It may arrive after
			// a later item's infer event (overlapped scoring) — RunFeed matches by
			// id and uses ItemMs, so out-of-order finishes fold correctly.
			mu.Lock()
			fin := *resPtr
			mu.Unlock()
			send(progress, Progress{Index: idx + 1, Total: total, ProblemID: p.ID, ProblemName: p.Name,
				Phase: "item_done", Outcome: outcomeOf(fin), Score: fin.Score, ItemMs: fin.TotalMs, Detail: fin.Err})
		}(i, p, comp.Content)
		checkpoint()
	}
	wg.Wait()
	return results, transcripts(r, trs), nil
}

// transcripts returns trs when transcript saving is enabled, else nil.
func transcripts(r *Runner, trs []ProblemTranscript) []ProblemTranscript {
	if r.cfg.SaveTranscripts {
		return trs
	}
	return nil
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
func (llamaBenchHandler) Count(r *Runner) int                  { return r.capCount(len(r.presets)) }
func (llamaBenchHandler) Prepare(*Runner) (Scorer, error)      { return nil, nil }
func (llamaBenchHandler) Finalize(*Aggregate, []ProblemResult) {}

func (llamaBenchHandler) Execute(ctx context.Context, r *Runner, base, model string, _ Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error) {
	var results []ProblemResult
	var transcripts []ProblemTranscript
	total := r.capCount(len(r.presets))
	for i, ps := range r.presets[:total] {
		select {
		case <-ctx.Done():
			return results, transcripts, ctx.Err()
		default:
		}
		send(progress, Progress{Index: i + 1, Total: total, ProblemID: ps.id(), ProblemName: ps.name(), Phase: "infer"})
		pr, tr := r.runLlamaBench(ctx, base, model, ps)
		results = append(results, pr)
		if r.cfg.SaveTranscripts {
			transcripts = append(transcripts, tr)
		}
	}
	return results, transcripts, nil
}
