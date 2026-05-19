package pages

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/ui/components"
)

// handleProfilePicked applies the selected GGUF path to the chosen
// existing profile and persists, then clears the picker state.
func (p ModelsPage) handleProfilePicked(msg components.ProfilePickedMsg) (tea.Model, tea.Cmd) {
	targetPath := p.profilePickerTargetPath
	p.profilePicker = nil
	p.profilePickerTargetPath = ""
	if p.store == nil {
		p, fc := p.withFlashError("profile store not wired")
		return p, fc
	}
	pr, err := p.store.Get(msg.ID)
	if err != nil {
		p, fc := p.withFlashError("load profile: " + err.Error())
		return p, fc
	}
	pr.Model = targetPath
	if err := p.store.Save(pr); err != nil {
		p, fc := p.withFlashError("save profile: " + err.Error())
		return p, fc
	}
	p, fc := p.withFlash("updated " + msg.ID)
	return p, fc
}
