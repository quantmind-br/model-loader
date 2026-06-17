package pages

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/configweb"
)

func formatArgsBlock(args map[string]any) string {
	if len(args) == 0 {
		return "    (none)"
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString("\n    --")
		b.WriteString(k)
		if v := formatArgValue(args[k]); v != "" {
			b.WriteString(" ")
			b.WriteString(v)
		}
	}
	return b.String()
}

func formatArgValue(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (p ProfilesPage) togglePinSelected() (tea.Model, tea.Cmd) {
	if _, isCorrupt := p.list.SelectedItem().(corruptItem); isCorrupt {
		return p, nil
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok {
		return p, nil
	}
	pr := sel.p
	pr.Pinned = !pr.Pinned
	if err := p.store.Save(pr); err != nil {
		p, fc := p.withFlashError("pin failed: " + err.Error())
		return p, fc
	}
	// Optimistically update the selected item so the pin emoji appears
	// immediately, before the async reload completes.
	idx := p.list.Index()
	if idx >= 0 {
		p.list.SetItem(idx, item{p: pr})
	}
	return p, p.loadCmd()
}

func (p ProfilesPage) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	p.deleteConfirm, cmd = p.deleteConfirm.Update(msg)
	return p, cmd
}

// performDelete executes the actual store deletion in response to the
// profileDeleteConfirmedMsg emitted by deleteConfirm.onYes. Splitting it
// out keeps store I/O and flash mutation on the page rather than inside
// the Confirm callback closure.
func (p ProfilesPage) performDelete(id string) (tea.Model, tea.Cmd) {
	var fc tea.Cmd
	if err := p.store.Delete(id); err != nil {
		p, fc = p.withFlashError("delete failed: " + err.Error())
	} else {
		p, fc = p.withFlash("deleted " + id)
	}
	return p, tea.Batch(p.loadCmd(), fc)
}

func (p ProfilesPage) startNew() (tea.Model, tea.Cmd) {
	d := configweb.Draft{
		IsNew:     true,
		Name:      "New Profile",
		ID:        domain.Slugify("New Profile"),
		Args:      map[string]string{},
		ExtraArgs: []string{},
	}
	if p.catalogStore != nil {
		if catalog, err := p.catalogStore.Load(); err == nil && catalog.DefaultBackendID != "" {
			d.BackendID = catalog.DefaultBackendID
		}
	}
	p.webEditing = true
	return p, p.startWebEdit(d)
}

func (p ProfilesPage) startEditSelected() (tea.Model, tea.Cmd) {
	if _, isCorrupt := p.list.SelectedItem().(corruptItem); isCorrupt {
		p, fc := p.withFlashError("selected entry is corrupt — delete it (X) or fix the JSON file")
		return p, fc
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok {
		return p, nil
	}
	pr := sel.p
	d := configweb.Draft{
		ID:          pr.ID,
		OrigID:      pr.ID,
		Name:        pr.Name,
		Description: pr.Description,
		Tags:        pr.Tags,
		Model:       pr.Model,
		BackendID:   pr.Launch.BackendID,
		Args:        argsToStrings(pr.Args),
		Env:         pr.Launch.Env,
	}
	if d.Tags == nil {
		d.Tags = []string{}
	}
	if d.Args == nil {
		d.Args = map[string]string{}
	}
	p.webEditing = true
	return p, p.startWebEdit(d)
}

// argsToStrings converts a map[string]any args map to map[string]string
// using formatArgValue for each value.
func argsToStrings(args map[string]any) map[string]string {
	out := make(map[string]string, len(args))
	for k, v := range args {
		out[k] = formatArgValue(v)
	}
	return out
}

func (p ProfilesPage) duplicateSelected() (tea.Model, tea.Cmd) {
	if _, isCorrupt := p.list.SelectedItem().(corruptItem); isCorrupt {
		p, fc := p.withFlashError("selected entry is corrupt — delete it (X) or fix the JSON file")
		return p, fc
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok {
		return p, nil
	}
	newID := sel.p.ID + "-copy"
	if _, err := p.store.Duplicate(sel.p.ID, newID); err != nil {
		p, fc := p.withFlashError("duplicate failed: " + err.Error())
		return p, fc
	}
	p, fc := p.withFlash("duplicated as " + newID)
	return p, tea.Batch(p.loadCmd(), fc)
}

func (p ProfilesPage) askDeleteSelected() (tea.Model, tea.Cmd) {
	var id string
	switch sel := p.list.SelectedItem().(type) {
	case item:
		id = sel.p.ID
	case corruptItem:
		id = sel.id
	default:
		return p, nil
	}
	var cmd tea.Cmd
	p.deleteConfirm, cmd = setupConfirm("Delete profile "+id+"?", "Delete", "Cancel",
		func() tea.Cmd { return func() tea.Msg { return profileDeleteConfirmedMsg{id: id} } })
	return p, cmd
}
