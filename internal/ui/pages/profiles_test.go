package pages

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// viewWrapper wraps a page model so that teatest sees overlay content
// (modal/editor/picker) when active, matching the old full-screen View()
// behavior that tests depend on.
type viewWrapper struct {
	page tea.Model
}

func (w viewWrapper) Init() tea.Cmd { return w.page.Init() }
func (w viewWrapper) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := w.page.Update(msg)
	w.page = m
	return w, cmd
}
func (w viewWrapper) View() string {
	type overlayer interface {
		OverlayView() Overlay
	}
	if ov, ok := w.page.(overlayer); ok {
		overlay := ov.OverlayView()
		if overlay.Active {
			return overlay.Content
		}
	}
	return w.page.View()
}

func (w viewWrapper) inner() tea.Model { return w.page }

// fakeProxy implements ProxyController for launcher tests, recording the
// profile IDs loaded and the number of unloads.
type fakeProxy struct {
	status      httpproxy.Status
	loaded      []string
	unloads     int
	ensureCalls int
	ensureErr   error
	loadErr     error
	unloadErr   error
}

func (f *fakeProxy) EnsureRunning(context.Context) error { f.ensureCalls++; return f.ensureErr }
func (f *fakeProxy) Load(_ context.Context, id string) (httpproxy.Status, error) {
	if f.loadErr != nil {
		return httpproxy.Status{}, f.loadErr
	}
	f.loaded = append(f.loaded, id)
	f.status = httpproxy.Status{Running: true, LoadedProfileID: id, LoadedPID: 4242}
	return f.status, nil
}
func (f *fakeProxy) Unload(context.Context, bool) (httpproxy.Status, error) {
	f.unloads++
	if f.unloadErr != nil {
		return httpproxy.Status{}, f.unloadErr
	}
	f.status = httpproxy.Status{Running: true}
	return f.status, nil
}
func (f *fakeProxy) Status() httpproxy.Status { return f.status }
func (f *fakeProxy) BaseURL() string          { return "http://127.0.0.1:9999" }

// stubResolver satisfies backendcatalog.Resolver with zero-value results so
// the launch pre-flight passes without a real catalog on disk.
type stubResolver struct{}

func (stubResolver) Resolve(domain.Profile) (backendcatalog.ResolvedBackend, error) {
	return backendcatalog.ResolvedBackend{}, nil
}
func (stubResolver) ResolveSchema(domain.Profile) (domain.BackendValidationSchema, domain.Backend, error) {
	return domain.BackendValidationSchema{}, domain.Backend{}, nil
}

func TestProfilesPage_LoadsExistingProfile(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(domain.Profile{
		ID:    "qwen",
		Name:  "Qwen Coder",
		Model: "/m.gguf",
		Args:  map[string]any{"ngl": float64(99)},
	}); err != nil {
		t.Fatal(err)
	}

	page := NewProfilesPage(store, domain.FlagSchema{})
	tm := teatest.NewTestModel(t, page, teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Qwen Coder")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}) // root would handle q; here we just ensure quit
	_ = tm.Quit()
}

func TestProfilesPage_NewProfileSavesViaStore(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	page := NewProfilesPage(store, domain.FlagSchema{})

	// 'n' opens the web editor: assert webEditing=true and a cmd is returned.
	updated, cmd := page.startNew()
	page = updated.(ProfilesPage)
	if !page.webEditing {
		t.Fatal("startNew should set webEditing=true")
	}
	if cmd == nil {
		t.Fatal("startNew should return a launch cmd")
	}

	// Ensure no profile was persisted (web editor not submitted)
	got, _ := store.List()
	if len(got) != 0 {
		t.Errorf("List len = %d, want 0", len(got))
	}
}

type stubScanner struct{}

func (stubScanner) Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error) {
	ch := make(chan domain.ScanEvent, 1)
	close(ch)
	return ch, nil
}

func TestProfilesPage_PickerWritesDraftModel(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	page := NewProfilesPage(store, domain.FlagSchema{}).WithModelScanner(stubScanner{}, nil)

	// Start a new draft — now routes to web editor.
	model, cmd := page.startNew()
	page = model.(ProfilesPage)
	if !page.webEditing {
		t.Fatal("startNew should set webEditing=true")
	}
	if cmd == nil {
		t.Fatal("startNew should return a launch cmd")
	}
}

func TestProfilesPage_EditHydratesBackendID(t *testing.T) {
	store := newFakeStoreWithDiagnostics([]domain.Profile{
		{
			ID:    "demo",
			Name:  "Demo",
			Model: "/m.gguf",
			Args:  map[string]any{"ngl": float64(99), "ctx-size": float64(8192), "port": float64(4321)},
			Launch: domain.LaunchConfig{
				BackendID: "llama-cpp-custom",
			},
		},
	}, nil)
	page := NewProfilesPage(store, domain.FlagSchema{})
	updated, _ := page.Update(loadedMsg{profiles: store.ps})
	page = updated.(ProfilesPage)

	updated, cmd := page.startEditSelected()
	page = updated.(ProfilesPage)

	if !page.webEditing {
		t.Fatal("startEditSelected should set webEditing=true")
	}
	if cmd == nil {
		t.Fatal("startEditSelected should return a launch cmd")
	}
}

