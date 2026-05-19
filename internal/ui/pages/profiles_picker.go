package pages

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/ui/components"
)

func (p ProfilesPage) handleUseInNewProfile(msg UseInNewProfileMsg) (tea.Model, tea.Cmd) {
	d := p.newDraftDefaults()
	d.Model = msg.Path
	p.editor = p.prepareEditor()
	var openCmd tea.Cmd
	p.editor, openCmd = p.editor.Open(d)
	p, fc := p.withFlash("new profile prefilled with picked model")
	return p, tea.Batch(openCmd, fc)
}

func (p ProfilesPage) handleModelPicked(msg components.ModelPickedMsg) (tea.Model, tea.Cmd) {
	p.picker.active = false
	if c := p.picker.picker.Cancel(); c != nil {
		c()
	}
	var cmd tea.Cmd
	p.editor, cmd = p.editor.SetModelPath(msg.Path)
	return p, cmd
}

func (p ProfilesPage) handleModelPickerCancelled(_ components.ModelPickerCancelledMsg) (tea.Model, tea.Cmd) {
	p.picker.active = false
	return p, nil
}

func (p ProfilesPage) handleNavigateToSizing(msg NavigateToSizingMsg) (tea.Model, tea.Cmd) {
	items := p.list.Items()
	for i, it := range items {
		if sel, ok := it.(item); ok && sel.p.ID == msg.ProfileID {
			p.list.Select(i)
			p, cmd := p.startEditSelected()
			if rm, ok := p.(ProfilesPage); ok {
				rm.editor = rm.editor.SetSubTabSizing()
				return rm, cmd
			}
			return p, cmd
		}
	}
	p, fc := p.withFlashError("profile not found for sizing navigation")
	return p, fc
}

func (p ProfilesPage) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	picker, cmd := p.picker.picker.Update(msg)
	p.picker.picker = picker
	return p, cmd
}
