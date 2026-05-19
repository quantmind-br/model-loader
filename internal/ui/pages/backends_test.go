package pages

import (
	"context"
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
	if !strings.Contains(out, "No backends yet.") || !strings.Contains(out, "Press [n] to add one") {
		t.Fatalf("empty state missing; got:\n%s", out)
	}
}

func TestBackendsPage_EmptyStateWhenNoDefault(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	addBackendForPage(t, mgr, "Llama Main", "/bin/echo")
	p = loadBackendsPage(t, p)
	p.defaultBackendID = ""

	out := p.View()
	if !strings.Contains(out, "No default backend set") {
		t.Errorf("no-default Backends view missing hint; got:\n%s", out)
	}
	if !strings.Contains(out, "Press [D] to set a backend as default") {
		t.Errorf("no-default Backends view missing action hint; got:\n%s", out)
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
	if !strings.Contains(p.flash.Message(), "deleted") {
		t.Fatalf("flash = %q", p.flash.Message())
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
	if !strings.Contains(p.flash.Message(), "default") {
		t.Fatalf("flash = %q", p.flash.Message())
	}
}

func TestBackendsPage_RefreshSchema(t *testing.T) {
	p, mgr, gen := newBackendsPageHarness(t)
	b := addBackendForPage(t, mgr, "Refresh Backend", "/bin/echo")
	p = loadBackendsPage(t, p)
	before := gen.calls
	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	p = model.(BackendsPage)

	if !p.refreshConfirm.Active() {
		t.Fatal("expected refresh confirmation to be active")
	}
	if gen.calls != before {
		t.Fatalf("generator calls = %d, want %d (no refresh yet)", gen.calls, before)
	}

	model, _ = p.Update(backendRefreshConfirmedMsg{id: b.ID})
	p = model.(BackendsPage)

	if gen.calls != before+1 {
		t.Fatalf("generator calls = %d, want %d", gen.calls, before+1)
	}
	if !strings.Contains(p.flash.Message(), "schema refreshed") {
		t.Fatalf("flash = %q", p.flash.Message())
	}
	if p.refreshConfirm.Active() {
		t.Fatal("expected refresh confirmation to be cleared")
	}
}

func TestBackendsPage_RefreshSchemaCancel(t *testing.T) {
	p, mgr, gen := newBackendsPageHarness(t)
	addBackendForPage(t, mgr, "Refresh Backend", "/bin/echo")
	p = loadBackendsPage(t, p)
	before := gen.calls
	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	p = model.(BackendsPage)

	if !p.refreshConfirm.Active() {
		t.Fatal("expected refresh confirmation to be active")
	}

	model, _ = p.Update(tea.KeyMsg{Type: tea.KeyEscape})
	p = model.(BackendsPage)

	if gen.calls != before {
		t.Fatalf("generator calls = %d, want %d (no refresh after cancel)", gen.calls, before)
	}
	if p.refreshConfirm.Active() {
		t.Fatal("expected refresh confirmation to be cleared after cancel")
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

type fakeProber struct {
	events []backendcatalog.ProbeEvent
}

func (f *fakeProber) Probe(ctx context.Context) (<-chan backendcatalog.ProbeEvent, error) {
	ch := make(chan backendcatalog.ProbeEvent, len(f.events)+1)
	for _, ev := range f.events {
		ch <- ev
	}
	ch <- backendcatalog.ProbeEvent{Done: true}
	close(ch)
	return ch, nil
}

func TestBackendsPage_Probe(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	b := addBackendForPage(t, mgr, "Probe Backend", "/bin/echo")
	p = loadBackendsPage(t, p)

	prober := &fakeProber{
		events: []backendcatalog.ProbeEvent{
			{BackendID: b.ID, Status: backendcatalog.ProbeStatusOK, Detail: "v1.0.0"},
		},
	}
	p = p.WithProber(prober)

	model, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	p = model.(BackendsPage)

	for cmd != nil {
		msg := cmd()
		model, cmd = p.Update(msg)
		p = model.(BackendsPage)
		if m, ok := msg.(probeEventMsg); ok && m.event.Done {
			break
		}
	}

	if p.prober == nil {
		t.Fatal("prober not wired")
	}
	if len(p.probeResults) != 1 {
		t.Fatalf("probe results = %d, want 1", len(p.probeResults))
	}
	if p.probeResults[b.ID].status != backendcatalog.ProbeStatusOK {
		t.Fatalf("status = %s, want OK", p.probeResults[b.ID].status)
	}
}

func TestBackendsPage_RefreshPendingGuard(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	addBackendForPage(t, mgr, "Refresh Backend", "/bin/echo")
	p = loadBackendsPage(t, p)

	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	p = model.(BackendsPage)

	if !p.pendingRefresh {
		t.Fatal("expected pendingRefresh true after first R")
	}
	if !p.refreshConfirm.Active() {
		t.Fatal("expected refresh confirmation active")
	}

	model, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	p = model.(BackendsPage)

	if !p.refreshConfirm.Active() {
		t.Fatal("expected refresh confirmation still active")
	}
	if cmd != nil {
		t.Fatal("expected no command while refresh pending")
	}
	if !p.pendingRefresh {
		t.Fatal("expected pendingRefresh still true")
	}
}

func TestBackendsPage_ProbePendingGuard(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	b := addBackendForPage(t, mgr, "Probe Backend", "/bin/echo")
	p = loadBackendsPage(t, p)

	prober := &fakeProber{
		events: []backendcatalog.ProbeEvent{
			{BackendID: b.ID, Status: backendcatalog.ProbeStatusOK, Detail: "v1.0.0"},
		},
	}
	p = p.WithProber(prober)

	model, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	p = model.(BackendsPage)
	oldEpoch := p.probeEpoch

	if !p.pendingProbe {
		t.Fatal("expected pendingProbe true after first P")
	}

	// Process the first event so readNextProbeEvent is queued
	msg := cmd()
	model, cmd = p.Update(msg)
	p = model.(BackendsPage)

	// Press P again while probe is still in flight
	model, cmd2 := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	p = model.(BackendsPage)

	if p.probeEpoch != oldEpoch {
		t.Fatal("expected probe epoch unchanged while pending")
	}
	if cmd2 != nil {
		t.Fatal("expected no command while probe pending")
	}
	if !p.pendingProbe {
		t.Fatal("expected pendingProbe still true")
	}

	// Drain remaining events to completion
	for cmd != nil {
		msg := cmd()
		model, cmd = p.Update(msg)
		p = model.(BackendsPage)
		if m, ok := msg.(probeEventMsg); ok && m.event.Done {
			break
		}
	}

	if p.pendingProbe {
		t.Fatal("expected pendingProbe false after probe complete")
	}
}

func TestBackendsPage_ProbeStaleEpochIgnored(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	b := addBackendForPage(t, mgr, "Probe Backend", "/bin/echo")
	p = loadBackendsPage(t, p)

	prober := &fakeProber{
		events: []backendcatalog.ProbeEvent{
			{BackendID: b.ID, Status: backendcatalog.ProbeStatusOK, Detail: "v1"},
		},
	}
	p = p.WithProber(prober)

	model, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	p = model.(BackendsPage)
	oldEpoch := p.probeEpoch

	for cmd != nil {
		msg := cmd()
		model, cmd = p.Update(msg)
		p = model.(BackendsPage)
		if m, ok := msg.(probeEventMsg); ok && m.event.Done {
			break
		}
	}

	model, cmd = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	p = model.(BackendsPage)
	if p.probeEpoch == oldEpoch {
		t.Fatal("expected new probe epoch")
	}

	for cmd != nil {
		msg := cmd()
		model, cmd = p.Update(msg)
		p = model.(BackendsPage)
		if m, ok := msg.(probeEventMsg); ok && m.event.Done {
			break
		}
	}

	stale := probeEventMsg{
		event: backendcatalog.ProbeEvent{BackendID: b.ID, Status: backendcatalog.ProbeStatusErr},
		epoch: oldEpoch,
	}
	model, _ = p.Update(stale)
	p = model.(BackendsPage)
	if p.probeResults[b.ID].status != backendcatalog.ProbeStatusOK {
		t.Fatal("stale event should not overwrite current results")
	}
}

// drainBackendsCmd settles internal Cmd→Msg chains (bubbles list
// filterItems) so the page assertions can observe the post-filter state
// without spinning a tea.Program.
func drainBackendsCmd(t *testing.T, p *BackendsPage, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil && i < 16; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				drainBackendsCmd(t, p, c)
			}
			return
		}
		upd, next := p.Update(msg)
		*p = upd.(BackendsPage)
		cmd = next
	}
}

// F-07 regression: pressing `/` on the Backends tab must enter the
// bubbles list filter mode and IsCapturingInput must report true so
// global shortcuts stop stealing keystrokes.
func TestBackendsPage_SlashEntersFilterMode(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	addBackendForPage(t, mgr, "Alpha Backend", "/bin/echo")
	p = loadBackendsPage(t, p)

	if p.IsCapturingInput() {
		t.Fatal("expected not capturing input before filter starts")
	}
	model, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	p = model.(BackendsPage)
	drainBackendsCmd(t, &p, cmd)
	if p.list.FilterState() != list.Filtering {
		t.Fatalf("filterState=%v after '/'; want Filtering", p.list.FilterState())
	}
	if !p.IsCapturingInput() {
		t.Fatalf("expected capturing input after '/'; filterState=%v", p.list.FilterState())
	}
}

// F-07 regression: characters typed while filtering must reach the list
// (narrowing visible items) — they must NOT trigger page shortcuts like
// `n` (new), `e` (edit), `x` (delete).
func TestBackendsPage_FilterNarrowsList(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	addBackendForPage(t, mgr, "Alpha", "/bin/a")
	addBackendForPage(t, mgr, "Beta", "/bin/b")
	p = loadBackendsPage(t, p)

	send := func(p BackendsPage, msg tea.Msg) BackendsPage {
		upd, cmd := p.Update(msg)
		out := upd.(BackendsPage)
		drainBackendsCmd(t, &out, cmd)
		return out
	}

	p = send(p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "alph" {
		p = send(p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	p = send(p, tea.KeyMsg{Type: tea.KeyEnter})

	visible := p.list.VisibleItems()
	if len(visible) != 1 {
		t.Fatalf("VisibleItems=%d after typing 'alph'; want 1", len(visible))
	}
	if bi, ok := visible[0].(backendItem); !ok || bi.backend.Name != "Alpha" {
		t.Fatalf("VisibleItems[0]=%+v; want Alpha", visible[0])
	}
}
