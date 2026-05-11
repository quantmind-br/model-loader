package pages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
)

type pageFakeGenerator struct {
	calls int
	store backendcatalog.SchemaStore
}

func (g *pageFakeGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	g.calls++
	schema := domain.BackendValidationSchema{
		SchemaVersion: 1,
		Kind:          domain.ValidationSchemaCLIFlagsV1,
		BackendKind:   backend.Kind,
		BackendID:     backend.ID,
		Flags:         map[string]domain.FlagSpec{},
	}
	ref := strings.TrimPrefix(backend.SchemaRef, "schemas/")
	_ = g.store.Save(ref, schema)
	return schema, nil
}

func newBackendsPageHarness(t *testing.T) (BackendsPage, *backendschema.Manager, *pageFakeGenerator) {
	t.Helper()
	dir := t.TempDir()
	store := backendcatalog.NewFSStore(dir)
	schemaStore := backendcatalog.NewFSSchemaStore(dir)
	mgr := backendschema.NewManager(store, schemaStore)
	gen := &pageFakeGenerator{store: schemaStore}
	mgr.Register(domain.BackendKindLlamaServer, gen)
	return NewBackendsPage(mgr), mgr, gen
}

func loadBackendsPage(t *testing.T, p BackendsPage) BackendsPage {
	t.Helper()
	model, _ := p.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	p = model.(BackendsPage)
	msg := p.loadCmd()()
	model, _ = p.Update(msg)
	return model.(BackendsPage)
}

func addBackendForPage(t *testing.T, mgr *backendschema.Manager, name, exe string) domain.Backend {
	t.Helper()
	b, err := mgr.AddBackend(t.Context(), name, exe, domain.BackendKindLlamaServer)
	if err != nil {
		t.Fatalf("AddBackend: %v", err)
	}
	return b
}

func TestBackendsPage_LoadsBackends(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	addBackendForPage(t, mgr, "Llama Main", "/bin/echo")

	p = loadBackendsPage(t, p)

	if got := len(p.list.Items()); got != 1 {
		t.Fatalf("list items = %d, want 1", got)
	}
	if !strings.Contains(p.list.Items()[0].FilterValue(), "Llama Main") {
		t.Fatalf("loaded item = %q, want Llama Main", p.list.Items()[0].FilterValue())
	}
}

func TestBackendsPage_RendersEmptyState(t *testing.T) {
	p, _, _ := newBackendsPageHarness(t)
	p = loadBackendsPage(t, p)

	out := p.View()
	if !strings.Contains(out, "No backends yet. Press [n] to add one.") {
		t.Fatalf("empty state missing; got:\n%s", out)
	}
}

func TestBackendsPage_DetailShowsSelectedBackend(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	b := addBackendForPage(t, mgr, "Detail Backend", "/usr/bin/llama-server")
	_, err := mgr.UpdateBackend(b.ID, domain.Backend{Name: b.Name, Executable: b.Executable, Description: "GPU tuned", Tags: []string{"cuda", "fast"}})
	if err != nil {
		t.Fatal(err)
	}
	p = loadBackendsPage(t, p)

	out := p.View()
	for _, want := range []string{"Detail Backend", "default", b.ID, "llama-server", "/usr/bin/llama-server", b.SchemaRef, "GPU tuned", "cuda, fast", "Created:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("detail missing %q; got:\n%s", want, out)
		}
	}
}

func TestBackendsPage_HintsVaryByMode(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	addBackendForPage(t, mgr, "Hint Backend", "/bin/echo")
	p = loadBackendsPage(t, p)

	if got := p.Hints(); !strings.Contains(got, "[enter/e] edit") || !strings.Contains(got, "[R] refresh schema") {
		t.Fatalf("list hints = %q", got)
	}
	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	p = model.(BackendsPage)
	if got := p.Hints(); got != "[enter] submit  [esc] cancel" {
		t.Fatalf("form hints = %q", got)
	}
	p.form = nil
	model, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	p = model.(BackendsPage)
	if got := p.Hints(); got != "[←→] choose  [enter] confirm  [esc] cancel" {
		t.Fatalf("confirm hints = %q", got)
	}
}

func TestBackendsPage_IsCapturingInputDuringFormAndConfirm(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	addBackendForPage(t, mgr, "Capture Backend", "/bin/echo")
	p = loadBackendsPage(t, p)

	if p.IsCapturingInput() {
		t.Fatal("list mode should not capture input")
	}
	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	p = model.(BackendsPage)
	if !p.IsCapturingInput() {
		t.Fatal("form mode should capture input")
	}
	p.form = nil
	model, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	p = model.(BackendsPage)
	if !p.IsCapturingInput() {
		t.Fatal("confirm mode should capture input")
	}
}