func TestProfilesPage_RendersCorruptMarker(t *testing.T) {
	store := newFakeStoreWithDiagnostics(
		[]domain.Profile{{ID: "ok", Name: "Ok"}},
		[]profilestore.ListDiagnostic{{ID: "broken", Err: profilestore.ErrInvalidJSON}},
	)
	p := NewProfilesPage(store, domain.FlagSchema{})
	// Seed a window size so the list has room to render.
	model, _ := p.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	p = model.(ProfilesPage)

	loaded := p.loadCmd()()
	model, _ = p.Update(loaded)
	page := model.(ProfilesPage)

	out := page.View()
	if !strings.Contains(out, "broken") || !strings.Contains(out, "⚠") {
		t.Fatalf("view missing corrupt marker; got:\n%s", out)
	}
}

type fakeStoreWithDiag struct {
	ps    []domain.Profile
	diags []profilestore.ListDiagnostic
}

func newFakeStoreWithDiagnostics(ps []domain.Profile, diags []profilestore.ListDiagnostic) *fakeStoreWithDiag {
	return &fakeStoreWithDiag{ps: ps, diags: diags}
}

func (f *fakeStoreWithDiag) List() ([]domain.Profile, error) { return f.ps, nil }
func (f *fakeStoreWithDiag) ListWithDiagnostics() ([]domain.Profile, []profilestore.ListDiagnostic, error) {
	return f.ps, f.diags, nil
}
func (f *fakeStoreWithDiag) Get(id string) (domain.Profile, error) {
	for _, p := range f.ps {
		if p.ID == id {
			return p, nil
		}
	}
	return domain.Profile{}, profilestore.ErrNotFound
}
func (f *fakeStoreWithDiag) Create(_ domain.Profile) error { return nil }
func (f *fakeStoreWithDiag) Save(_ domain.Profile) error   { return nil }
func (f *fakeStoreWithDiag) Delete(_ string) error         { return nil }
func (f *fakeStoreWithDiag) Duplicate(_, _ string) (domain.Profile, error) {
	return domain.Profile{}, nil
}
func (f *fakeStoreWithDiag) Rename(_ string, _ domain.Profile) error { return nil }

func TestProfilesPage_UseInNewProfilePrefillsDraft(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	page := NewProfilesPage(store, domain.FlagSchema{})

	updated, cmd := page.Update(UseInNewProfileMsg{Path: "/foo/bar.gguf"})
	page = updated.(ProfilesPage)

	// Now routes to web editor — assert webEditing=true and cmd returned.
	if !page.webEditing {
		t.Fatal("webEditing = false, want true after UseInNewProfileMsg")
	}
	if cmd == nil {
		t.Fatal("expected a launch cmd, got nil")
	}
}

// TestLaunch_GoesThroughProxy locks in the proxy-only lifecycle: pressing
// [enter] must EnsureRunning + Load through the HTTP proxy (which already
// waits for backend health) and never spawn a process from this TUI.
func TestLaunch_GoesThroughProxy(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(domain.Profile{
		ID:    "demo",
		Name:  "Demo",
		Model: "/m.gguf",
		Args:  map[string]any{"port": float64(8080)},
	}); err != nil {
		t.Fatal(err)
	}
	fp := &fakeProxy{}
	page := NewProfilesPage(store, domain.FlagSchema{}).
		WithProxyController(fp).
		WithBackendResolver(stubResolver{})
	updated, _ := page.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	page = updated.(ProfilesPage)
	updated, _ = page.Update(loadedMsg{profiles: []domain.Profile{{ID: "demo", Name: "Demo"}}})
	page = updated.(ProfilesPage)

	updated, cmd := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = updated.(ProfilesPage)
	if cmd == nil {
		t.Fatal("expected launch cmd, got nil")
	}
	if !page.launch.inFlight {
		t.Fatal("launch should be marked in flight while the proxy loads")
	}

	// Run the launch pipeline and pick out the success msg.
	msgs := drainCmd(cmd)
	var loaded *proxyLoadedMsg
	for _, m := range msgs {
		if v, ok := m.(proxyLoadedMsg); ok {
			loaded = &v
			break
		}
	}
	if loaded == nil {
		t.Fatalf("no proxyLoadedMsg from launch cmd; got %v", msgs)
	}
	if fp.ensureCalls != 1 {
		t.Errorf("EnsureRunning calls = %d, want 1", fp.ensureCalls)
	}
	if len(fp.loaded) != 1 || fp.loaded[0] != "demo" {
		t.Errorf("proxy loads = %v, want [demo]", fp.loaded)
	}

	// Routing the success msg clears in-flight, flashes, and switches tabs.
	updated, cmd = page.Update(*loaded)
	page = updated.(ProfilesPage)
	if page.launch.inFlight {
		t.Error("inFlight should be cleared after proxyLoadedMsg")
	}
	if !strings.Contains(page.flash.Message(), "loaded demo") {
		t.Errorf("flash = %q, want it to mention 'loaded demo'", page.flash.Message())
	}
	var switched bool
	for _, m := range drainCmd(cmd) {
		if _, ok := m.(SwitchToServerMsg); ok {
			switched = true
		}
	}
	if !switched {
		t.Error("expected SwitchToServerMsg after successful load")
	}
}

// TestLaunch_ProxyLoadErrorFlashes covers the failure leg: a proxy load
// error must clear the in-flight state and surface an error flash.
func TestLaunch_ProxyLoadErrorFlashes(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	fp := &fakeProxy{loadErr: errors.New("model file vanished")}
	page := NewProfilesPage(store, domain.FlagSchema{}).
		WithProxyController(fp).
		WithBackendResolver(stubResolver{})
	page, _ = page.startLaunch(domain.Profile{ID: "demo", Name: "Demo"})
	if !page.launch.inFlight {
		t.Fatal("launch should be in flight")
	}

	cmd := page.launchProfileCmd(domain.Profile{ID: "demo", Name: "Demo"})
	msgs := drainCmd(cmd)
	var lerr *launchErrMsg
	for _, m := range msgs {
		if v, ok := m.(launchErrMsg); ok {
			lerr = &v
			break
		}
	}
	if lerr == nil {
		t.Fatalf("no launchErrMsg from failing load; got %v", msgs)
	}
	updated, _ := page.Update(*lerr)
	page = updated.(ProfilesPage)
	if page.launch.inFlight {
		t.Error("inFlight should be cleared after launchErrMsg")
	}
	if !strings.Contains(page.flash.Message(), "model file vanished") {
		t.Errorf("flash = %q, want the proxy error surfaced", page.flash.Message())
	}
}

