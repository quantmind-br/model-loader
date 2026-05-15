package processmgr

import (
	"bytes"
	"os"
)

// ExitInfo is the per-PID record populated by the cmd.Wait enrichment
// goroutine and read by LauncherPage.handleLaunchErr via
// Manager.GetExitInfo(pid). It is an ephemeral in-memory mirror of the four
// new RunningInstance fields — kept separate so the Manager interface does
// not leak the full RunningInstance shape to UI consumers.
type ExitInfo struct {
	ExitCode   *int
	ExitSignal string
	ExitReason string
	StderrTail []string
}

// stderrTailLines is the number of trailing log lines captured per exit.
// Matches the FRD requirement of 50.
const stderrTailLines = 50

// readStderrTail reads the on-disk log file and returns up to maxLines of
// trailing lines (each preserved verbatim, including empty intermediate
// lines — the LauncherPage's enrichWithExit picks the last non-empty one).
// Returns nil on any error — the enrichment is best-effort and must never
// block or panic the Wait goroutine.
//
// This runs OUTSIDE the manager's mutex so file I/O does not serialize
// Launch/Kill against a slow log volume.
func readStderrTail(logPath string, maxLines int) []string {
	if logPath == "" || maxLines <= 0 {
		return nil
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	data = bytes.TrimRight(data, "\n")
	if len(data) == 0 {
		return nil
	}
	lines := bytes.Split(data, []byte{'\n'})
	start := 0
	if len(lines) > maxLines {
		start = len(lines) - maxLines
	}
	out := make([]string, 0, len(lines)-start)
	for _, ln := range lines[start:] {
		out = append(out, string(ln))
	}
	return out
}
