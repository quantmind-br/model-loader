package pages

import (
	"github.com/charmbracelet/bubbles/spinner"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
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
	inFlight bool
	spinner  spinner.Model
}

// loadedMsg is emitted by the load command.
type loadedMsg struct {
	profiles []domain.Profile
	diags    []profilestore.ListDiagnostic
	err      error
}

// proxyLoadedMsg reports a successful /_admin/load (backend already healthy).
type proxyLoadedMsg struct{ status httpproxy.Status }

// launchErrMsg is emitted when validation or the proxy load fails.
// firstIssue carries the first validation issue (when the failure was a
// validation error) so the flash can surface a human-readable hint —
// otherwise the user sees only "validation failed: N errors" with no
// indication of WHICH fields broke (F-01 audit).
type launchErrMsg struct {
	err        error
	firstIssue string
}

// profilesUnloadConfirmedMsg is emitted by killConfirm.onYes when the user
// confirms unloading the currently loaded model. The page handles it in
// Update so the async proxy call is dispatched from the UI thread.
type profilesUnloadConfirmedMsg struct{ profileID string }

// profilesUnloadDoneMsg carries the outcome of the async /_admin/unload.
type profilesUnloadDoneMsg struct {
	profileID string
	err       error
}

// profileDeleteConfirmedMsg is emitted by deleteConfirm.onYes when the user
// confirms a profile deletion. The page handles it in Update so the actual
// store mutation, flash, and reload all happen on the UI thread.
type profileDeleteConfirmedMsg struct{ id string }

type importDoneMsg struct {
	Result profilestore.ImportResult
}

// importFailedMsg surfaces an ImportBundle error so the user sees an error
// flash instead of a silent no-op (the cmd previously returned an empty
// FlashClearMsg on failure).
type importFailedMsg struct{ err error }

type undoDoneMsg struct{}

// undoFailedMsg surfaces a failed restore-previous save (same silent-failure
// fix as importFailedMsg).
type undoFailedMsg struct{ err error }