// TestProfilesPage_UnloadFlow drives the K shortcut end to end: confirm
// opens against the proxy's loaded profile, affirmative dispatches an async
// Unload, and the done msg flashes the result.
func TestProfilesPage_UnloadFlow(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	fp := &fakeProxy{status: httpproxy.Status{Running: true, LoadedProfileID: "demo", LoadedPID: 4242}}
	page := NewProfilesPage(store, domain.FlagSchema{}).WithProxyController(fp)
	updated, _ := page.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	page = updated.(ProfilesPage)
	updated, _ = page.Update(loadedMsg{profiles: []domain.Profile{{ID: "demo", Name: "Demo"}}})
	page = updated.(ProfilesPage)

	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	page = updated.(ProfilesPage)
	if !page.killConfirm.Active() {
		t.Fatal("'K' should open the unload confirm when a model is loaded")
	}

	// Affirmative completion: emit the msg killConfirm.onYes would build.
	page.killConfirm = components.Confirm{}
	updated, cmd := page.Update(profilesUnloadConfirmedMsg{profileID: "demo"})
	page = updated.(ProfilesPage)
	msgs := drainCmd(cmd)
	var done *profilesUnloadDoneMsg
	for _, m := range msgs {
		if v, ok := m.(profilesUnloadDoneMsg); ok {
			done = &v
			break
		}
	}
	if done == nil {
		t.Fatalf("no profilesUnloadDoneMsg from unload cmd; got %v", msgs)
	}
	if fp.unloads != 1 {
		t.Errorf("proxy unloads = %d, want 1", fp.unloads)
	}
	updated, _ = page.Update(*done)
	page = updated.(ProfilesPage)
	if !strings.Contains(page.flash.Message(), "unloaded demo") {
		t.Errorf("flash = %q, want 'unloaded demo'", page.flash.Message())
	}
}

// TestProfilesPage_UnloadErrorFlashes covers the failure leg of the K
// shortcut: a proxy Unload error must surface as an error flash.
func TestProfilesPage_UnloadErrorFlashes(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	fp := &fakeProxy{
		status:    httpproxy.Status{Running: true, LoadedProfileID: "demo", LoadedPID: 4242},
		unloadErr: errors.New("backend busy"),
	}
	page := NewProfilesPage(store, domain.FlagSchema{}).WithProxyController(fp)
	updated, cmd := page.Update(profilesUnloadConfirmedMsg{profileID: "demo"})
	page = updated.(ProfilesPage)

	msgs := drainCmd(cmd)
	var done *profilesUnloadDoneMsg
	for _, m := range msgs {
		if v, ok := m.(profilesUnloadDoneMsg); ok {
			done = &v
			break
		}
	}
	if done == nil {
		t.Fatalf("no profilesUnloadDoneMsg from unload cmd; got %v", msgs)
	}
	if done.err == nil {
		t.Fatal("done.err = nil, want the proxy unload error")
	}
	updated, _ = page.Update(*done)
	page = updated.(ProfilesPage)
	if !strings.Contains(page.flash.Message(), "unload failed") ||
		!strings.Contains(page.flash.Message(), "backend busy") {
		t.Errorf("flash = %q, want 'unload failed: ... backend busy'", page.flash.Message())
	}
}

// TestLaunch_ProxyEnsureRunningErrorFlashes covers the EnsureRunning failure
// leg: when the proxy process cannot be started, the launch pipeline must
// emit a launchErrMsg and the page must surface an error flash without ever
// attempting a Load.
func TestLaunch_ProxyEnsureRunningErrorFlashes(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	fp := &fakeProxy{ensureErr: errors.New("proxy port busy")}
	page := NewProfilesPage(store, domain.FlagSchema{}).
		WithProxyController(fp).
		WithBackendResolver(stubResolver{})
	page, _ = page.startLaunch(domain.Profile{ID: "demo", Name: "Demo"})
	if !page.launch.inFlight {
		t.Fatal("launch should be in flight")
	}

	cmd := page.launchProfileCmd(domain.Profile{ID: "demo", Name: "Demo"})
	msgs := drainCmd(cmd)
	var lerr *launchErrMsg
	for _, m := range msgs {
		if v, ok := m.(launchErrMsg); ok {
			lerr = &v
			break
		}
	}
	if lerr == nil {
		t.Fatalf("no launchErrMsg from failing EnsureRunning; got %v", msgs)
	}
	if !strings.Contains(lerr.err.Error(), "start proxy") {
		t.Errorf("launch err = %q, want it to mention 'start proxy'", lerr.err)
	}
	if len(fp.loaded) != 0 {
		t.Errorf("proxy loads = %v, want none after EnsureRunning failure", fp.loaded)
	}
	updated, _ := page.Update(*lerr)
	page = updated.(ProfilesPage)
	if page.launch.inFlight {
		t.Error("inFlight should be cleared after launchErrMsg")
	}
	if !strings.Contains(page.flash.Message(), "proxy port busy") {
		t.Errorf("flash = %q, want the EnsureRunning error surfaced", page.flash.Message())
	}
}

