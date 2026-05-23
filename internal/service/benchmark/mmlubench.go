package benchmark

import (
	"fmt"
	"regexp"
	"strings"
)

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
