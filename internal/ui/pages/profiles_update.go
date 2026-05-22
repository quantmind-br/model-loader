package pages

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/ui/components"
)

// Update is a thin dispatcher: each typed-message arm delegates to a
// private handle<MsgType> method. Non-key messages forward to active surfaces
// (delete confirm, list filter) so their internal Cmd→Msg handshakes complete.
func (p ProfilesPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		return p.handleResize(m)
	case components.FlashClearMsg:
		return p.handleFlashClear(m)
	case loadedMsg:
		return p.handleLoaded(m)
	case components.PickerScanStartedMsg, components.PickerScanEventMsg, components.PickerScanClosedMsg:
		return p.handlePickerScan(msg)
	case UseInNewProfileMsg:
		return p.handleUseInNewProfile(m)
	case components.ModelPickedMsg:
		return p.handleModelPicked(m)
	case components.ModelPickerCancelledMsg:
		return p.handleModelPickerCancelled(m)
	case profileDeleteConfirmedMsg:
		return p.performDelete(m.id)
	case importDoneMsg:
		return p.handleImportDone(m)
	case undoDoneMsg:
		return p.handleUndoDone(m)
	case NavigateToSizingMsg:
		return p.handleNavigateToSizing(m)
	case launchedMsg:
		return p.handleLaunched(m)
	case healthyMsg:
		return p.handleHealthy(m)
	case launchErrMsg:
		return p.handleLaunchErr(m)
	case spinner.TickMsg:
		return p.handleSpinnerTick(m)
	case LaunchProfileMsg:
		return p.handleLaunchProfile(m)
	case profilesKillConfirmedMsg:
		return p.handleKillConfirmed(m)
	case webEditStartedMsg:
		p.webSession = m.session
		p.webURL = m.url
		return p, waitForWebEdit(m.session)
	case webEditFailedMsg:
		p.webEditing = false
		p, fc := p.withFlashError("web editor failed: " + m.err.Error())
		return p, fc
	case webEditDoneMsg:
		p.webEditing = false
		p.webSession = nil
		if m.err != nil {
			p, fc := p.withFlashError("web editor error: " + m.err.Error())
			return p, fc
		}
		var fc tea.Cmd
		if m.saved {
			p, fc = p.withFlash("saved " + m.profileID)
		}
		return p, tea.Batch(p.loadCmd(), fc)
	case tea.KeyMsg:
		return p.handleKey(m)
	}
	return p.forwardNonKey(msg)
}

func (p ProfilesPage) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	p.width, p.height = msg.Width, msg.Height
	listWidth := p.width / 3
	listHeight := msg.Height - 6
	if listHeight < 3 {
		listHeight = 3
	}
	p.list.SetSize(listWidth, listHeight)
	return p, nil
}

func (p ProfilesPage) handleFlashClear(msg components.FlashClearMsg) (tea.Model, tea.Cmd) {
	p.flash, _ = p.flash.Update(msg)
	return p, nil
}

func (p ProfilesPage) handleLoaded(msg loadedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		p, fc := p.withFlashError("load error: " + msg.err.Error())
		return p, fc
	}
	sortProfilesPinnedFirst(msg.profiles)
	items := make([]list.Item, 0, len(msg.profiles)+len(msg.diags))
	for _, pr := range msg.profiles {
		items = append(items, item{p: pr})
	}
	for _, d := range msg.diags {
		items = append(items, corruptItem{id: d.ID, err: d.Err})
	}
	p.list.SetItems(items)
	return p, nil
}

func (p ProfilesPage) handlePickerScan(msg tea.Msg) (tea.Model, tea.Cmd) {
	if p.picker.active {
		return p.updatePicker(msg)
	}
	return p, nil
}