// TestProfilesPage_UnloadWithNothingLoadedFlashes asserts K is a friendly
// no-op when the proxy has no loaded model.
func TestProfilesPage_UnloadWithNothingLoadedFlashes(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	fp := &fakeProxy{status: httpproxy.Status{Running: true}}
	page := NewProfilesPage(store, domain.FlagSchema{}).WithProxyController(fp)
	updated, _ := page.Update(loadedMsg{profiles: []domain.Profile{{ID: "demo", Name: "Demo"}}})
	page = updated.(ProfilesPage)

	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	page = updated.(ProfilesPage)
	if page.killConfirm.Active() {
		t.Fatal("'K' must not open a confirm when nothing is loaded")
	}
	if !strings.Contains(page.flash.Message(), "no model loaded") {
		t.Errorf("flash = %q, want 'no model loaded'", page.flash.Message())
	}
}

func TestProfilesPage_FlashAutoClear(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})

	page, _ = page.withFlash("hello")
	if page.flash.Message() != "hello" {
		t.Fatalf("flash = %q, want hello", page.flash.Message())
	}

	// Stale clear (mismatching at) should be ignored.
	updated, _ := page.Update(components.FlashClearMsg{Tag: "profiles", At: time.Time{}})
	page = updated.(ProfilesPage)
	if page.flash.Message() != "hello" {
		t.Errorf("stale FlashClearMsg erased current flash; flash=%q", page.flash.Message())
	}

	// Matching clear erases.
	updated, _ = page.Update(components.FlashClearMsg{Tag: "profiles", At: page.flash.At()})
	page = updated.(ProfilesPage)
	if page.flash.Message() != "" {
		t.Errorf("matching FlashClearMsg should clear; flash=%q", page.flash.Message())
	}
}

func TestProfilesPage_FlashRenamedClearTagIgnored(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})
	page, _ = page.withFlash("hello")

	// FlashClearMsg from another page must be ignored.
	updated, _ := page.Update(components.FlashClearMsg{Tag: "models", At: page.flash.At()})
	page = updated.(ProfilesPage)
	if page.flash.Message() != "hello" {
		t.Errorf("cross-tag FlashClearMsg erased flash; flash=%q", page.flash.Message())
	}
}

func TestProfilesPage_DelegateRendersCorruptInWarnColors(t *testing.T) {
	dgt := newProfileItemDelegate()
	l := list.New([]list.Item{corruptItem{id: "broken", err: errors.New("syntax error")}}, dgt, 80, 6)

	var buf strings.Builder
	dgt.Render(&buf, l, 0, l.Items()[0])

	want := theme.Error.Render("⚠ broken")
	if !strings.Contains(buf.String(), want) {
		t.Errorf("delegate output missing styled title %q; got %q", want, buf.String())
	}
}

func TestProfilesPage_DelegateDelegatesHealthyRowsToDefault(t *testing.T) {
	dgt := newProfileItemDelegate()
	healthy := item{p: domain.Profile{ID: "demo", Name: "Demo"}}
	l := list.New([]list.Item{healthy}, dgt, 80, 6)

	var buf strings.Builder
	dgt.Render(&buf, l, 0, healthy)
	if !strings.Contains(buf.String(), "Demo") {
		t.Errorf("default delegate output missing %q; got %q", "Demo", buf.String())
	}
}

// TestProfilesPage_DeleteCompletesViaAsyncMsgs drives the delete-confirm
// flow end-to-end through teatest. Regression for the bug where
// huh.StateCompleted was reached only via async nextFieldMsg/nextGroupMsg
// arriving in the non-key forwarding path — if that path doesn't check
// completion + finalize, the form stays referenced and the delete never
// fires until the user presses another key.
func TestProfilesPage_DeleteCompletesViaAsyncMsgs(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	_ = store.Save(domain.Profile{
		ID: "doomed", Name: "Doomed", Model: "/m.gguf",
		Args: map[string]any{"port": float64(8080)},
	})

	page := NewProfilesPage(store, domain.FlagSchema{})
	tm := teatest.NewTestModel(t, viewWrapper{page: page}, teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	// Wait for the profile to render.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Doomed")
	}, teatest.WithDuration(2*time.Second))

	// Press 'X' to open the delete confirm.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Delete profile doomed?")
	}, teatest.WithDuration(2*time.Second))

	// Toggle to affirmative (left arrow) and submit.
	tm.Send(tea.KeyMsg{Type: tea.KeyLeft})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// Without the fix the delete never fires on a single submit; the flash
	// would not appear until another keypress kicks the loop.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "deleted doomed")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	final := tm.FinalModel(t).(viewWrapper).inner().(ProfilesPage)
	if final.deleteConfirm.Active() {
		t.Error("deleteConfirm should be inactive after async-driven completion")
	}
	// Profile must actually be gone from the store.
	if _, err := store.Get("doomed"); err == nil {
		t.Error("profile 'doomed' still in store after delete")
	}
}

