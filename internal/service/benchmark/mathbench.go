package benchmark

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

//go:embed data/gsm8k_curated.json
var mathDataset []byte

// MathProblem is one curated math-reasoning item.
type MathProblem struct {
	ID         string `json:"id"`
	Question   string `json:"question"`
	Answer     string `json:"answer"`     // normalized numeric string (e.g. "42")
	Difficulty int    `json:"difficulty"` // 1..3 by reasoning-step count
}

// loadMathProblems decodes the embedded GSM8K subset.
func loadMathProblems() ([]MathProblem, error) {
	var ps []MathProblem
	if err := json.Unmarshal(mathDataset, &ps); err != nil {
		return nil, fmt.Errorf("decode math dataset: %w", err)
	}
	if len(ps) == 0 {
		return nil, fmt.Errorf("math dataset is empty")
	}
	return ps, nil
}

var (
	thinkRe  = regexp.MustCompile(`(?is)<think>.*?</think>`)
	hashRe   = regexp.MustCompile(`####\s*(-?[\d,]+(?:\.\d+)?)`)
	answerRe = regexp.MustCompile(`(?i)(?:final answer|the answer is|answer:)\s*\$?(-?[\d,]+(?:\.\d+)?)`)
	numberRe = regexp.MustCompile(`-?[\d,]+(?:\.\d+)?`)
)

// normalizeNumber canonicalizes a numeric string: strip $, commas, surrounding
// space; drop a trailing ".0"; lowercase non-numeric text so it still compares.
func normalizeNumber(s string) string {
	s = strings.TrimSpace(s)
	cleaned := strings.NewReplacer("$", "", ",", "", " ", "").Replace(s)
	if f, err := strconv.ParseFloat(cleaned, 64); err == nil {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strings.ToLower(s)
}

// extractFinalAnswer pulls the model's final numeric answer from a response,
// stripping any <think> chain first and preferring explicit markers (#### or
// "the answer is") over the last bare number.
func extractFinalAnswer(response string) string {
	clean := thinkRe.ReplaceAllString(response, " ")
	if m := hashRe.FindStringSubmatch(clean); m != nil {
		return normalizeNumber(m[1])
	}
	if m := answerRe.FindStringSubmatch(clean); m != nil {
		return normalizeNumber(m[1])
	}
	nums := numberRe.FindAllString(clean, -1)
	if len(nums) > 0 {
		return normalizeNumber(nums[len(nums)-1])
	}
	return ""
}

// matchAnswer reports whether the model response yields the expected answer
// after normalization.
func matchAnswer(expected, response string) bool {
	got := extractFinalAnswer(response)
	want := normalizeNumber(expected)
	return got != "" && got == want
}
