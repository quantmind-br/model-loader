package components

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/ui/internal/filter"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// ProfilePickedMsg is emitted when the user confirms a selection.
type ProfilePickedMsg struct {
	ID string
}

// ProfilePickerCancelledMsg is emitted on Esc / cancel.
type ProfilePickerCancelledMsg struct{}

// profilePickerItem is the picker's internal row representation.
// FilterValue holds the searchable surface: profile name + ID + tags
// + backend ID, joined by spaces so substring search hits any field.
type profilePickerItem struct {
	id          string
	name        string
	backendID   string
	tags        []string
	filterValue string
}

// FilterValue exposes the precomputed haystack used by ContainsFold.
func (it profilePickerItem) FilterValue() string { return it.filterValue }

// ProfilePicker is a modal overlay listing existing profiles, mirroring
// ModelPicker's key handling but without the async scanner — all rows
// are passed in at construction time.
type ProfilePicker struct {
	items    []profilePickerItem
	filtered []profilePickerItem

	cursor     int
	filter     string
	filterMode bool

	width  int
	height int
}

// NewProfilePicker builds a picker over the provided profile slice.
// Sort and filter are applied immediately so the first View() renders
// the full sorted list.
func NewProfilePicker(profiles []domain.Profile) ProfilePicker {
	items := make([]profilePickerItem, 0, len(profiles))
	for _, pr := range profiles {
		fv := pr.Name + " " + pr.ID + " " + strings.Join(pr.Tags, " ") + " " + pr.Launch.BackendID
		items = append(items, profilePickerItem{
			id:          pr.ID,
			name:        pr.Name,
			backendID:   pr.Launch.BackendID,
			tags:        append([]string(nil), pr.Tags...),
			filterValue: fv,
		})
	}
	p := ProfilePicker{items: items}
	p.applyFilter()
	return p
}

// Update routes window-resize and key events into the picker state.
// All other message types are ignored.
func (p ProfilePicker) Update(msg tea.Msg) (ProfilePicker, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		return p, nil
	case tea.KeyMsg:
		return p.handleKey(msg)
	}
	return p, nil
}

// handleKey dispatches one key event. Esc / Enter terminate the picker;
// '/' toggles filter mode; while filter mode is on printable runes
// append to the filter; otherwise arrows / pgup-pgdn / home-end / g / G
// move the cursor.
func (p ProfilePicker) handleKey(msg tea.KeyMsg) (ProfilePicker, tea.Cmd) {
	keys := defaultPickerKeys()
	if key.Matches(msg, keys.Esc) {
		return p, func() tea.Msg { return ProfilePickerCancelledMsg{} }
	}
	if key.Matches(msg, keys.Enter) {
		if len(p.filtered) == 0 {
			return p, nil
		}
		id := p.filtered[p.cursor].id
		return p, func() tea.Msg { return ProfilePickedMsg{ID: id} }
	}
	if key.Matches(msg, keys.Filter) {
		p.filterMode = !p.filterMode
		return p, nil
	}
	if p.filterMode {
		return p.handleFilterKey(msg, keys)
	}
	return p.handleNavKey(msg, keys)
}

// handleFilterKey runs while filter mode is on: backspace pops a char,
// printable runes append. Anything else falls through to navigation.
func (p ProfilePicker) handleFilterKey(msg tea.KeyMsg, keys pickerKeys) (ProfilePicker, tea.Cmd) {
	if key.Matches(msg, keys.Backspace) {
		if len(p.filter) > 0 {
			p.filter = p.filter[:len(p.filter)-1]
			p.applyFilter()
		}
		return p, nil
	}
	if len(msg.Runes) == 1 {
		p.filter += string(msg.Runes)
		p.applyFilter()
		return p, nil
	}
	return p.handleNavKey(msg, keys)
}

// handleNavKey runs cursor navigation: up/down, pgup/pgdn, home/end,
// and (only outside filter mode) g / G.
func (p ProfilePicker) handleNavKey(msg tea.KeyMsg, keys pickerKeys) (ProfilePicker, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Up):
		if p.cursor > 0 {
			p.cursor--
		}
	case key.Matches(msg, keys.Down):
		if p.cursor < len(p.filtered)-1 {
			p.cursor++
		}
	case key.Matches(msg, keys.PgUp):
		p.cursor -= 10
		if p.cursor < 0 {
			p.cursor = 0
		}
	case key.Matches(msg, keys.PgDn):
		p.cursor += 10
		if p.cursor > len(p.filtered)-1 {
			p.cursor = len(p.filtered) - 1
		}
		if p.cursor < 0 {
			p.cursor = 0
		}
	case key.Matches(msg, keys.Home) || (!p.filterMode && key.Matches(msg, keys.GTop)):
		p.cursor = 0
	case key.Matches(msg, keys.End) || (!p.filterMode && key.Matches(msg, keys.GBottom)):
		if len(p.filtered) > 0 {
			p.cursor = len(p.filtered) - 1
		}
	}
	return p, nil
}

// applyFilter rebuilds p.filtered honoring the current filter string.
// Pattern matches picker.go: clone matched results into a persistent
// buffer so the subsequent sort never mutates p.items.
func (p *ProfilePicker) applyFilter() {
	matched := filter.ContainsFold(p.items, p.filter, func(it profilePickerItem) string { return it.filterValue })
	p.filtered = append(p.filtered[:0], matched...)
	sort.Slice(p.filtered, func(i, j int) bool { return p.filtered[i].name < p.filtered[j].name })
	if p.cursor >= len(p.filtered) {
		p.cursor = 0
	}
}

// ActiveFilter exposes the current filter string for tests and hint
// rendering at the host page level.
func (p ProfilePicker) ActiveFilter() string { return p.filter }

// View renders the picker as a vertical stack of title, status, filter
// indicator, list, and hint line. Style matches the inline action menu
// in ModelsPage: '> ' prefix + theme.OK on the selected row, dim on
// the rest.
func (p ProfilePicker) View() string {
	title := theme.Title.Render("Pick a profile")
	hint := "[↑↓] move  [/] filter  [enter] select  [esc] cancel"
	if p.filterMode {
		hint = "[type] add  [backspace] del  [/] exit filter  [enter] select"
	}
	statusLine := theme.Subtitle.Render(fmt.Sprintf("%d profiles", len(p.items)))
	filterLine := ""
	if p.filterMode || p.filter != "" {
		filterLine = theme.Subtitle.Render(fmt.Sprintf("filter: %q", p.filter))
	}

	rows := make([]string, 0, len(p.filtered))
	for i, it := range p.filtered {
		label := it.name + " (" + it.id + ")"
		if it.backendID != "" {
			label += " [" + it.backendID + "]"
		}
		if len(it.tags) > 0 {
			label += " {" + strings.Join(it.tags, ",") + "}"
		}
		prefix := "  "
		if i == p.cursor {
			prefix = "> "
			label = theme.OK.Render(label)
		} else {
			label = theme.Subtitle.Render(label)
		}
		rows = append(rows, prefix+label)
	}
	body := strings.Join(rows, "\n")
	help := theme.Subtitle.Render(hint)
	parts := []string{title, statusLine, filterLine, body, help}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