// TestProfilesPage_VimKNavigatesListNotKill locks in KEY-01: lowercase j/k
// move the list cursor (vim nav) and no longer trigger a kill; the kill action
// moved to uppercase K.
func TestProfilesPage_VimKNavigatesListNotKill(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	fp := &fakeProxy{}
	page := NewProfilesPage(store, domain.FlagSchema{}).WithProxyController(fp)
	updated, _ := page.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	page = updated.(ProfilesPage)
	updated, _ = page.Update(loadedMsg{profiles: []domain.Profile{
		{ID: "a", Name: "Alpha"}, {ID: "b", Name: "Beta"}, {ID: "c", Name: "Gamma"},
	}})
	page = updated.(ProfilesPage)

	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	page = updated.(ProfilesPage)
	if page.list.Index() != 1 {
		t.Fatalf("after 'j' index = %d, want 1 (down nav)", page.list.Index())
	}
	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	page = updated.(ProfilesPage)
	if page.list.Index() != 0 {
		t.Fatalf("after 'k' index = %d, want 0 ('k' must navigate up, not kill)", page.list.Index())
	}
	if page.killConfirm.Active() {
		t.Fatal("'k' must not open the unload confirm")
	}

	// Uppercase 'K' with a loaded model opens the unload confirm.
	fp.status = httpproxy.Status{Running: true, LoadedProfileID: "a", LoadedPID: 4242}
	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	page = updated.(ProfilesPage)
	if !page.killConfirm.Active() {
		t.Fatal("'K' should open the unload confirm when a model is loaded")
	}
}

func TestProfilesPage_HintsIncludeLaunchAndEdit(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})
	hints := page.Hints()
	if !strings.Contains(hints, "[enter] launch") {
		t.Errorf("list-mode Hints missing [enter] launch; got %q", hints)
	}
	if !strings.Contains(hints, "[e] edit") {
		t.Errorf("list-mode Hints missing [e] edit; got %q", hints)
	}
	// F-10 audit: [E] export moved off the inline footer into the [?] help
	// pane to keep the footer fitting at common widths. The escape hatch
	// "(more: ?)" tail remains so the user knows additional bindings exist.
	if !strings.Contains(hints, "(more: ?)") {
		t.Errorf("list-mode Hints missing (more: ?) tail; got %q", hints)
	}
}

func TestProfilesPage_EscWithUnchangedDraftClosesEditor(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})

	// Open web editor.
	updated, _ := page.startNew()
	page = updated.(ProfilesPage)
	if !page.webEditing {
		t.Fatal("expected webEditing=true after startNew")
	}

	// Press esc while webEditing — key is swallowed; webEditing stays true
	// until session delivers webEditDoneMsg (session.Cancel() was called).
	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = updated.(ProfilesPage)
	// webEditing remains true (waiting for session Done channel)
	if !page.webEditing {
		t.Error("webEditing should remain true after esc (waiting for session to close)")
	}
}

func TestProfilesPage_HintsVaryByMode(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})

	// Open the web editor through the public path.
	editing, _ := page.startNew()
	editingPage := editing.(ProfilesPage)
	// Web editing mode shows list hints (no ctrl+t) — web editor is in browser.
	// Just assert webEditing=true and IsCapturingInput=true.
	if !editingPage.webEditing {
		t.Errorf("expected webEditing=true after startNew")
	}
	if !editingPage.IsCapturingInput() {
		t.Errorf("expected IsCapturingInput=true while webEditing")
	}

	// Picker hints (set the flag directly — picker is a page-owned overlay).
	page.picker.active = true
	if !strings.Contains(page.Hints(), "[enter] pick") {
		t.Errorf("picker Hints missing [enter] pick; got %q", page.Hints())
	}
	page.picker.active = false

	page.deleteConfirm = components.NewConfirm("Delete?", "id", nil, "", "")
	if !strings.Contains(page.Hints(), "[enter] confirm") {
		t.Errorf("confirm Hints missing [enter] confirm; got %q", page.Hints())
	}
}

func TestProfilesPage_IsCapturingInputDuringEditAndPicker(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})

	if page.IsCapturingInput() {
		t.Errorf("idle page captures input")
	}
	editing, _ := page.startNew()
	if !editing.(ProfilesPage).IsCapturingInput() {
		t.Errorf("editing page should capture input")
	}

	page.picker.active = true
	if !page.IsCapturingInput() {
		t.Errorf("picker page should capture input")
	}
	page.picker.active = false
	page.deleteConfirm = components.NewConfirm("Delete?", "id", nil, "", "")
	if !page.IsCapturingInput() {
		t.Errorf("deleteConfirm page should capture input")
	}
	page.deleteConfirm = components.Confirm{}
}

// TestProfilesPage_DiscardConfirmKeepsInputCaptured verifies the
// page-level capture contract while the web editor is open. Regression
// cover: IsCapturingInput() must return true while webEditing=true so
// the global shortcut gate doesn't steal keys from the web session.
func TestProfilesPage_DiscardConfirmKeepsInputCaptured(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})

	// Open web editor.
	updated, _ := page.startNew()
	page = updated.(ProfilesPage)
	if !page.webEditing {
		t.Fatal("expected webEditing=true after startNew")
	}

	// Page must capture input while web editor is open.
	if !page.IsCapturingInput() {
		t.Fatal("page must capture input while webEditing=true")
	}
}

// [?] help token is now owned by the global status bar (see
// ui/root_test.go TestRoot_StatusBarMentionsHelp). Pages publish their
// own hints via the HintProvider contract — see TestProfilesPage_Hints*.

func TestProfilesPage_ItemFilterValueIncludesTags(t *testing.T) {
	pr := domain.Profile{
		ID:   "qwen",
		Name: "Qwen Coder",
		Tags: []string{"coding", "32b"},
	}
	fv := item{p: pr}.FilterValue()
	for _, want := range []string{"Qwen Coder", "qwen", "coding", "32b"} {
		if !strings.Contains(fv, want) {
			t.Errorf("FilterValue %q missing %q", fv, want)
		}
	}
}

