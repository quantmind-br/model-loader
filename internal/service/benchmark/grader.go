package benchmark

import "context"

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
