package pages

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/pages/profile_editor"
)

// resolveSchemaForBackendID looks up the schema for a backend ID through
// the catalog and schema stores. Returns an empty schema on any error.
func (p ProfilesPage) resolveSchemaForBackendID(backendID string) domain.FlagSchema {
	if p.catalogStore == nil || p.schemaStore == nil {
		return domain.FlagSchema{}
	}
	catalog, err := p.catalogStore.Load()
	if err != nil {
		return domain.FlagSchema{}
	}
	if backendID == "" {
		backendID = catalog.DefaultBackendID
	}
	var backend domain.Backend
	for _, b := range catalog.Backends {
		if b.ID == backendID {
			backend = b
			break
		}
	}
	if backend.ID == "" {
		return domain.FlagSchema{}
	}
	schema, err := p.schemaStore.Load(backendcatalog.SchemaStoreRef(backend.SchemaRef))
	if err != nil {
		return domain.FlagSchema{}
	}
	return schema.ToFlagSchema()
}

// handleEditorCommitted persists the saved Draft via the store, refreshes
// the list, and surfaces success/failure as a flash.
func (p ProfilesPage) handleEditorCommitted(msg profile_editor.EditorCommittedMsg) (tea.Model, tea.Cmd) {
	d := msg.Draft
	if d.ID == "" {
		d.ID = domain.Slugify(d.Name)
	}
	schema := p.resolveSchemaForBackendID(d.BackendID)

	renaming := !d.IsNew && d.OrigID != "" && d.ID != d.OrigID
	if renaming {
		for _, ri := range p.running {
			if ri.ProfileID == d.OrigID {
				p, fc := p.withFlashError("stop the running instance before renaming")
				return p, fc
			}
		}
	}

	var pr domain.Profile
	lookupID := d.ID
	if renaming {
		lookupID = d.OrigID
	}
	if !d.IsNew {
		if existing, err := p.store.Get(lookupID); err == nil {
			pr = d.ApplyToWithSchema(existing, schema)
			pr.Meta = existing.Meta
		} else {
			pr = d.ToProfileWithSchema(schema)
		}
	} else {
		pr = d.ToProfileWithSchema(schema)
	}

	var fc tea.Cmd
	switch {
	case renaming:
		// Rename writes the final profile under the new id before removing the
		// old file, so a failure leaves the original intact (no lost edits).
		if err := p.store.Rename(d.OrigID, pr); err != nil {
			p, fc = p.withFlashError("rename failed: " + err.Error())
		} else {
			p, fc = p.withFlash("saved " + pr.ID)
		}
	case d.IsNew:
		// Create is exclusive: it refuses to overwrite a profile that claimed
		// this id after the editor opened (the field validator's snapshot is
		// stale by submit time), surfacing ErrDuplicateID instead of clobbering.
		if err := p.store.Create(pr); err != nil {
			if errors.Is(err, profilestore.ErrDuplicateID) {
				p, fc = p.withFlashError("id already exists: " + pr.ID)
			} else {
				p, fc = p.withFlashError("save failed: " + err.Error())
			}
		} else {
			p, fc = p.withFlash("saved " + pr.ID)
		}
	default:
		if err := p.store.Save(pr); err != nil {
			p, fc = p.withFlashError("save failed: " + err.Error())
		} else {
			p, fc = p.withFlash("saved " + pr.ID)
		}
	}
	return p, tea.Batch(p.loadCmd(), fc)
}

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

// newDraftDefaults builds a fresh Draft pre-seeded with sensible defaults
// for new profiles. Shared by [n] (start new) and "use in new profile".
func (p ProfilesPage) newDraftDefaults() profile_editor.Draft {
	d := profile_editor.Draft{
		ID:             domain.Slugify("New Profile"),
		Name:           "New Profile",
		Tags:           "",
		RestartPolicy:  string(domain.RestartPolicyNone),
		MaxRestarts:    "3",
		BackoffSeconds: "5",
		IsNew:          true,
		Args:           map[string]any{},
	}
	if p.catalogStore != nil {
		if catalog, err := p.catalogStore.Load(); err == nil && catalog.DefaultBackendID != "" {
			d.BackendID = catalog.DefaultBackendID
		}
	}
	return d
}

func (p ProfilesPage) backendOptions() []huh.Option[string] {
	if p.catalogStore == nil {
		return nil
	}
	catalog, err := p.catalogStore.Load()
	if err != nil {
		return nil
	}
	opts := make([]huh.Option[string], 0, len(catalog.Backends))
	for _, b := range catalog.Backends {
		opts = append(opts, huh.NewOption(b.Name, b.ID))
	}
	return opts
}

func (p ProfilesPage) prepareEditor() profile_editor.Editor {
	return p.editor.
		SetBackendOptions(p.backendOptions()).
		SetCatalogStore(p.catalogStore).
		SetSchemaStore(p.schemaStore).
		SetExistingIDs(p.existingIDs())
}

// existingIDs lists every profile id currently in the store so the editor can
// reject id collisions. I/O errors degrade to an empty list (best-effort).
func (p ProfilesPage) existingIDs() []string {
	profiles, err := p.store.List()
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(profiles))
	for _, pr := range profiles {
		ids = append(ids, pr.ID)
	}
	return ids
}

func (p ProfilesPage) startNew() (tea.Model, tea.Cmd) {
	p.editor = p.prepareEditor()
	var cmd tea.Cmd
	p.editor, cmd = p.editor.Open(p.newDraftDefaults())
	return p, cmd
}

func (p ProfilesPage) startEditSelected() (tea.Model, tea.Cmd) {
	if _, isCorrupt := p.list.SelectedItem().(corruptItem); isCorrupt {
		p, fc := p.withFlashError("selected entry is corrupt — delete it (x) or fix the JSON file")
		return p, fc
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok {
		return p, nil
	}
	pr := sel.p
	d := profile_editor.Draft{
		ID:             pr.ID,
		OrigID:         pr.ID,
		Name:           pr.Name,
		Description:    pr.Description,
		Tags:           profile_editor.FormatTags(pr.Tags),
		Model:          pr.Model,
		BackendID:      pr.Launch.BackendID,
		Env:            append([]domain.EnvVar(nil), pr.Launch.Env...),
		RestartPolicy:  string(pr.Launch.RestartPolicy),
		MaxRestarts:    strconv.Itoa(pr.Launch.MaxRestarts),
		BackoffSeconds: strconv.Itoa(pr.Launch.BackoffSeconds),
		Args:           copyArgs(pr.Args),
	}
	p.editor = p.prepareEditor()
	var cmd tea.Cmd
	p.editor, cmd = p.editor.Open(d)
	return p, cmd
}

func copyArgs(args map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range args {
		out[k] = v
	}
	return out
}

func (p ProfilesPage) duplicateSelected() (tea.Model, tea.Cmd) {
	if _, isCorrupt := p.list.SelectedItem().(corruptItem); isCorrupt {
		p, fc := p.withFlashError("selected entry is corrupt — delete it (x) or fix the JSON file")
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
	p.deleteConfirm = components.NewConfirm(
		"Delete profile "+id+"?",
		id,
		func(payload any) tea.Cmd {
			pid, _ := payload.(string)
			return func() tea.Msg { return profileDeleteConfirmedMsg{id: pid} }
		},
		"Delete",
		"Cancel",
	)
	return p, p.deleteConfirm.Init()
}
