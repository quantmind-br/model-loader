package benchmark

import "strings"

// truncateQuestion flattens a problem question/prompt onto one line and caps it
// at 60 characters (ellipsized) so it fits a progress label or result name.
// Shared by the math, mmlu, instruction and ragas benches.
func truncateQuestion(q string) string {
	q = strings.ReplaceAll(q, "\n", " ")
	if len(q) > 60 {
		return q[:57] + "..."
	}
	return q
}
