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
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// profilesKeyMap groups the master-list / launcher key bindings used by
// ProfilesPage. Defined here so profiles.go stays focused on dispatch.
type profilesKeyMap struct {
	New, Save, Duplicate, Delete, Edit, Cancel, Tab, Launch, Export key.Binding
}

func defaultProfilesKeys() profilesKeyMap {
	return profilesKeyMap{
		New:       key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		Save:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "save")),
		Duplicate: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "dup")),
		Delete:    key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "del")),
		Edit:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit")),
		Cancel:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		Tab:       key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "tab editor")),
		Launch:    key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "launch")),
		Export:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "export")),
	}
}

// item adapts domain.Profile to bubbles/list.
type item struct {
	p domain.Profile
}

func (i item) Title() string { return i.p.Name }
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
// remove the bad file. Implementa design § 8 — "marca entry com ⚠,
// exclui de operações até user fix/delete".
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
	return profileItemDelegate{DefaultDelegate: list.NewDefaultDelegate()}
}

func (d profileItemDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	c, ok := item.(corruptItem)
	if !ok {
		d.DefaultDelegate.Render(w, m, index, item)
		return
	}
	title := theme.Error.Render("⚠ " + c.id)
	desc := theme.Warn.Render("corrupt: " + c.err.Error())
	if index == m.Index() {
		// Mirror the default delegate's "selected" indent ("> ") to keep the
		// cursor position visible even on corrupt rows.
		fmt.Fprintf(w, "> %s\n  %s", title, desc)
		return
	}
	fmt.Fprintf(w, "  %s\n  %s", title, desc)
}
