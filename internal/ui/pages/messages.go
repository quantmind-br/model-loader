package pages

import "github.com/quantmind-br/model-loader/internal/ui/theme"

// LaunchProfileMsg requests the Profiles page to start the profile identified
// by ID. Routed by root.go to the Profiles tab.
type LaunchProfileMsg struct {
	ID string
}

// SwitchToServerMsg is emitted by ProfilesPage after a launch is healthy.
// root.go consumes this to switch the active tab to Server and pre-select
// the new PID.
type SwitchToServerMsg struct {
	PID int
}

// ServerSelectPIDMsg instructs ServerPage to select the row whose PID
// matches. Sent by root when handling SwitchToServerMsg so the Server
// page lands focused on the newly-launched instance.
type ServerSelectPIDMsg struct {
	PID int
}

// truncate clips s to the target visual width, replacing the tail with "…"
// when the input is wider. Uses theme.RuneWidth so CJK and emoji are counted
// by display cells, not bytes or runes.
func truncate(s string, max int) string {
	if theme.RuneWidth(s) <= max {
		return s
	}
	return theme.TruncateRuneWidth(s, max, "…")
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
