package pages

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/configweb"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

func (p ProfilesPage) handleUseInNewProfile(msg UseInNewProfileMsg) (tea.Model, tea.Cmd) {
	d := configweb.Draft{
		IsNew:     true,
		Name:      "New Profile",
		ID:        domain.Slugify("New Profile"),
		Model:     msg.Path,
		Args:      map[string]string{},
		ExtraArgs: []string{},
	}
	if p.catalogStore != nil {
		if catalog, err := p.catalogStore.Load(); err == nil && catalog.DefaultBackendID != "" {
			d.BackendID = catalog.DefaultBackendID
		}
	}
	p.webEditing = true
	p, fc := p.withFlash("new profile prefilled with picked model")
	return p, tea.Batch(p.startWebEdit(d), fc)
}

func (p ProfilesPage) handleModelPicked(msg components.ModelPickedMsg) (tea.Model, tea.Cmd) {
	p.picker.active = false
	if c := p.picker.picker.Cancel(); c != nil {
		c()
	}
	_ = msg // model picker no longer used with huh editor; web editor receives model path via URL
	return p, nil
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
			return p.startEditSelected()
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
