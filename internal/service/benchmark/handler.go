package benchmark

import "context"

// executeSerialBench drives the shared serial-benchmark loop used by every
// non-judge mode: for each of total items it checks cancellation, streams an
// "infer" Progress (id/name from meta), runs runOne, then appends the result
// and — when transcript saving is enabled — its transcript. On cancellation it
// returns the partial results formed so far with ctx.Err(), preserving the
// per-mode partial-run semantics. Modes with a trailing "score" phase send it
// from inside their runOne closure.
func executeSerialBench(
	ctx context.Context, r *Runner, progress chan<- Progress,
	total int, meta func(int) (id, name string),
	runOne func(int) (ProblemResult, ProblemTranscript),
) ([]ProblemResult, []ProblemTranscript, error) {
	var results []ProblemResult
	var transcripts []ProblemTranscript
	for i := range total {
		select {
		case <-ctx.Done():
			return results, transcripts, ctx.Err()
		default:
		}
		id, name := meta(i)
		send(progress, Progress{Index: i + 1, Total: total, ProblemID: id, ProblemName: name, Phase: "infer"})
		pr, tr := runOne(i)
		results = append(results, pr)
		if r.cfg.SaveTranscripts {
			transcripts = append(transcripts, tr)
		}
	}
	return results, transcripts, nil
}

// Category groups modes for the UI picker.
type Category string

const (
	CatQuality    Category = "Quality"
	CatSpeed      Category = "Speed"
	CatRobustness Category = "Robustness"
	CatKnowledge  Category = "Knowledge"
)

// modeHandler encapsulates one benchmark mode: its identity, how many problems
// it runs, fail-fast preparation (config validation / scorer build, done before
// the expensive backend launch), execution, and any mode-specific aggregate
// finalization.
type modeHandler interface {
	Mode() Mode
	Category() Category
	Count(r *Runner) int
	// Prepare validates config and builds the scorer BEFORE the backend is
	// launched. Returns a nil Scorer for objective (non-judged) modes.
	Prepare(r *Runner) (Scorer, error)
	// Execute runs every problem/preset and returns per-problem results and
	// optional transcripts. It is responsible for streaming "infer"/"score"
	// progress; Run sends "launch" and "done".
	Execute(ctx context.Context, r *Runner, base, model string, scorer Scorer, progress chan<- Progress) ([]ProblemResult, []ProblemTranscript, error)
	// Finalize sets mode-specific Aggregate fields after the generic rollup.
	Finalize(agg *Aggregate, problems []ProblemResult)
}

var handlers = map[Mode]modeHandler{}

// registerHandler adds a handler to the registry; called from init().
func registerHandler(h modeHandler) { handlers[h.Mode()] = h }

// handlerFor returns the handler for a mode, if registered.
func handlerFor(m Mode) (modeHandler, bool) {
	h, ok := handlers[m]
	return h, ok
}

// CategoryOf reports the UI category for a registered mode. The bool is false
// for an unregistered mode. Used by the picker to group modes.
func CategoryOf(m Mode) (Category, bool) {
	h, ok := handlerFor(m)
	if !ok {
		return "", false
	}
	return h.Category(), true
}

// modeOrder is the canonical display order for pickers and grouped views:
// modes of the same category are contiguous, categories follow
// Quality → Speed → Robustness → Knowledge. Add new modes here when
// registering a new handler.
var modeOrder = []Mode{
	// Quality
	ModeJudge, ModeMathBench, ModeCodeGenBench, ModeRagasBench, ModeSummaryBench,
	// Speed
	ModeLlamaBench,
	// Robustness
	ModeLongContext, ModeInstBench,
	// Knowledge
	ModeMMLUBench,
}

// ModesInOrder returns the registered modes in canonical display order. It is
// the single source of truth for UI mode lists.
func ModesInOrder() []Mode {
	out := make([]Mode, 0, len(modeOrder))
	for _, m := range modeOrder {
		if _, ok := handlerFor(m); ok {
			out = append(out, m)
		}
	}
	return out
}
