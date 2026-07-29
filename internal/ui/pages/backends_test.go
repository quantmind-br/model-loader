package pages

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
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

	if got := p.Hints(); !strings.Contains(got, "[e] edit") || !strings.Contains(got, "[R] refresh") {
		t.Fatalf("list hints = %q", got)
	}
	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	p = model.(BackendsPage)
	if got := p.Hints(); got != "editing in browser…  [esc] cancel" {
		t.Fatalf("web edit hints = %q", got)
	}
	p.webEditing = false
	model, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	p = model.(BackendsPage)
	if got := p.Hints(); got != "[←→] choose  [enter] confirm  [esc] cancel" {
		t.Fatalf("confirm hints = %q", got)
	}
}

func TestBackendsPage_IsCapturingInputDuringWebEditAndConfirm(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	addBackendForPage(t, mgr, "Capture Backend", "/bin/echo")
	p = loadBackendsPage(t, p)

	if p.IsCapturingInput() {
		t.Fatal("list mode should not capture input")
	}
	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	p = model.(BackendsPage)
	if !p.IsCapturingInput() {
		t.Fatal("web edit mode should capture input")
	}
	p.webEditing = false
	model, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	p = model.(BackendsPage)
	if !p.IsCapturingInput() {
		t.Fatal("confirm mode should capture input")
	}
}

func TestBackendsPage_AddBackendViaWebEdit(t *testing.T) {
	p, _, _ := newBackendsPageHarness(t)
	p = loadBackendsPage(t, p)
	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	p = model.(BackendsPage)

	if !p.webEditing {
		t.Fatal("expected webEditing after startAdd")
	}

	model, _ = p.Update(backendWebEditDoneMsg{saved: true, backendID: "added-backend"})
	p = model.(BackendsPage)
	if p.webEditing {
		t.Fatal("expected webEditing=false after done")
	}
}

