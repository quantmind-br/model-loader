package pages

import (
	"context"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

type modelsKeyMap struct {
	Filter, Rescan, Enter, Cancel key.Binding
}

// NavigateToSizingMsg is emitted by ModelsPage when the user presses →/g on
// the info panel for a model used by exactly one profile. root.go consumes
// it to switch to the Profiles tab and open that profile in the editor.
type NavigateToSizingMsg struct {
	ModelPath string
	ProfileID string
}

func defaultModelsKeys() modelsKeyMap {
	return modelsKeyMap{
		Filter: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Rescan: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "rescan")),
		Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "actions")),
		Cancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear filter")),
	}
}

// IsCapturingInput tells the root model when the page owns global
// keystrokes (Tab/Shift+Tab) — true while the inline action menu or
// filter input is open so cursor navigation does not leak into tab
// cycling and printable characters are not stolen by global shortcuts.
func (p ModelsPage) IsCapturingInput() bool {
	return CaptureAny(
		func() bool { return p.action != nil },
		func() bool { return p.deleteConfirm.Active() },
		func() bool { return p.filterMode },
		func() bool { return p.profilePicker != nil },
		func() bool { return p.hfSearch != nil && p.hfSearch.IsActive() },
		func() bool { return p.hfFilePicker != nil && p.hfFilePicker.IsActive() },
		func() bool { return p.downloads != nil && p.downloads.IsFocusVisible() },
	)
}

// scanStartedMsg delivers the channel + cancel handle from a fresh scan
// start. State mutations happen when this message lands in Update.
// scanID matches ModelsPage.scanID at start; stale messages from a
// previous rescan are discarded by epoch comparison.
type scanStartedMsg struct {
	scanID int
	ch     <-chan domain.ScanEvent
	cancel context.CancelFunc
	err    error
}

// scanEventMsg carries one ScanEvent plus the channel for re-arming.
type scanEventMsg struct {
	scanID int
	ch     <-chan domain.ScanEvent
	evt    domain.ScanEvent
}

// scanChannelClosedMsg signals the scan goroutine finished and closed
// its channel.
type scanChannelClosedMsg struct {
	scanID int
}

// modelsReloadMsg is dispatched by Reload() when the root activates this
// tab. It triggers a silent rescan (no flash) so external filesystem
// changes surface without requiring the user to press R.
type modelsReloadMsg struct{}

// downloadEventMsg lifts a downloadmgr.Event onto the Bubble Tea bus so
// the page can react to lifecycle changes (queued → active → completed/
// failed/cancelled). Read pump lives in T13/T15.
type downloadEventMsg struct {
	event downloadmgr.Event
}

// downloadCancelMsg requests cancellation of a download by ID. Emitted
// by the progress footer (T15 wires the Manager.Cancel call).
type downloadCancelMsg struct {
	id string
}

// handleFilterKey processes keystrokes while the filter buffer is active.
// It returns handled=true when the key is consumed by filter mode; when
// handled=false the caller falls through to the command-key dispatch
// (e.g. Enter, which acts on the current selection).
func (p ModelsPage) handleFilterKey(msg tea.KeyMsg) (handled bool, m tea.Model, cmd tea.Cmd) {
	switch {
	case key.Matches(msg, p.keys.Filter):
		p.filterMode = false
		return true, p, nil
	case key.Matches(msg, p.keys.Cancel):
		p.filterMode = false
		p.filter = ""
		p.refreshRows()
		return true, p, nil
	case key.Matches(msg, p.keys.Enter):
		// fall through to the action-menu logic in handleKey
		return false, p, nil
	default:
		if msg.String() == "backspace" {
			if len(p.filter) > 0 {
				rs := []rune(p.filter)
				p.filter = string(rs[:len(rs)-1])
				p.refreshRows()
			}
			return true, p, nil
		}
		// Append every rune in the message, not just single-rune events.
		// Fast/bursted typing (and paste) arrives as one KeyMsg carrying
		// multiple runes — the old `== 1` guard silently dropped those,
		// losing characters under speed (INPUT-01).
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			p.filter += string(msg.Runes)
			p.refreshRows()
			return true, p, nil
		}
		return true, p, nil
	}
}

