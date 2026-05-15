package domain

import "time"

// RunningInstance describes a live llama-server process tracked by ProcessManager.
type RunningInstance struct {
	ProfileID  string     `json:"profileId"`
	PID        int        `json:"pid"`
	Port       int        `json:"port"`
	LogPath    string     `json:"logPath"`
	BinaryPath string     `json:"binaryPath,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	Background bool       `json:"background"`
	Crashed    bool       `json:"crashed,omitempty"`
	ExitedAt   *time.Time `json:"exitedAt,omitempty"`
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

// LogLine is a single line of llama-server output.
type LogLine struct {
	Timestamp time.Time
	Level     string // INFO | WARN | ERROR | "" if unparseable
	Text      string
}
