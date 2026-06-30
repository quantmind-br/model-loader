package benchmark

import "fmt"

// FormatBenchProgress renders a running-benchmark status line. When total is
// unknown (0), agentic harness modes omit the denominator so the UI does not
// show misleading values like "19/0".
func FormatBenchProgress(index, total int, problemName, phase string) string {
	if total > 0 {
		return fmt.Sprintf("problem %d/%d: %s (%s)", index, total, problemName, phase)
	}
	return fmt.Sprintf("problem %d: %s (%s)", index, problemName, phase)
}