func (p ModelsPage) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While typing in the filter buffer, route printable runes to the filter.
	// Only Filter (toggle off), Cancel (clear), and Enter (act on selection)
	// remain active — page shortcuts like Rescan must NOT fire from rune keys.
	if p.filterMode {
		if handled, m, cmd := p.handleFilterKey(msg); handled {
			return m, cmd
		}
	}
	if p.hfSearch != nil && p.hfSearch.IsActive() {
		if key.Matches(msg, p.keys.Enter) {
			item, ok := p.hfSearch.Selected()
			if !ok {
				return p, nil
			}
			return p.openHFFilePicker(item)
		}
		return p, p.hfSearch.Update(msg)
	}
	if p.hfFilePicker != nil && p.hfFilePicker.IsActive() {
		if key.Matches(msg, p.keys.Enter) {
			return p.startSelectedDownloads()
		}
		return p, p.hfFilePicker.Update(msg)
	}
	if p.downloads != nil && p.downloads.IsFocusVisible() {
		return p, p.downloads.Update(msg)
	}

	switch {
	case key.Matches(msg, p.keys.Filter):
		p.filterMode = !p.filterMode
		return p, nil
	case key.Matches(msg, p.keys.Cancel):
		if p.infoPanel != nil {
			p.infoPanel = nil
			p.infoPanelUsedBy = nil
			p.resizeColumns(p.width)
			return p, nil
		}
		if p.filterMode || p.filter != "" {
			p.filterMode = false
			p.filter = ""
			p.refreshRows()
			return p, nil
		}
	case key.Matches(msg, p.keys.Rescan):
		if p.isScanning() {
			return p.withFlashError("Scan already in progress")
		}
		next, cmd := p.beginRescan(true)
		return next, cmd
	case key.Matches(msg, p.keys.Enter):
		return p.openActionMenuForSelection()
	case msg.String() == "i":
		if !p.filterMode {
			if p.infoPanel != nil {
				p.infoPanel = nil
				p.infoPanelUsedBy = nil
				p.resizeColumns(p.width)
				return p, nil
			}
			return p.openInfoPanel()
		}
	case msg.String() == "right", msg.String() == "g":
		if p.infoPanel != nil {
			visible := p.visibleFiles()
			idx := p.table.Cursor()
			var path string
			if idx >= 0 && idx < len(visible) {
				path = visible[idx].Path
			}
			if len(p.infoPanelUsedBy) == 1 {
				return p, func() tea.Msg {
					return NavigateToSizingMsg{ModelPath: path, ProfileID: p.infoPanelUsedBy[0].ID}
				}
			}
			if len(p.infoPanelUsedBy) > 1 {
				p, fc := p.withFlash("Multiple profiles use this model — switch to Profiles tab manually")
				return p, fc
			}
			p, fc := p.withFlash("No profile uses this model — create one first")
			return p, fc
		}
	case msg.String() == "s":
		if !p.filterMode && p.hfClient != nil {
			p.hfSearch = components.NewHFSearchPicker(hfSearcherAdapter{client: p.hfClient}, p.width, p.height)
			return p, p.hfSearch.Init()
		}
	case msg.String() == "x":
		if p.downloads != nil && p.downloads.IsVisible() {
			return p, p.downloads.Update(msg)
		}
	case msg.String() == "d":
		// Toggle download footer visibility even when currently
		// hidden — the component's own forwarding only kicks in
		// after IsVisible(), so we need an unconditional entry point.
		if p.downloads != nil {
			return p, p.downloads.Update(msg)
		}
	case msg.String() == "r":
		if p.downloads != nil && p.downloads.IsVisible() {
			return p, p.downloads.Update(msg)
		}
	}

	t, cmd := p.table.Update(msg)
	p.table = t
	return p, cmd
}

type modelDeleteConfirmedMsg struct{ path string }

// UseInNewProfileMsg requests creating a new profile pre-filled with Path.
// Root catches this message, switches to the Profiles tab, and forwards
// it so ProfilesPage starts a new draft.
type UseInNewProfileMsg struct {
	Path string
}
