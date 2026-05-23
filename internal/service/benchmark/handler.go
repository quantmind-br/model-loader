package benchmark

import "context"

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
