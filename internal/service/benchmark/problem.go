// Package benchmark evaluates inference profiles against a curated, embedded
// set of real SWE-bench Lite coding problems. It launches the profile's
// backend, sends single-turn prompt→patch requests, and grades the produced
// diff with a reference-guided LLM judge (the only scoring mode), aggregating
// quality, performance, resource and token-cost metrics per run. A separate
// long-context needle probe diagnoses KV-cache-quantization decay.
package benchmark

// Problem is one curated benchmark item drawn from SWE-bench Lite. The same
// fixed set is embedded in the binary and used identically for every profile
// so runs stay reproducible and comparable.
type Problem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Language string `json:"language"`

	// RepoName is the source repository identifier (e.g. "sympy/sympy") shown
	// to the model in the closed-book prompt.
	RepoName string `json:"repoName,omitempty"`

	// Statement is the issue/bug report shown to the model.
	Statement string `json:"statement"`

	// ContextFiles is the SWE-bench "oracle" context: the full source of the
	// file(s) the gold patch touches (path → raw content), shown to the model
	// so localization is a real (open-book) task, not memorization.
	ContextFiles map[string]string `json:"contextFiles"`

	// GoldenPatch is the reference fix. It anchors the LLM judge (reference-
	// guided judging): the judge compares the candidate against it for
	// functional equivalence rather than syntactic identity.
	GoldenPatch string `json:"goldenPatch"`
}
