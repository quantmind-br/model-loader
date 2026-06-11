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

// Tab attention page names root maps to tabs.
const (
	AttentionModels = "models"
	AttentionServer = "server"
)

// TabAttentionMsg signals a background event (download finished/failed,
// instance crash) so root can badge the owning tab until it's visited.
type TabAttentionMsg struct{ Page string }

// truncate clips s to the target visual width, replacing the tail with "…"
// when the input is wider. Uses theme.RuneWidth so CJK and emoji are counted
// by display cells, not bytes or runes.
func truncate(s string, max int) string {
	if theme.RuneWidth(s) <= max {
		return s
	}
	return theme.TruncateRuneWidth(s, max, "…")
}
