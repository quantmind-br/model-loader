package pages

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

// modelPickerOverlay groups the page-owned ctrl+p picker overlay state
// (active flag, the picker model, and the scanner/paths used to seed it).
// Bundled so ProfilesPage stays under the 12-field cap from CQ-002.
type modelPickerOverlay struct {
	active    bool
	picker    components.ModelPicker
	scanner   components.ModelScanner
	scanPaths []string
}

type launchTracker struct {
	status   string
	statusAt time.Time
	waitPID  int
	spinner  spinner.Model
}

// loadedMsg is emitted by the load command.
type loadedMsg struct {
	profiles []domain.Profile
	diags    []profilestore.ListDiagnostic
	err      error
}

// launchedMsg is emitted after a successful Launch + WaitHealthy.
type launchedMsg struct {
	inst      domain.RunningInstance
	attemptID string
}

// launchErrMsg is emitted when validation or Launch itself fails.
// firstIssue carries the first validation issue (when the failure was a
// validation error) so the flash can surface a human-readable hint —
// otherwise the user sees only "validation failed: N errors" with no
// indication of WHICH fields broke (F-01 audit).
type launchErrMsg struct {
	err        error
	firstIssue string
}

type healthyMsg struct{ pid int }

// profilesKillConfirmedMsg is emitted by killConfirm.onYes when the user
// confirms a kill. The page handles it in Update so manager I/O and status
// mutation stay on the UI thread.
type profilesKillConfirmedMsg struct{ pid int }

// profileDeleteConfirmedMsg is emitted by deleteConfirm.onYes when the user
// confirms a profile deletion. The page handles it in Update so the actual
// store mutation, flash, and reload all happen on the UI thread.
type profileDeleteConfirmedMsg struct{ id string }

type importDoneMsg struct {
	Result profilestore.ImportResult
}

type undoDoneMsg struct{}
