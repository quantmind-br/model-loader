package pages

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

func (p ModelsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if p.downloadEvents != nil {
		select {
		case ev := <-p.downloadEvents:
			return p, func() tea.Msg { return downloadEventMsg{event: ev} }
		default:
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return p.handleResize(msg)
	case components.FlashClearMsg:
		p.flash, _ = p.flash.Update(msg)
		return p, nil
	case scanStartedMsg:
		if msg.scanID != p.scanID {
			if msg.cancel != nil {
				msg.cancel()
			}
			return p, nil
		}
		if msg.err != nil {
			for _, root := range p.paths {
				p.statusMap[root] = pathStatus{state: "error", err: msg.err.Error()}
			}
			return p, nil
		}
		p.cancel = msg.cancel
		return p, waitForScanEvent(msg.ch, msg.scanID)
	case scanEventMsg:
		if msg.scanID != p.scanID {
			return p, waitForScanEvent(msg.ch, msg.scanID)
		}
		updated, _ := p.handleScanEvent(msg.evt)
		next := updated.(ModelsPage)
		return next, waitForScanEvent(msg.ch, msg.scanID)
	case scanChannelClosedMsg:
		return p, nil
	case modelsReloadMsg:
		next, cmd := p.beginRescan(false)
		return next, cmd
	case downloadEventMsg:
		return p.handleDownloadEvent(msg.event)
	case components.HFSearchResultMsg:
		if p.hfSearch != nil && p.hfSearch.IsActive() {
			return p, p.hfSearch.Update(msg)
		}
		return p, nil
	case hfSearchResultMsg:
		if p.hfSearch != nil && p.hfSearch.IsActive() {
			return p, p.hfSearch.Update(msg.componentMsg())
		}
		return p, nil
	case components.HFFileListMsg:
		if p.hfFilePicker != nil && p.hfFilePicker.IsActive() {
			return p, p.hfFilePicker.Update(msg)
		}
		return p, nil
	case hfFileListMsg:
		if p.hfFilePicker != nil && p.hfFilePicker.IsActive() {
			return p, p.hfFilePicker.Update(msg.componentMsg())
		}
		return p, nil
	case components.DownloadCancelMsg:
		if p.dlManager != nil {
			_ = p.dlManager.Cancel(downloadmgr.ID(msg.ID))
		}
		return p, nil
	case downloadCancelMsg:
		if p.dlManager != nil {
			_ = p.dlManager.Cancel(downloadmgr.ID(msg.id))
		}
		return p, nil
	case components.DownloadResumeMsg:
		if p.dlManager != nil {
			if err := p.dlManager.Resume(downloadmgr.ID(msg.ID)); err != nil {
				return p.withFlashError("resume: " + err.Error())
			}
			return p.withFlash("resuming " + msg.ID)
		}
		return p, nil
	case components.ProfilePickedMsg:
		return p.handleProfilePicked(msg)
	case components.ProfilePickerCancelledMsg:
		p.profilePicker = nil
		p.profilePickerTargetPath = ""
		return p, nil
	case modelDeleteConfirmedMsg:
		return p.performDelete(msg.path)
	case tea.KeyMsg:
		return p.dispatchKey(msg)
	default:
		return p.forwardNonKey(msg)
	}
}

func (p ModelsPage) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	p.width, p.height = msg.Width, msg.Height
	p.table.SetHeight(msg.Height - 8)
	if p.hfSearch != nil {
		p.hfSearch.SetSize(msg.Width, msg.Height)
	}
	if p.hfFilePicker != nil {
		p.hfFilePicker.SetSize(msg.Width, msg.Height)
	}
	if p.downloads != nil {
		p.downloads.SetWidth(msg.Width)
	}
	return p, nil
}

func (p ModelsPage) dispatchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if p.profilePicker != nil {
		np, cmd := p.profilePicker.Update(msg)
		p.profilePicker = &np
		return p, cmd
	}
	if p.deleteConfirm.Active() {
		return p.updateDeleteConfirm(msg)
	}
	if p.action != nil {
		return p.updateActionMenu(msg)
	}
	return p.handleKey(msg)
}

// forwardNonKey routes non-key messages to the highest-priority active
// surface so its internal Cmd→Msg loops complete (huh focus init, async
// transitions). Without this the deleteConfirm's huh.Form never reaches
// StateCompleted on Enter and onYes never fires.
func (p ModelsPage) forwardNonKey(msg tea.Msg) (tea.Model, tea.Cmd) {
	if p.deleteConfirm.Active() {
		var cmd tea.Cmd
		p.deleteConfirm, cmd = p.deleteConfirm.Update(msg)
		return p, cmd
	}
	return p, nil
}
