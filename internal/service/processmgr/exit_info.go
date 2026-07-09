package processmgr

import (
	"bytes"
	"io"
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

// stderrTailMaxBytes caps how much of the log tail is read into memory. vLLM/
// SGLang logs reach tens of MiB; reading the whole file per exit (once per
// generation in a crash loop) is wasteful, so only the last 64 KiB is read
// (audit C5).
const stderrTailMaxBytes = 64 << 10

// readStderrTail reads up to maxLines trailing lines of the log file (each
// preserved verbatim, including empty intermediate lines — the LauncherPage's
// enrichWithExit picks the last non-empty one). Only the final
// stderrTailMaxBytes are read; when the file is larger, the leading partial
// line is dropped so the first returned line is whole.
// Returns nil on any error — the enrichment is best-effort and must never
// block or panic the Wait goroutine.
//
// This runs OUTSIDE the manager's mutex so file I/O does not serialize
// Launch/Kill against a slow log volume.
func readStderrTail(logPath string, maxLines int) []string {
	if logPath == "" || maxLines <= 0 {
		return nil
	}
	f, err := os.Open(logPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	size := fi.Size()
	var data []byte
	if size > stderrTailMaxBytes {
		buf := make([]byte, stderrTailMaxBytes)
		if _, err := f.ReadAt(buf, size-stderrTailMaxBytes); err != nil && err != io.EOF {
			return nil
		}
		// Drop the leading partial line so the first returned line is whole.
		if idx := bytes.IndexByte(buf, '\n'); idx >= 0 {
			buf = buf[idx+1:]
		}
		data = buf
	} else {
		buf := make([]byte, size)
		if _, err := f.ReadAt(buf, 0); err != nil && err != io.EOF {
			return nil
		}
		data = buf
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
