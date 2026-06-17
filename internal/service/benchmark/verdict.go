package benchmark

import (
	"encoding/json"
	"strings"
)

// verdict is the shared shape of a judge/grader JSON reply: a 0..1 score, a
// boolean outcome (the judge calls it "resolved", the grader "pass"), and a
// short rationale. A given reply carries only one of the two booleans; the
// other stays false and is ignored by the mode-specific caller.
type verdict struct {
	Score     float64
	Resolved  bool
	Pass      bool
	Rationale string
}

// parseVerdict extracts the JSON verdict object from raw model output —
// tolerating surrounding prose or code fences by slicing from the first '{' to
// the last '}' — decodes it, and clamps the score to [0,1]. It is the single
// place JSON extraction + clamping happens for both the judge and the grader.
func parseVerdict(raw string) (verdict, error) {
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			raw = raw[start : end+1]
		}
	}
	var v struct {
		Score     float64 `json:"score"`
		Resolved  bool    `json:"resolved"`
		Pass      bool    `json:"pass"`
		Rationale string  `json:"rationale"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return verdict{}, err
	}
	if v.Score < 0 {
		v.Score = 0
	}
	if v.Score > 1 {
		v.Score = 1
	}
	return verdict{Score: v.Score, Resolved: v.Resolved, Pass: v.Pass, Rationale: v.Rationale}, nil
}
