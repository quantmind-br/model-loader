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
	Kind           BackendKind `json:"kind,omitempty"`
	StartedAt      time.Time   `json:"startedAt"`
	Background     bool        `json:"background"`
	Crashed        bool        `json:"crashed,omitempty"`
	ExitedAt       *time.Time  `json:"exitedAt,omitempty"`
	RestartCount   int         `json:"restartCount,omitempty"`
	LastRestartAt  *time.Time  `json:"lastRestartAt,omitempty"`
	RestartPolicy  string      `json:"restartPolicy,omitempty"`
	MaxRestarts    int         `json:"maxRestarts,omitempty"`
	BackoffSeconds int         `json:"backoffSeconds,omitempty"`
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

// LogLine is a single line of llama-server output.
type LogLine struct {
	Timestamp time.Time
	Level     string // INFO | WARN | ERROR | "" if unparseable
	Text      string
}
