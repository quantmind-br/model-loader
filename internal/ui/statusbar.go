package ui

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/pages"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// globalHints is the prefix shown in every status bar line.
const globalHints = "[1-5] tabs  [tab] next  [q] quit" + components.HelpToken

// recomputeHints reads page-local hints (when the active page implements
// HintProvider) and updates the status bar to globalHints + " | " +
// page hints. Called after every page state change so the status footer
// always reflects what the user can do right now. Also pulls the active
// page's StatusMessage so critical feedback (launch errors, export
// results) lands in the always-visible status bar instead of being
// clipped to the in-body flash at narrow geometries.
func (m *RootModel) recomputeHints() {
	if h, ok := m.pages[m.active].(HintProvider); ok {
		if ph := h.Hints(); ph != "" {
			m.status.Hints = globalHints + " | " + ph
		} else {
			m.status.Hints = globalHints
		}
	} else {
		m.status.Hints = globalHints
	}
	if sp, ok := m.pages[m.active].(StatusMessageProvider); ok {
		msg, level := sp.StatusMessage()
		if msg != "" {
			m.status.SetMessage(level, msg)
		} else if m.status.Message != "" && m.status.Level != components.StatusWarn {
			// Clear stale page-sourced messages but preserve the boot
			// warning (StatusWarn from WithStatusWarn) until it's
			// explicitly replaced.
			m.status.SetMessage(components.StatusInfo, "")
		}
	}
	m.status.RestartCount = 0
	if m.pm != nil {
		for _, inst := range m.pm.List() {
			m.status.RestartCount += inst.RestartCount
		}
	}
}

// attentionTab maps a TabAttentionMsg page name to its Tab.
func attentionTab(page string) (Tab, bool) {
	switch page {
	case pages.AttentionModels:
		return TabModels, true
	case pages.AttentionServer:
		return TabServer, true
	default:
		return 0, false
	}
}

// attentionGlyph is the badge rendered next to a tab label with a pending
// background event; ASCII fallback under NO_COLOR. ● is East-Asian
// ambiguous-width (RENDER-02): on CJK-wide terminals it may render 2 cells
// and cost the strip a column — accepted, matching the ‹› indicators this
// component already uses in color mode.
func attentionGlyph() string {
	if theme.NoColor() {
		return "*"
	}
	return "●"
}

func (m RootModel) renderTabs() string {
	labels := make([]string, tabCount)
	badges := make([]string, tabCount)
	glyph := attentionGlyph()
	for i := Tab(0); i < tabCount; i++ {
		labels[i] = fmt.Sprintf("%d %s", int(i)+1, i.Title())
		if m.badges[i] {
			badges[i] = glyph
		}
	}
	return components.TabBar(components.TabBarOptions{
		Labels:         labels,
		ActiveIndex:    int(m.active),
		AvailableWidth: m.width,
		Badges:         badges,
	})
}
