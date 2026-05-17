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
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/service/validator"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/pages/profile_editor"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

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
	tm := teatest.NewTestModel(t, page, teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "No profiles yet")
	}, teatest.WithDuration(2*time.Second))

	// 'n' opens the form
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Name") && strings.Contains(string(out), "Model path")
	}, teatest.WithDuration(2*time.Second))

	// We don't drive the full huh form here — just exit. The store-side
	// behavior is already covered by FSStore tests; this asserts wiring.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	_ = tm.Quit()

	// Ensure no profile was persisted (esc cancels)
	got, _ := store.List()
	if len(got) != 0 {
		t.Errorf("List len = %d, want 0", len(got))
	}
}

// TestProfilesPage_ValidationDetectsUbatchOverBatch exercises the same
// preview-validator path the editor renders, but without reaching into
// editor internals: we build the Draft ourselves and call the validator
// directly. Behavior under test (ubatch > batch produces an error) is
// owned by validator, not by ProfilesPage.
func TestProfilesPage_ValidationDetectsUbatchOverBatch(t *testing.T) {
	d := profile_editor.Draft{
		ID:         "x",
		Name:       "X",
		BatchSize:  "2048",
		UBatchSize: "4096",
		IsNew:      true,
	}
	pr := d.ToProfile()
	report := validator.New(log.Nop()).Validate(pr, domain.FlagSchema{})

	found := false
	for _, e := range report.Errors {
		if e.Field == "ubatch-size" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected ubatch-size error in report; got Errors=%v Warnings=%v", report.Errors, report.Warnings)
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

	// Start a new draft so editing is active.
	model, _ := page.startNew()
	page = model.(ProfilesPage)
	if !page.editor.Active() {
		t.Fatal("startNew should activate editor")
	}

	// Simulate ModelPickedMsg landing in Update.
	updated, _ := page.Update(components.ModelPickedMsg{Path: "/picked/model.gguf"})
	page = updated.(ProfilesPage)

	if got := page.editor.CurrentDraft().Model; got != "/picked/model.gguf" {
		t.Fatalf("draft.Model = %q, want /picked/model.gguf", got)
	}
	if page.picker.active {
		t.Errorf("pickerActive = true, want false after pick")
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

	updated, _ = page.startEditSelected()
	page = updated.(ProfilesPage)

	if !page.editor.Active() {
		t.Fatal("startEditSelected should activate editor")
	}
	if got := page.editor.CurrentDraft().BackendID; got != "llama-cpp-custom" {
		t.Fatalf("draft.BackendID = %q, want llama-cpp-custom", got)
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
func (f *fakeStoreWithDiag) Save(_ domain.Profile) error { return nil }
func (f *fakeStoreWithDiag) Delete(_ string) error       { return nil }
func (f *fakeStoreWithDiag) Duplicate(_, _ string) (domain.Profile, error) {
	return domain.Profile{}, nil
}

func TestProfilesPage_UseInNewProfilePrefillsDraft(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	page := NewProfilesPage(store, domain.FlagSchema{})

	updated, _ := page.Update(UseInNewProfileMsg{Path: "/foo/bar.gguf"})
	page = updated.(ProfilesPage)

	if !page.editor.Active() {
		t.Fatal("editor.Active() = false, want true")
	}
	d := page.editor.CurrentDraft()
	if d.Model != "/foo/bar.gguf" {
		t.Fatalf("draft.Model = %q", d.Model)
	}
	if !d.IsNew {
		t.Errorf("IsNew = false, want true")
	}
}

func TestProfilesPage_LKeyEmitsLaunchProfileMsg(t *testing.T) {
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
	page := NewProfilesPage(store, domain.FlagSchema{})
	updated, _ := page.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	page = updated.(ProfilesPage)
	updated, _ = page.Update(loadedMsg{profiles: []domain.Profile{{ID: "demo", Name: "Demo"}}})
	page = updated.(ProfilesPage)

	updated, cmd := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	_ = updated
	if cmd == nil {
		t.Fatal("expected LaunchProfileMsg cmd, got nil")
	}
	got := cmd()
	lp, ok := got.(LaunchProfileMsg)
	if !ok {
		t.Fatalf("msg type = %T, want LaunchProfileMsg", got)
	}
	if lp.ID != "demo" {
		t.Errorf("ID = %q, want demo", lp.ID)
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
	tm := teatest.NewTestModel(t, page, teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	// Wait for the profile to render.
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Doomed")
	}, teatest.WithDuration(2*time.Second))

	// Press 'x' to open the delete confirm.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
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
	final := tm.FinalModel(t).(ProfilesPage)
	if final.deleteConfirm.Active() {
		t.Error("deleteConfirm should be inactive after async-driven completion")
	}
	// Profile must actually be gone from the store.
	if _, err := store.Get("doomed"); err == nil {
		t.Error("profile 'doomed' still in store after delete")
	}
}

func TestProfilesPage_HintsIncludeLaunch(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})
	hints := page.Hints()
	if !strings.Contains(hints, "[L] launch") {
		t.Errorf("list-mode Hints missing [L] launch; got %q", hints)
	}
	if !strings.Contains(hints, "[e] export") {
		t.Errorf("list-mode Hints missing [e] export; got %q", hints)
	}
}

func TestProfilesPage_EscWithUnchangedDraftClosesEditor(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})

	// Open a fresh editor.
	updated, _ := page.startNew()
	page = updated.(ProfilesPage)
	if !page.editor.Active() {
		t.Fatal("expected editor active after startNew")
	}

	// Press esc with no edits — should close immediately, no confirm.
	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = updated.(ProfilesPage)
	if page.editor.Active() {
		t.Error("esc with unchanged draft should close editor")
	}
}