func TestBackendsPage_EditBackendViaWebEdit(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	b := addBackendForPage(t, mgr, "Edit Backend", "/bin/echo")
	_, _ = mgr.UpdateBackend(b.ID, domain.Backend{Name: b.Name, Executable: b.Executable, Description: "old desc"})
	p = loadBackendsPage(t, p)
	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	p = model.(BackendsPage)

	if !p.webEditing {
		t.Fatal("expected webEditing after startEditSelected")
	}

	_, _ = mgr.UpdateBackend(b.ID, domain.Backend{Name: "Edited Backend", Executable: "/bin/cat", Description: "edited desc", Tags: []string{"stable"}})
	model, _ = p.Update(backendWebEditDoneMsg{saved: true, backendID: b.ID})
	p = model.(BackendsPage)
	if p.webEditing {
		t.Fatal("expected webEditing=false after done")
	}

	got, err := mgr.GetBackend(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Edited Backend" || got.Executable != "/bin/cat" || got.Description != "edited desc" || strings.Join(got.Tags, ",") != "stable" {
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
	// Reload goes through backendsReloadMsg (so Update can clear stale probe
	// results) and only then chains into loadCmd.
	msg := cmd()
	if _, ok := msg.(backendsReloadMsg); !ok {
		t.Fatalf("Reload cmd produced %T, want backendsReloadMsg", msg)
	}
	model, loadCmd := p.Update(msg)
	p = model.(BackendsPage)
	if loadCmd == nil {
		t.Fatal("handling backendsReloadMsg returned nil cmd")
	}
	if _, ok := loadCmd().(backendsLoadedMsg); !ok {
		t.Fatal("reload did not chain into backendsLoadedMsg")
	}
}

func TestBackendsPage_IgnoresNonKeyDuringWebEdit(t *testing.T) {
	p, _, _ := newBackendsPageHarness(t)
	p = loadBackendsPage(t, p)
	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	p = model.(BackendsPage)

	model, _ = p.Update(list.FilterMatchesMsg{})
	p = model.(BackendsPage)
	if !p.webEditing {
		t.Fatal("webEditing should stay true during non-key msg")
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
	cmd = firstProbeEventCmd(cmd)

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

// ctxCapturingProber records the ctx passed to Probe and returns a channel it
// never closes, so the consumer stays pending until the timeout fires.
type ctxCapturingProber struct{ ctx context.Context }

func (c *ctxCapturingProber) Probe(ctx context.Context) (<-chan backendcatalog.ProbeEvent, error) {
	c.ctx = ctx
	return make(chan backendcatalog.ProbeEvent), nil
}

// TestBackendsPage_ProbeTimeoutCancelsContext guards audit N-C14: the probe
// timeout branch cancels the ctx so the prober's producer goroutine unblocks.
func TestBackendsPage_ProbeTimeoutCancelsContext(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	addBackendForPage(t, mgr, "Probe Backend", "/bin/echo")
	p = loadBackendsPage(t, p)

	prober := &ctxCapturingProber{}
	p = p.WithProber(prober)

	model, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'P'}})
	p = model.(BackendsPage)
	if prober.ctx == nil {
		t.Fatal("prober ctx not captured")
	}

	// Force the timeout branch: pretend the probe started 4s ago.
	p.probeStartTime = time.Now().Add(-4 * time.Second)
	model, _ = p.Update(spinner.TickMsg{})
	p = model.(BackendsPage)
	if p.pendingProbe {
		t.Fatal("expected pendingProbe false after timeout")
	}

	select {
	case <-prober.ctx.Done():
	case <-time.After(1 * time.Second):
		t.Fatal("probe context not canceled on timeout")
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
	cmd = firstProbeEventCmd(cmd)
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
	cmd = firstProbeEventCmd(cmd)
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
	cmd = firstProbeEventCmd(cmd)

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

// firstProbeEventCmd unwraps askProbeAll's Batch(readNext, spinnerTick) into a
// single cmd yielding the next probeEventMsg, dropping the spinner tick (armed
// per audit N-P5) so the probe-driving test loops work unchanged.
func firstProbeEventCmd(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return func() tea.Msg { return msg }
	}
	for _, c := range batch {
		if pe, ok := c().(probeEventMsg); ok {
			return func() tea.Msg { return pe }
		}
	}
	return nil
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

// UIUX-025: probe results reflect a point in time. When the tab is reloaded
// (root activates it again), stale probe lines must be cleared so old health
// data is not presented as current.
func TestBackendsPage_ReloadClearsStaleProbeResults(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	b := addBackendForPage(t, mgr, "Probe Backend", "/bin/echo")
	p = loadBackendsPage(t, p)

	p.probeResults = map[string]backendProbeResult{
		b.ID: {status: backendcatalog.ProbeStatusOK, detail: "v1.0.0"},
	}
	if !strings.Contains(p.detailView(120), "Probe:") {
		t.Fatalf("detail view missing probe line before reload:\n%s", p.detailView(120))
	}

	cmd := p.Reload()
	if cmd == nil {
		t.Fatal("Reload returned nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(backendsReloadMsg); !ok {
		t.Fatalf("Reload cmd produced %T, want backendsReloadMsg", msg)
	}

	model, loadCmd := p.Update(msg)
	p = model.(BackendsPage)
	if len(p.probeResults) != 0 {
		t.Fatalf("probeResults = %d entries after reload, want 0", len(p.probeResults))
	}
	if strings.Contains(p.detailView(120), "Probe:") {
		t.Fatalf("detail view still shows probe line after reload:\n%s", p.detailView(120))
	}
	if loadCmd == nil {
		t.Fatal("handling backendsReloadMsg returned nil cmd; expected loadCmd")
	}
	if _, ok := loadCmd().(backendsLoadedMsg); !ok {
		t.Fatal("reload did not chain into loadCmd")
	}
}

// TUI-RESP: Backends responsive stacked layout and truncation.
func TestBackendsPage_ResponsiveLayout(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	b := addBackendForPage(t, mgr, "long", strings.Repeat("x", 100))
	p = loadBackendsPage(t, p)
	for i, it := range p.list.Items() {
		if it.(backendItem).backend.ID == b.ID {
			p.list.Select(i)
			break
		}
	}
	updated, _ := p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	p = updated.(BackendsPage)
	if mode, _, _ := theme.ResponsiveSplit(80); mode != theme.LayoutStacked {
		t.Fatalf("want stacked at 80 cols")
	}
	for _, line := range strings.Split(p.View(), "\n") {
		if line == "" {
			continue
		}
		if lipgloss.Width(line) > 80 {
			t.Fatalf("line > 80 cols: %q", line)
		}
	}
	updated, _ = p.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	p = updated.(BackendsPage)
	detail := p.detailView(47)
	if !strings.Contains(detail, "…") {
		t.Fatalf("executable should truncate at narrow width; got:\n%s", detail)
	}
}

// UIUX-038: while bubbles/list owns the filter input every page key is
// swallowed, so the footer must drop the list actions.
func TestBackendsPage_HintsDuringFilter(t *testing.T) {
	p, mgr, _ := newBackendsPageHarness(t)
	addBackendForPage(t, mgr, "Llama Main", "/bin/echo")
	p = loadBackendsPage(t, p)

	updated, _ := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	p = updated.(BackendsPage)
	if p.list.FilterState() != list.Filtering {
		t.Fatalf("'/' did not enter filter mode; state = %v", p.list.FilterState())
	}

	got := p.Hints()
	if got != filteringHints {
		t.Fatalf("Hints() = %q, want %q", got, filteringHints)
	}
	if strings.Contains(got, "[R] refresh") {
		t.Fatalf("filter-mode hints leak the default list tail: %q", got)
	}
}