// handleKey routes key input. Priority: web editor > delete confirm > list nav.
// Picker is intercepted on ctrl+p or while open.
func (p ProfilesPage) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if p.webEditing {
		if msg.String() == "esc" && p.webSession != nil {
			p.webSession.Cancel()
		}
		return p, nil
	}
	if p.killConfirm.Active() {
		return p.updateKillConfirm(msg)
	}
	if p.importPickerActive {
		updated, cmd := p.importPicker.Update(msg)
		p.importPicker = updated
		if didSelect, path := p.importPicker.DidSelectFile(msg); didSelect {
			p.importPickerActive = false
			return p.startImportWithPath(path)
		}
		if msg.String() == "esc" {
			p.importPickerActive = false
			return p, nil
		}
		return p, cmd
	}
	if p.picker.active {
		return p.updatePicker(msg)
	}
	if p.conflictModal.Active() {
		var cmd tea.Cmd
		p.conflictModal, cmd = p.conflictModal.Update(msg)
		return p, cmd
	}
	if p.undoModal.Active() {
		var cmd tea.Cmd
		p.undoModal, cmd = p.undoModal.Update(msg)
		return p, cmd
	}
	if p.deleteConfirm.Active() {
		return p.updateConfirm(msg)
	}
	return p.updateList(msg)
}

// forwardNonKey routes non-key messages to active surfaces so their internal
// Cmd→Msg loops complete (huh focus init, async validation). Forwards to
// delete confirm, then to the list for filter Cmd→Msg cycles.
func (p ProfilesPage) forwardNonKey(msg tea.Msg) (tea.Model, tea.Cmd) {
	if p.deleteConfirm.Active() {
		var cmd tea.Cmd
		p.deleteConfirm, cmd = p.deleteConfirm.Update(msg)
		return p, cmd
	}
	if _, isFilterMatches := msg.(list.FilterMatchesMsg); isFilterMatches {
		updated, cmd := p.list.Update(msg)
		p.list = updated
		return p, cmd
	}
	return p, nil
}

func (p ProfilesPage) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if p.list.FilterState() == list.Filtering {
		updated, cmd := p.list.Update(msg)
		p.list = updated
		return p, cmd
	}
	switch {
	case key.Matches(msg, p.listKeys.New):
		return p.startNew()
	case key.Matches(msg, p.listKeys.Edit):
		return p.startEditSelected()
	case key.Matches(msg, p.listKeys.Duplicate):
		return p.duplicateSelected()
	case key.Matches(msg, p.listKeys.Delete):
		return p.askDeleteSelected()
	case key.Matches(msg, p.listKeys.Launch):
		return p.launchSelected()
	case key.Matches(msg, p.listKeys.BgToggle):
		p.bgMode = !p.bgMode
		return p, nil
	case key.Matches(msg, p.listKeys.Kill):
		return p.askKillMostRecent()
	case key.Matches(msg, p.listKeys.Refresh):
		return p, p.loadCmd()
	case key.Matches(msg, p.listKeys.Export):
		return p.exportProfiles()
	case key.Matches(msg, p.listKeys.Pin):
		return p.togglePinSelected()
	case key.Matches(msg, p.listKeys.Import):
		return p.startImport()
	case key.Matches(msg, p.listKeys.Undo):
		return p.startUndo()
	}

	updated, cmd := p.list.Update(msg)
	p.list = updated
	return p, cmd
}

func (p ProfilesPage) IsCapturingInput() bool {
	return CaptureAny(
		func() bool { return p.webEditing },
		func() bool { return p.deleteConfirm.Active() },
		func() bool { return p.picker.active },
		func() bool { return p.conflictModal.Active() },
		func() bool { return p.undoModal.Active() },
		func() bool { return p.importPickerActive },
		func() bool { return p.killConfirm.Active() },
		func() bool { return p.list.FilterState() != list.Unfiltered },
	)
}

// Reload triggers a fresh load from the underlying store. Called by the
// root when the user navigates back to the Profiles tab.
func (p ProfilesPage) Reload() tea.Cmd {
	return p.loadCmd()
}

func (p ProfilesPage) withFlash(msg string) (ProfilesPage, tea.Cmd) {
	var cmd tea.Cmd
	p.flash, cmd = flashSuccess(p.flash, msg)
	return p, cmd
}

func (p ProfilesPage) withFlashError(msg string) (ProfilesPage, tea.Cmd) {
	var cmd tea.Cmd
	p.flash, cmd = flashError(p.flash, msg)
	return p, cmd
}

func (p ProfilesPage) updateKillConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	p.killConfirm, cmd = p.killConfirm.Update(msg)
	return p, cmd
}
