// Package pages — list-item types + delegate for the profiles master list.
//
// Split out of profiles.go to keep that file's surface focused on the
// page's value-receiver Update/View dispatch. The types here have no
// dependency on ProfilesPage state and could be reused by any
// profile-list view.
package pages

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// profilesKeyMap groups the master-list / launcher key bindings used by
// ProfilesPage. Defined here so profiles.go stays focused on dispatch.
type profilesKeyMap struct {
	New, Save, Duplicate, Delete, Edit, Cancel, Tab, Launch, Kill, Refresh, Export, Pin, Import, Undo key.Binding
}

func defaultProfilesKeys() profilesKeyMap {
	return profilesKeyMap{
		New:       key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		Save:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "save")),
		Duplicate: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "dup")),
		// Destructive actions use uppercase + a confirm so they honor the
		// app-wide "uppercase = heavy/destructive" convention and free the
		// lowercase vim keys (k/j) for list navigation (KEY-01, CONSIST-02).
		Delete: key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "del")),
		// edit is lowercase (light) and shared with the Backends tab; export
		// moves to uppercase E, resolving the old e/E collision (CONSIST-01).
		Edit:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Cancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		Tab:    key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "tab editor")),
		Launch: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "launch")),
		Kill:   key.NewBinding(key.WithKeys("K"), key.WithHelp("K", "unload")),
		// refresh re-reads the store (heavier) → uppercase R, matching Models
		// and Backends rescan/refresh (CONSIST-01).
		Refresh: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh")),
		Export:  key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "export")),
		Pin:     key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pin")),
		Import:  key.NewBinding(key.WithKeys("I"), key.WithHelp("I", "import")),
		Undo:    key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "undo")),
	}
}

// item adapts domain.Profile to bubbles/list.
type item struct {
	p domain.Profile
}

func sortProfilesPinnedFirst(profiles []domain.Profile) {
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].Pinned != profiles[j].Pinned {
			return profiles[i].Pinned
		}
		return profiles[i].Meta.UpdatedAt.After(profiles[j].Meta.UpdatedAt)
	})
}

func (i item) Title() string {
	if i.p.Pinned {
		return "📌 " + i.p.Name
	}
	return i.p.Name
}
func (i item) Description() string {
	desc := i.p.ID
	if i.p.Launch.BackendID != "" {
		desc += " | backend: " + i.p.Launch.BackendID
	}
	return desc
}
func (i item) FilterValue() string {
	return i.p.Name + " " + i.p.ID + " " + strings.Join(i.p.Tags, " ")
}

// corruptItem is a list row representing a profile JSON entry that failed
// to parse. Edit/duplicate are no-ops; delete is allowed so the user can
// remove the bad file. Implements design § 8 — "marks entry with ⚠,
// excludes it from operations until the user fixes/deletes it".
type corruptItem struct {
	id  string
	err error
}

func (c corruptItem) Title() string       { return "⚠ " + c.id }
func (c corruptItem) Description() string { return "corrupt: " + c.err.Error() }
func (c corruptItem) FilterValue() string { return c.id }

// profileItemDelegate wraps list.DefaultDelegate to paint corruptItem rows
// in warn/error theme colors so they stand out from healthy entries. Healthy
// rows fall through to the default delegate's rendering.
type profileItemDelegate struct {
	list.DefaultDelegate
}

func newProfileItemDelegate() profileItemDelegate {
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	return profileItemDelegate{DefaultDelegate: d}
}

func (d profileItemDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	c, ok := item.(corruptItem)
	if !ok {
		d.DefaultDelegate.Render(w, m, index, item)
		return
	}
	title := theme.Error.Render("⚠ " + c.id)
	if index == m.Index() {
		// Mirror the default delegate's "selected" indent ("> ") to keep the
		// cursor position visible even on corrupt rows.
		fmt.Fprintf(w, "> %s", title)
		return
	}
	fmt.Fprintf(w, "  %s", title)
}
