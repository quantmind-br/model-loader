package pages

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
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

	var pr domain.Profile
	if !d.IsNew {
		if existing, err := p.store.Get(d.ID); err == nil {
			pr = d.ApplyToWithSchema(existing, schema)
			pr.Meta = existing.Meta
		} else {
			pr = d.ToProfileWithSchema(schema)
		}
	} else {
		pr = d.ToProfileWithSchema(schema)
	}

	var fc tea.Cmd
	if err := p.store.Save(pr); err != nil {
		p, fc = p.withFlashError("save failed: " + err.Error())
	} else {
		p, fc = p.withFlash("saved " + pr.ID)
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
		Name:           "New Profile",
		Tags:           "",
		NGL:            "99",
		CtxSize:        "8192",
		BatchSize:      "2048",
		UBatchSize:     "512",
		Port:           "4321",
		FlashAttn:      "auto",
		CacheTypeK:     "q8_0",
		CacheTypeV:     "q8_0",
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
		SetSchemaStore(p.schemaStore)
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
		Name:           pr.Name,
		Description:    pr.Description,
		Tags:           profile_editor.FormatTags(pr.Tags),
		Model:          pr.Model,
		BackendID:      pr.Launch.BackendID,
		NGL:            profile_editor.ArgString(pr.Args["ngl"]),
		CtxSize:        profile_editor.ArgString(pr.Args["ctx-size"]),
		BatchSize:      profile_editor.ArgString(pr.Args["batch-size"]),
		UBatchSize:     profile_editor.ArgString(pr.Args["ubatch-size"]),
		Port:           profile_editor.ArgString(pr.Args["port"]),
		FlashAttn:      profile_editor.FlashAttnToString(pr.Args["flash-attn"]),
		CacheTypeK:     profile_editor.ArgString(pr.Args["cache-type-k"]),
		CacheTypeV:     profile_editor.ArgString(pr.Args["cache-type-v"]),
		Env:            append([]domain.EnvVar(nil), pr.Launch.Env...),
		RestartPolicy:  string(pr.Launch.RestartPolicy),
		MaxRestarts:    strconv.Itoa(pr.Launch.MaxRestarts),
		BackoffSeconds: strconv.Itoa(pr.Launch.BackoffSeconds),
	}
	// Copy remaining args not mapped to hardcoded Essentials fields into
	// the generic Args map so the Advanced tab can edit them.
	d.Args = map[string]any{}
	for k, v := range pr.Args {
		switch k {
		case "ngl", "ctx-size", "batch-size", "ubatch-size", "port", "flash-attn", "cache-type-k", "cache-type-v":
			continue
		}
		d.Args[k] = v
	}
	p.editor = p.prepareEditor()
	var cmd tea.Cmd
	p.editor, cmd = p.editor.Open(d)
	return p, cmd
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