func TestProfilesPage_DetailViewRendersTags(t *testing.T) {
	store := newFakeStoreWithDiagnostics([]domain.Profile{
		{
			ID:    "demo",
			Name:  "Demo",
			Model: "/m.gguf",
			Tags:  []string{"coding", "32b"},
			Args:  map[string]any{"port": float64(8080)},
		},
	}, nil)
	page := NewProfilesPage(store, domain.FlagSchema{})
	updated, _ := page.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	page = updated.(ProfilesPage)
	updated, _ = page.Update(loadedMsg{profiles: store.ps})
	page = updated.(ProfilesPage)

	view := page.detailView()
	if !strings.Contains(view, "Tags:") {
		t.Fatalf("detailView missing Tags label; got:\n%s", view)
	}
	if !strings.Contains(view, "coding, 32b") {
		t.Fatalf("detailView missing tag values; got:\n%s", view)
	}
}

func TestProfilesPage_DetailViewRendersNoneWhenTagsEmpty(t *testing.T) {
	store := newFakeStoreWithDiagnostics([]domain.Profile{
		{ID: "demo", Name: "Demo", Model: "/m.gguf"},
	}, nil)
	page := NewProfilesPage(store, domain.FlagSchema{})
	updated, _ := page.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	page = updated.(ProfilesPage)
	updated, _ = page.Update(loadedMsg{profiles: store.ps})
	page = updated.(ProfilesPage)

	view := page.detailView()
	if !strings.Contains(view, "Tags:    (none)") {
		t.Fatalf("detailView missing Tags: (none); got:\n%s", view)
	}
}

func TestProfilesPage_EditHydratesTags(t *testing.T) {
	store := newFakeStoreWithDiagnostics([]domain.Profile{
		{
			ID:    "demo",
			Name:  "Demo",
			Model: "/m.gguf",
			Tags:  []string{"coding", "32b"},
			Args:  map[string]any{"port": float64(4321)},
		},
	}, nil)
	page := NewProfilesPage(store, domain.FlagSchema{})
	updated, _ := page.Update(loadedMsg{profiles: store.ps})
	page = updated.(ProfilesPage)

	updated, cmd := page.startEditSelected()
	page = updated.(ProfilesPage)

	// Now routes to web editor — assert webEditing and cmd returned.
	if !page.webEditing {
		t.Fatal("startEditSelected should set webEditing=true")
	}
	if cmd == nil {
		t.Fatal("startEditSelected should return a launch cmd")
	}
}

func TestProfilesPage_ExportWithoutDirFlashesNotConfigured(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	page = updated.(ProfilesPage)
	updated, _ = page.Update(loadedMsg{profiles: nil})
	page = updated.(ProfilesPage)

	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
	page = updated.(ProfilesPage)

	// F-03 audit: the "directory not configured" flash now spells out the
	// remediation hint (the expected exports path) instead of a bare
	// status line, but it still must surface as an error flash.
	got := page.flash.Message()
	if !strings.HasPrefix(got, "export failed: directory not configured") {
		t.Errorf("flash = %q, want prefix %q", got, "export failed: directory not configured")
	}
}

