package pages

// SwitchToMonitorMsg is emitted by LauncherPage after a launch is healthy.
// root.go consumes this to switch the active tab to Monitor and pre-select
// the new PID.
type SwitchToMonitorMsg struct {
	PID int
}

// MonitorSelectPIDMsg instructs MonitorPage to select the row whose PID
// matches. Sent by root when handling SwitchToMonitorMsg so the Monitor
// page lands focused on the newly-launched instance.
type MonitorSelectPIDMsg struct {
	PID int
}

// truncate clips s to max runes, replacing the last with "…" when the
// input is longer. Used by ModelsPage for table-cell shortening.
//
// NOTE: name is misleading — this clips by BYTES, not runes. Safe for the
// ASCII-only callers in models.go / profile_editor/draft.go. For UTF-8 input
// (e.g. stderr-tail lines that may contain multi-byte characters), use
// `truncRunes` below instead.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// truncRunes clips s to max RUNES (not bytes), appending "…" when the input
// is longer. Rune-safe alternative to truncate() above, which slices by
// byte index and would corrupt multi-byte UTF-8 mid-rune. Used by
// launcher.go's enrichWithExit for stderr-tail lines (llama-server may emit
// non-ASCII in localized CUDA error messages).
//
// Edge cases: max <= 0 returns ""; max == 1 returns "…" for any non-empty s.
func truncRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(rs[:max-1]) + "…"
}