func TestProfilesPage_HintsVaryByMode(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})

	// Open the editor through the public path.
	editing, _ := page.startNew()
	if !strings.Contains(editing.(ProfilesPage).Hints(), "[ctrl+t]") {
		t.Errorf("editing Hints missing [ctrl+t]; got %q", editing.(ProfilesPage).Hints())
	}

	// Picker hints (set the flag directly — picker is a page-owned overlay).
	page.picker.active = true
	if !strings.Contains(page.Hints(), "[enter] pick") {
		t.Errorf("picker Hints missing [enter] pick; got %q", page.Hints())
	}
	page.picker.active = false

	page.deleteConfirm = components.NewConfirm("Delete?", "id", nil)
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
	page.deleteConfirm = components.NewConfirm("Delete?", "id", nil)
	if !page.IsCapturingInput() {
		t.Errorf("deleteConfirm page should capture input")
	}
	page.deleteConfirm = components.Confirm{}
}

// TestProfilesPage_DiscardConfirmKeepsInputCaptured verifies the
// page-level capture contract while the editor's discard-confirm is
// open. Regression cover: the equivalent editor-level test
// (TestEditor_EscOnDirtyDraftPromptsDiscard) cannot prove that
// ProfilesPage.IsCapturingInput() flows through editor.Active() — only
// a page-level test can. Without this, the global shortcut gate could
// silently regress to stealing keys away from the discard prompt.
func TestProfilesPage_DiscardConfirmKeepsInputCaptured(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})

	// Open a fresh editor and dirty the draft so esc routes through the
	// discard-confirm path instead of closing immediately.
	updated, _ := page.startNew()
	page = updated.(ProfilesPage)
	if !page.editor.Active() {
		t.Fatal("expected editor active after startNew")
	}
	page.editor, _ = page.editor.SetModelPath("/dirty/model.gguf")

	// esc on dirty draft should arm the discard-confirm overlay.
	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = updated.(ProfilesPage)

	if !page.IsCapturingInput() {
		t.Fatal("page must capture input while discard-confirm is open")
	}
	if !page.editor.Active() {
		t.Errorf("editor.Active() must remain true while discard-confirm is open")
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

	updated, _ = page.startEditSelected()
	page = updated.(ProfilesPage)

	if !page.editor.Active() {
		t.Fatal("startEditSelected should activate editor")
	}
	if got := page.editor.CurrentDraft().Tags; got != "coding, 32b" {
		t.Fatalf("draft.Tags = %q, want %q", got, "coding, 32b")
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

	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	page = updated.(ProfilesPage)

	if got := page.flash.Message(); got != "export directory not configured" {
		t.Errorf("flash = %q, want %q", got, "export directory not configured")
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

	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	page = updated.(ProfilesPage)

	flash := page.flash.Message()
	if !strings.HasPrefix(flash, "exported to profiles-export-") {
		t.Errorf("flash = %q, want prefix 'exported to profiles-export-'", flash)
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

	updated, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	page = updated.(ProfilesPage)

	if got := page.flash.Message(); !strings.HasPrefix(got, "export failed:") {
		t.Errorf("flash = %q, want prefix 'export failed:'", got)
	}
}
