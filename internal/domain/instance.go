package domain

import "time"

// RunningInstance describes a live llama-server process tracked by ProcessManager.
type RunningInstance struct {
	ProfileID  string `json:"profileId"`
	PID        int    `json:"pid"`
	Port       int    `json:"port"`
	LogPath    string `json:"logPath"`
	BinaryPath string `json:"binaryPath,omitempty"`
	// Kind is the backend kind, stamped at launch. It is NOT reconstructed on
	// reconcile/recovery, so a recovered-but-not-relaunched instance may carry
	// an empty Kind. The proxy re-derives readiness/auth by relaunching
	// (WaitReady), so this field is for launch-time use only.
	Kind      BackendKind `json:"kind,omitempty"`
	StartedAt time.Time   `json:"startedAt"`
	// StartTicks is /proc/<pid>/stat field 22 captured at launch. Combined
	// with PID it detects PID recycling (audit A1/A11). 0 = unknown (legacy
	// entry or /proc read failure) — identity checks then degrade to Alive.
	StartTicks     uint64     `json:"startTicks,omitempty"`
	Background     bool       `json:"background"`
	Crashed        bool       `json:"crashed,omitempty"`
	ExitedAt       *time.Time `json:"exitedAt,omitempty"`
	RestartCount   int        `json:"restartCount,omitempty"`
	LastRestartAt  *time.Time `json:"lastRestartAt,omitempty"`
	RestartPolicy  string     `json:"restartPolicy,omitempty"`
	MaxRestarts    int        `json:"maxRestarts,omitempty"`
	BackoffSeconds int        `json:"backoffSeconds,omitempty"`
	// ExitCode is the process exit status when known. Set by processmgr's
	// cmd.Wait goroutine; nil for processes that are still alive or whose
	// exit was signal-only.
	ExitCode *int `json:"exitCode,omitempty"`
	// ExitSignal is the signal name (e.g. "SIGSEGV") when the process was
	// killed by a signal; "" otherwise.
	ExitSignal string `json:"exitSignal,omitempty"`
	// ExitReason is a human-readable cause: "exit:1", "signal:SIGTERM", or
	// "unknown" when cmd.Wait returned a non-ExitError.
	ExitReason string `json:"exitReason,omitempty"`
	// StderrTail is the last 50 lines of the process' log file at exit,
	// captured by the Wait goroutine for friendlyLaunchError enrichment.
	StderrTail []string `json:"stderrTail,omitempty"`
}

// ExitedInstance describes a process that has exited and been recorded in
// the persistent exit-history file.
type ExitedInstance struct {
	ProfileID       string    `json:"profileId"`
	PID             int       `json:"pid"`
	Port            int       `json:"port"`
	LogPath         string    `json:"logPath,omitempty"`
	BinaryPath      string    `json:"binaryPath,omitempty"`
	StartedAt       time.Time `json:"startedAt"`
	ExitedAt        time.Time `json:"exitedAt"`
	DurationSeconds int64     `json:"durationSeconds"`
	Background      bool      `json:"background"`
	Crashed         bool      `json:"crashed,omitempty"`
	ExitCode        *int      `json:"exitCode,omitempty"`
	ExitSignal      string    `json:"exitSignal,omitempty"`
	ExitReason      string    `json:"exitReason,omitempty"`
	StderrTail      []string  `json:"stderrTail,omitempty"`
}

// ExitReasonOperatorStop is written to ExitReason when a backend was terminated
// deliberately (processmgr.Kill), so exit classification can distinguish it from
// a crash without changing Crashed's liveness meaning.
const ExitReasonOperatorStop = "operator-stop"

// ExitClass is the operator-facing label for a terminated instance. Crashed
// keeps its existing meaning ("no longer live") and is not consulted here.
//
// Precedence:
//
//	ExitReason == ExitReasonOperatorStop -> "stopped"
//	ExitSignal != ""                     -> "crashed"
//	ExitCode != nil && *ExitCode != 0    -> "crashed"
//	ExitCode != nil && *ExitCode == 0    -> "exited"
//	otherwise                            -> "crashed"
func ExitClass(ri RunningInstance) string {
	switch {
	case ri.ExitReason == ExitReasonOperatorStop:
		return "stopped"
	case ri.ExitSignal != "":
		return "crashed"
	case ri.ExitCode != nil && *ri.ExitCode != 0:
		return "crashed"
	case ri.ExitCode != nil:
		return "exited"
	default:
		return "crashed"
	}
}

// LogLine is a single line of llama-server output.
type LogLine struct {
	Timestamp time.Time
	Level     string // INFO | WARN | ERROR | "" if unparseable
	Text      string
}