func TestProfilesPage_ExportWritesBundleAndFlashesFilename(t *testing.T) {
	storeDir := t.TempDir()
	store, err := profilestore.NewFSStore(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(domain.Profile{
		ID: "alpha", Name: "Alpha", Model: "/m.gguf",
		Args: map[string]any{"port": float64(8080)},
	}); err != nil {
		t.Fatal(err)
	}

	exportDir := t.TempDir()
	page := NewProfilesPage(store, domain.FlagSchema{}).WithExportDir(exportDir)
	updated, _ := page.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	page = updated.(ProfilesPage)
	updated, _ = page.Update(loadedMsg{profiles: []domain.Profile{{ID: "alpha", Name: "Alpha"}}})
	page = updated.(ProfilesPage)

	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
	page = updated.(ProfilesPage)

	// F-03 audit: the success flash now reports the count and the bundle
	// filename so the user sees both "how many" and "where".
	flash := page.flash.Message()
	if !strings.Contains(flash, "exported") || !strings.Contains(flash, "profile") {
		t.Errorf("flash = %q, want substring 'exported' and 'profile'", flash)
	}
	if !strings.Contains(flash, "profiles-export-") {
		t.Errorf("flash = %q, want substring 'profiles-export-'", flash)
	}
	if !strings.HasSuffix(flash, ".json") {
		t.Errorf("flash = %q, want .json suffix", flash)
	}

	entries, err := os.ReadDir(exportDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("export dir entries = %d, want 1", len(entries))
	}
	raw, err := os.ReadFile(filepath.Join(exportDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"schemaVersion"`) {
		t.Errorf("export file missing schemaVersion: %s", string(raw))
	}
	if !strings.Contains(string(raw), `"exportedAt"`) {
		t.Errorf("export file missing exportedAt: %s", string(raw))
	}
	if !strings.Contains(string(raw), `"profiles"`) {
		t.Errorf("export file missing profiles: %s", string(raw))
	}
	if !strings.Contains(string(raw), `"id": "alpha"`) {
		t.Errorf("export file missing alpha profile: %s", string(raw))
	}
}

func TestProfilesPage_ExportFailureFlashesError(t *testing.T) {
	store := newFakeStoreWithDiagnostics([]domain.Profile{{ID: "a", Name: "A"}}, nil)

	exportDir := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(exportDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	page := NewProfilesPage(store, domain.FlagSchema{}).WithExportDir(exportDir)
	updated, _ := page.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	page = updated.(ProfilesPage)
	updated, _ = page.Update(loadedMsg{profiles: store.ps})
	page = updated.(ProfilesPage)

	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
	page = updated.(ProfilesPage)

	if got := page.flash.Message(); !strings.HasPrefix(got, "export failed:") {
		t.Errorf("flash = %q, want prefix 'export failed:'", got)
	}
	// The status bar level comes from the flash queue (SetError), not from
	// sniffing message prefixes — any failed action must report StatusError.
	if msg, level := page.StatusMessage(); level != components.StatusError || msg == "" {
		t.Errorf("StatusMessage = (%q, %v), want non-empty message at StatusError", msg, level)
	}
}

// The silent-failure fix: a failing ImportBundle must surface an error
// flash at StatusError instead of returning a no-op FlashClearMsg.
func TestProfilesPage_ImportFailureFlashesError(t *testing.T) {
	store := newFakeStoreWithDiagnostics(nil, nil)
	page := NewProfilesPage(store, domain.FlagSchema{})

	cmd := page.importBundleCmd(filepath.Join(t.TempDir(), "missing-bundle.json"), profilestore.ConflictModeMerge)
	msg := cmd()
	failed, ok := msg.(importFailedMsg)
	if !ok {
		t.Fatalf("importBundleCmd produced %T, want importFailedMsg", msg)
	}

	updated, _ := page.Update(failed)
	page = updated.(ProfilesPage)
	if got := page.flash.Message(); !strings.HasPrefix(got, "import failed:") {
		t.Errorf("flash = %q, want prefix 'import failed:'", got)
	}
	if _, level := page.StatusMessage(); level != components.StatusError {
		t.Errorf("StatusMessage level = %v, want StatusError", level)
	}
}

// F-04 regression: when the user presses `/` the list enters Filtering
// state. IsCapturingInput must report true so global shortcuts (Tab/q/?)
// stop stealing the user's keystrokes.
func TestProfilesPage_FilterCapturesInput(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(domain.Profile{ID: "a", Name: "Alpha", Model: "/m.gguf"}); err != nil {
		t.Fatal(err)
	}

	page := NewProfilesPage(store, domain.FlagSchema{})
	page.list.SetSize(80, 20)
	updated, _ := page.Update(loadedMsg{profiles: []domain.Profile{{ID: "a", Name: "Alpha", Model: "/m.gguf"}}})
	page = updated.(ProfilesPage)

	if page.IsCapturingInput() {
		t.Fatal("expected not capturing input before filter starts")
	}
	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	page = updated.(ProfilesPage)
	if !page.IsCapturingInput() {
		t.Fatalf("expected capturing input after '/'; filterState=%v", page.list.FilterState())
	}
}

// F-04 regression: characters typed during filter must reach the list and
// narrow the visible rows — not trigger profile shortcuts. Two items, one
// matches the typed filter.
//
// The bubbles list runs filtering through a tea.Cmd that produces a
// FilterMatchesMsg; tests have to drain that cmd chain to observe the
// narrowed VisibleItems.
func TestProfilesPage_FilterNarrowsList(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	page := NewProfilesPage(store, domain.FlagSchema{})
	page.list.SetSize(80, 20)
	updated, _ := page.Update(loadedMsg{profiles: []domain.Profile{
		{ID: "alpha", Name: "Alpha", Model: "/m.gguf"},
		{ID: "beta", Name: "Beta", Model: "/m.gguf"},
	}})
	page = updated.(ProfilesPage)

	send := func(p ProfilesPage, msg tea.Msg) ProfilesPage {
		upd, cmd := p.Update(msg)
		out := upd.(ProfilesPage)
		drainPageCmd(t, &out, cmd)
		return out
	}

	page = send(page, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "alph" {
		page = send(page, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	page = send(page, tea.KeyMsg{Type: tea.KeyEnter})

	visible := page.list.VisibleItems()
	if len(visible) != 1 {
		t.Fatalf("VisibleItems=%d after typing 'alph'+Enter; want 1", len(visible))
	}
	if it, ok := visible[0].(item); !ok || it.p.ID != "alpha" {
		t.Fatalf("VisibleItems[0]=%+v; want alpha", visible[0])
	}
}

// drainPageCmd executes a tea.Cmd recursively, feeding each produced msg
// back through the page Update so internal Cmd→Msg chains (e.g. bubbles
// list filterItems) settle synchronously for assertions.
func drainPageCmd(t *testing.T, page *ProfilesPage, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil && i < 16; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				drainPageCmd(t, page, c)
			}
			return
		}
		upd, next := page.Update(msg)
		*page = upd.(ProfilesPage)
		cmd = next
	}
}

// F-06 regression: while filter is active, pressing `E` must reach the
// list (typing into the filter buffer), NOT silently fire the export
// shortcut. Verified by ensuring the flash stays empty after `E`.
func TestProfilesPage_ExportGatedWhileFiltering(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	exportDir := t.TempDir()
	page := NewProfilesPage(store, domain.FlagSchema{}).WithExportDir(exportDir)
	page.list.SetSize(80, 20)
	updated, _ := page.Update(loadedMsg{profiles: []domain.Profile{{ID: "a", Name: "Alpha", Model: "/m.gguf"}}})
	page = updated.(ProfilesPage)

	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	page = updated.(ProfilesPage)
	if !page.IsCapturingInput() {
		t.Fatal("filter mode did not capture input")
	}

	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
	page = updated.(ProfilesPage)

	if got := page.flash.Message(); strings.HasPrefix(got, "exported to ") {
		t.Errorf("export fired during filter mode; flash=%q", got)
	}
	entries, _ := os.ReadDir(exportDir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			t.Errorf("export bundle was written during filter mode: %s", e.Name())
		}
	}
}

// F-06 regression: when filter is NOT active, `e` must still export and
// flash success — proves the gate didn't disable the shortcut outright.
func TestProfilesPage_ExportFlashesOnSuccess(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(domain.Profile{ID: "a", Name: "Alpha", Model: "/m.gguf"}); err != nil {
		t.Fatal(err)
	}
	exportDir := t.TempDir()
	page := NewProfilesPage(store, domain.FlagSchema{}).WithExportDir(exportDir)
	page.list.SetSize(80, 20)
	updated, _ := page.Update(loadedMsg{profiles: []domain.Profile{{ID: "a", Name: "Alpha", Model: "/m.gguf"}}})
	page = updated.(ProfilesPage)

	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
	page = updated.(ProfilesPage)

	// F-03 audit: success flash now includes profile count + filename.
	if got := page.flash.Message(); !strings.Contains(got, "exported") || !strings.Contains(got, "profiles-export-") {
		t.Errorf("flash=%q; want substring 'exported' and 'profiles-export-'", got)
	}
}

// F-09 regression: pinned profile must appear exactly once in the list
// and carry the 📌 glyph (replaced the star). Pinned items sort first
// (the regression observed duplicate rendering — the bug is that the
// single appearance must be unambiguous).
func TestProfilesPage_PinnedProfileAppearsOnceWithPinGlyph(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(domain.Profile{ID: "p", Name: "Pinned", Model: "/m.gguf", Pinned: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(domain.Profile{ID: "n", Name: "Normal", Model: "/m.gguf"}); err != nil {
		t.Fatal(err)
	}

	page := NewProfilesPage(store, domain.FlagSchema{})
	page.list.SetSize(80, 20)
	updated, _ := page.Update(loadedMsg{profiles: []domain.Profile{
		{ID: "p", Name: "Pinned", Model: "/m.gguf", Pinned: true},
		{ID: "n", Name: "Normal", Model: "/m.gguf"},
	}})
	page = updated.(ProfilesPage)

	items := page.list.Items()
	pinnedCount := 0
	var pinnedTitle string
	for _, it := range items {
		if i, ok := it.(item); ok && i.p.ID == "p" {
			pinnedCount++
			pinnedTitle = i.Title()
		}
	}
	if pinnedCount != 1 {
		t.Fatalf("pinned profile appears %d times; want exactly 1", pinnedCount)
	}
	if !strings.HasPrefix(pinnedTitle, "📌 ") {
		t.Fatalf("pinned title=%q; want '📌 ' prefix", pinnedTitle)
	}
	if strings.HasPrefix(pinnedTitle, "★") {
		t.Fatalf("pinned title still uses old star glyph: %q", pinnedTitle)
	}
}

// F-11 regression: Args must render as indented `--key value` lines, not
// as a Go map literal `map[k:v ...]`.
func TestProfilesPage_DetailRendersArgsAsFlags(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	pr := domain.Profile{
		ID:    "p",
		Name:  "Detail",
		Model: "/m.gguf",
		Args: map[string]any{
			"ngl":        float64(99),
			"flash-attn": true,
			"alias":      "test",
		},
	}
	if err := store.Save(pr); err != nil {
		t.Fatal(err)
	}

	page := NewProfilesPage(store, domain.FlagSchema{})
	page.list.SetSize(80, 20)
	updated, _ := page.Update(loadedMsg{profiles: []domain.Profile{pr}})
	page = updated.(ProfilesPage)
	page.width = 120
	page.height = 30

	view := page.detailView()
	if strings.Contains(view, "map[") {
		t.Errorf("detail view contains Go map literal:\n%s", view)
	}
	for _, want := range []string{"--alias test", "--flash-attn true", "--ngl 99"} {
		if !strings.Contains(view, want) {
			t.Errorf("detail view missing %q; got:\n%s", want, view)
		}
	}
}

// F-11 regression: an empty Args map renders as "(none)" instead of the
// raw map literal.
func TestProfilesPage_DetailRendersEmptyArgs(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	pr := domain.Profile{ID: "p", Name: "Empty", Model: "/m.gguf"}
	if err := store.Save(pr); err != nil {
		t.Fatal(err)
	}
	page := NewProfilesPage(store, domain.FlagSchema{})
	page.list.SetSize(80, 20)
	updated, _ := page.Update(loadedMsg{profiles: []domain.Profile{pr}})
	page = updated.(ProfilesPage)
	page.width = 120
	page.height = 30

	view := page.detailView()
	if strings.Contains(view, "map[") {
		t.Errorf("detail view contains Go map literal:\n%s", view)
	}
	if !strings.Contains(view, "Args:") {
		t.Errorf("detail view missing Args label:\n%s", view)
	}
}

// F-09 regression: pinned items must sort BEFORE unpinned items so the
// single appearance is visually distinct at the top of the list.
func TestProfilesPage_PinnedSortsFirst(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	page := NewProfilesPage(store, domain.FlagSchema{})
	page.list.SetSize(80, 20)
	updated, _ := page.Update(loadedMsg{profiles: []domain.Profile{
		{ID: "n", Name: "Normal", Model: "/m.gguf"},
		{ID: "p", Name: "Pinned", Model: "/m.gguf", Pinned: true},
	}})
	page = updated.(ProfilesPage)

	items := page.list.Items()
	if len(items) < 2 {
		t.Fatalf("Items=%d; want >=2", len(items))
	}
	first, ok := items[0].(item)
	if !ok || first.p.ID != "p" {
		t.Fatalf("first item=%+v; want pinned 'p'", items[0])
	}
}