func TestBackendsPage_AddBackendForm(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	p = loadBackendsPage(t, p)
	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	p = model.(BackendsPage)

	p.draft.Name = "Added Backend"
	p.draft.Kind = string(domain.BackendKindLlamaServer)
	p.draft.Executable = "/bin/echo"
	p.draft.Description = "new desc"
	p.draft.Tags = "alpha, beta"
	p.form.State = huh.StateCompleted
	model, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	p = model.(BackendsPage)
	if cmd != nil {
		_ = cmd()
	}

	b, err := mgr.GetBackend("added-backend")
	if err != nil {
		t.Fatalf("GetBackend: %v", err)
	}
	if b.Description != "new desc" || strings.Join(b.Tags, ",") != "alpha,beta" {
		t.Fatalf("backend metadata = %q %v", b.Description, b.Tags)
	}
}

func TestBackendsPage_EditBackendForm(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	b := addBackendForPage(t, mgr, "Edit Backend", "/bin/echo")
	p = loadBackendsPage(t, p)
	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	p = model.(BackendsPage)

	p.draft.Name = "Edited Backend"
	p.draft.Executable = "/bin/cat"
	p.draft.Description = "edited desc"
	p.draft.Tags = "stable"
	p.form.State = huh.StateCompleted
	model, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	p = model.(BackendsPage)
	if cmd != nil {
		_ = cmd()
	}

	got, err := mgr.GetBackend(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Edited Backend" || got.Executable != "/bin/cat" || got.Kind != domain.BackendKindLlamaServer || got.Description != "edited desc" {
		t.Fatalf("updated backend = %+v", got)
	}
}

func TestBackendsPage_DeleteBackendConfirm(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	b := addBackendForPage(t, mgr, "Delete Backend", "/bin/echo")
	p = loadBackendsPage(t, p)
	model, _ := p.Update(backendDeleteConfirmedMsg{id: b.ID})
	p = model.(BackendsPage)

	if _, err := mgr.GetBackend(b.ID); err == nil {
		t.Fatal("backend still exists after delete")
	}
	if !strings.Contains(p.flash, "deleted") {
		t.Fatalf("flash = %q", p.flash)
	}
}

func TestBackendsPage_SetDefaultBackend(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	addBackendForPage(t, mgr, "Backend One", "/bin/echo")
	b2 := addBackendForPage(t, mgr, "Backend Two", "/bin/cat")
	p = loadBackendsPage(t, p)
	p.list.Select(1)
	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	p = model.(BackendsPage)

	if p.defaultBackendID != b2.ID {
		t.Fatalf("defaultBackendID = %q, want %q", p.defaultBackendID, b2.ID)
	}
	if !strings.Contains(p.flash, "default") {
		t.Fatalf("flash = %q", p.flash)
	}
}

func TestBackendsPage_RefreshSchema(t *testing.T) {
	p, mgr, gen := newBackendsPageHarness(t)
	addBackendForPage(t, mgr, "Refresh Backend", "/bin/echo")
	p = loadBackendsPage(t, p)
	before := gen.calls
	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	p = model.(BackendsPage)

	if gen.calls != before+1 {
		t.Fatalf("generator calls = %d, want %d", gen.calls, before+1)
	}
	if !strings.Contains(p.flash, "schema refreshed") {
		t.Fatalf("flash = %q", p.flash)
	}
}

func TestBackendsPage_Reload(t *testing.T) {
	p, _, _ := newBackendsPageHarness(t)
	cmd := p.Reload()
	if cmd == nil {
		t.Fatal("Reload returned nil cmd")
	}
	if _, ok := cmd().(backendsLoadedMsg); !ok {
		t.Fatal("Reload cmd did not return backendsLoadedMsg")
	}
}

func TestBackendsPage_ForwardsNonKeyToActiveForm(t *testing.T) {
	p, _, _ := newBackendsPageHarness(t)
	p = loadBackendsPage(t, p)
	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	p = model.(BackendsPage)
	old := p.form

	model, _ = p.Update(list.FilterMatchesMsg{})
	p = model.(BackendsPage)
	if p.form == nil {
		t.Fatal("form closed after non-key msg")
	}
	if p.form != old {
		t.Fatal("form pointer changed unexpectedly")
	}
}
