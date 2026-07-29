package pages

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

// fakeModelsStore is a minimal profilestore.Store double for ModelsPage
// tests. It records the last Save call so tests can assert the Model
// path was persisted.
type fakeModelsStore struct {
	profiles []domain.Profile
	saved    *domain.Profile
	listErr  error
	getErr   error
	saveErr  error
}

func (f *fakeModelsStore) List() ([]domain.Profile, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.profiles, nil
}

func (f *fakeModelsStore) ListWithDiagnostics() ([]domain.Profile, []profilestore.ListDiagnostic, error) {
	return f.profiles, nil, nil
}

func (f *fakeModelsStore) Get(id string) (domain.Profile, error) {
	if f.getErr != nil {
		return domain.Profile{}, f.getErr
	}
	for _, p := range f.profiles {
		if p.ID == id {
			return p, nil
		}
	}
	return domain.Profile{}, profilestore.ErrNotFound
}

func (f *fakeModelsStore) Create(p domain.Profile) error { return f.Save(p) }

func (f *fakeModelsStore) Save(p domain.Profile) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	clone := p
	f.saved = &clone
	for i, existing := range f.profiles {
		if existing.ID == p.ID {
			f.profiles[i] = p
			return nil
		}
	}
	f.profiles = append(f.profiles, p)
	return nil
}

func (f *fakeModelsStore) Delete(_ string) error { return nil }
func (f *fakeModelsStore) Duplicate(_, _ string) (domain.Profile, error) {
	return domain.Profile{}, nil
}
func (f *fakeModelsStore) Rename(_ string, _ domain.Profile) error { return nil }

// fakeScanner emits a fixed sequence of events for tests.
type fakeScanner struct {
	events []domain.ScanEvent
}

func (f *fakeScanner) Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error) {
	ch := make(chan domain.ScanEvent, len(f.events)+1)
	go func() {
		defer close(ch)
		for _, e := range f.events {
			select {
			case ch <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func TestModelsPage_LoadsFilesIntoTable(t *testing.T) {
	mf := domain.ModelFile{
		Path:      "/tmp/models/qwen-32b.gguf",
		SizeBytes: 16_000_000_000,
		Name:      "qwen-32b.gguf",
		Quant:     "Q4_K_M",
		Params:    "32B",
	}
	scanner := &fakeScanner{events: []domain.ScanEvent{
		{Type: domain.ScanEventFile, Root: "/tmp/models", File: &mf},
		{Type: domain.ScanEventProgress, Root: "/tmp/models", Count: 1},
		{Type: domain.ScanEventDone},
	}}

	page := NewModelsPage(scanner, []string{"/tmp/models"})
	model := tea.Model(page)

	cmd := model.Init()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		var c tea.Cmd
		model, c = model.Update(msg)
		cmd = c
	}

	mp := model.(ModelsPage)
	if len(mp.files) != 1 {
		t.Fatalf("files = %d, want 1", len(mp.files))
	}
	if mp.files[0].Name != "qwen-32b.gguf" {
		t.Errorf("Name = %q", mp.files[0].Name)
	}
	if mp.statusMap["/tmp/models"].state != "scanned" {
		t.Errorf("status state = %q, want scanned", mp.statusMap["/tmp/models"].state)
	}
	if mp.statusMap["/tmp/models"].count != 1 {
		t.Errorf("status count = %d, want 1", mp.statusMap["/tmp/models"].count)
	}
}

func TestModelsPage_FilterModeReducesRows(t *testing.T) {
	files := []domain.ModelFile{
		{Path: "/m/a.gguf", Name: "alpha.gguf", Quant: "Q4_K_M"},
		{Path: "/m/b.gguf", Name: "beta.gguf", Quant: "Q5_K_M"},
		{Path: "/m/c.gguf", Name: "gamma.gguf", Quant: "Q8_0"},
	}
	scanner := &fakeScanner{}
	page := NewModelsPage(scanner, nil)
	page.files = files
	page.refreshRows()
	if got := len(page.table.Rows()); got != 3 {
		t.Fatalf("rows pre-filter = %d, want 3", got)
	}

	page.filter = "alpha"
	page.refreshRows()
	if got := len(page.table.Rows()); got != 1 {
		t.Fatalf("rows post-filter = %d, want 1", got)
	}
}

func TestModelsPage_ActionUseInNewProfileEmitsMsg(t *testing.T) {
	mf := domain.ModelFile{Path: "/m/q.gguf", Name: "q.gguf"}
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.files = []domain.ModelFile{mf}
	page.refreshRows()

	// Open the inline action menu programmatically and pick "new".
	page.action = &actionMenu{
		title:      "Action for q.gguf",
		options:    []actionOption{{label: "Use in new profile", value: "new"}},
		targetPath: mf.Path,
		stage:      actionStageRoot,
	}

	updated, cmd := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = updated
	if cmd == nil {
		t.Fatal("expected UseInNewProfileMsg cmd, got nil")
	}
	got := cmd()
	useMsg, ok := got.(UseInNewProfileMsg)
	if !ok {
		t.Fatalf("msg type = %T, want UseInNewProfileMsg", got)
	}
	if useMsg.Path != "/m/q.gguf" {
		t.Fatalf("Path = %q", useMsg.Path)
	}
}

// TestModelsPage_RescanDropsStaleEvents ensures events from a previous
// scan generation do not pollute state after rescan bumps scanID.
func TestModelsPage_RescanDropsStaleEvents(t *testing.T) {
	stale := domain.ModelFile{Path: "/old/stale.gguf", Name: "stale.gguf"}
	scanner := &fakeScanner{}
	page := NewModelsPage(scanner, []string{"/m"})
	page.scanID = 1 // simulate post-rescan epoch

	// Stale message tagged with old scanID=0 must be ignored.
	updated, _ := page.Update(scanEventMsg{
		scanID: 0,
		ch:     nil,
		evt:    domain.ScanEvent{Type: domain.ScanEventFile, Root: "/m", File: &stale},
	})
	mp := updated.(ModelsPage)
	if len(mp.files) != 0 {
		t.Fatalf("stale event accepted: files = %d, want 0", len(mp.files))
	}

	// Fresh message tagged with current scanID=1 must land.
	fresh := domain.ModelFile{Path: "/new/fresh.gguf", Name: "fresh.gguf"}
	updated, _ = mp.Update(scanEventMsg{
		scanID: 1,
		ch:     nil,
		evt:    domain.ScanEvent{Type: domain.ScanEventFile, Root: "/m", File: &fresh},
	})
	mp = updated.(ModelsPage)
	if len(mp.files) != 1 {
		t.Fatalf("fresh event dropped: files = %d, want 1", len(mp.files))
	}
	if mp.files[0].Name != "fresh.gguf" {
		t.Errorf("got %q, want fresh.gguf", mp.files[0].Name)
	}
}

func TestModelsPage_EmptyStateHintAfterScanComplete(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/models"})
	// Mark the root as scanned with zero results.
	page.statusMap["/models"] = pathStatus{state: "scanned"}
	out := page.View()
	if !strings.Contains(out, "No .gguf files") {
		t.Errorf("scanned-empty Models view missing hint; got:\n%s", out)
	}
}

func TestModelsPage_NoEmptyStateWhileScanning(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/models"})
	// statusMap initialized to "scanning" — empty hint must not show yet.
	out := page.View()
	if strings.Contains(out, "No .gguf files") {
		t.Errorf("scanning Models view should not show empty hint yet; got:\n%s", out)
	}
	if !strings.Contains(out, "Scanning configured paths") {
		t.Errorf("scanning Models view missing scanning notice; got:\n%s", out)
	}
}

// UIUX-011: zero files because a root failed to scan is an error artifact,
// not an empty library — the view must say the scan failed, never the
// misleading "No .gguf files" copy.
func TestModelsPage_EmptyStateWhenScanErrored(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/models"})
	page.statusMap["/models"] = pathStatus{state: "error", err: "permission denied"}
	out := page.View()
	if !strings.Contains(out, "Scan failed for one or more paths") {
		t.Errorf("error-root Models view missing scan-failed state; got:\n%s", out)
	}
	if strings.Contains(out, "No .gguf files") {
		t.Errorf("error-root Models view must not claim no files exist; got:\n%s", out)
	}
}

// UIUX-012: the filter prompt with its block cursor must appear the moment
// filter mode is entered, before the first character is typed.
func TestModelsPage_FilterPromptShowsCursorImmediately(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	page := NewModelsPage(&fakeScanner{}, []string{"/models"})
	page.statusMap["/models"] = pathStatus{state: "scanned"}
	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	page = updated.(ModelsPage)
	if !page.filterMode {
		t.Fatal("'/' did not enter filter mode")
	}
	out := page.View()
	if !strings.Contains(out, "filter: ") || !strings.Contains(out, "█") {
		t.Errorf("filter-mode view missing live prompt with cursor; got:\n%s", out)
	}
}

func TestModelsPage_EmptyStateWhenFilterNoMatches(t *testing.T) {
	mf := domain.ModelFile{
		Path:      "/tmp/models/qwen-32b.gguf",
		SizeBytes: 16_000_000_000,
		Name:      "qwen-32b.gguf",
		Quant:     "Q4_K_M",
		Params:    "32B",
	}
	scanner := &fakeScanner{events: []domain.ScanEvent{
		{Type: domain.ScanEventFile, Root: "/tmp/models", File: &mf},
		{Type: domain.ScanEventProgress, Root: "/tmp/models", Count: 1},
		{Type: domain.ScanEventDone},
	}}
	page := NewModelsPage(scanner, []string{"/tmp/models"})
	model := tea.Model(page)
	cmd := model.Init()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		var c tea.Cmd
		model, c = model.Update(msg)
		cmd = c
	}
	mp := model.(ModelsPage)
	mp.filter = "nonexistent"
	mp.refreshRows()
	out := mp.View()
	if !strings.Contains(out, "No models match the current filter") {
		t.Errorf("filter-empty Models view missing hint; got:\n%s", out)
	}
	if !strings.Contains(out, "Press [esc] to clear filter") {
		t.Errorf("filter-empty Models view missing action hint; got:\n%s", out)
	}
}

func TestModelsPage_RenderStatusWrapsWhenOverflow(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/a/very/long/root/path/one", "/another/long/root/path/two", "/third/root"})
	page.width = 60
	for _, p := range page.paths {
		page.statusMap[p] = pathStatus{state: "scanned", count: 3}
	}
	out := page.renderStatus()
	if !strings.Contains(out, "\n") {
		t.Errorf("narrow terminal renderStatus should wrap to multiple lines; got %q", out)
	}
}

func TestModelsPage_TruncFrontPreservesTail(t *testing.T) {
	got := truncFront("/very/long/path/to/the/big/dataset/folder", 20)
	if !strings.HasPrefix(got, "…") {
		t.Errorf("truncFront should start with …; got %q", got)
	}
	if !strings.HasSuffix(got, "/folder") {
		t.Errorf("truncFront should preserve tail; got %q", got)
	}
}

func TestModelsPage_RevealCopiesPathToClipboard(t *testing.T) {
	var captured string
	prev := clipboardWriter
	clipboardWriter = func(s string) error {
		captured = s
		return nil
	}
	t.Cleanup(func() { clipboardWriter = prev })

	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	updated, _ := page.commitRootAction("reveal", "/models/foo.gguf")
	mp := updated.(ModelsPage)

	if captured != "/models/foo.gguf" {
		t.Errorf("clipboard captured %q, want /models/foo.gguf", captured)
	}
	if !strings.Contains(mp.flash.Message(), "copied") {
		t.Errorf("flash %q missing 'copied'", mp.flash.Message())
	}
}

func TestModelsPage_RevealHandlesClipboardError(t *testing.T) {
	prev := clipboardWriter
	clipboardWriter = func(string) error { return errors.New("no display") }
	t.Cleanup(func() { clipboardWriter = prev })

	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	updated, _ := page.commitRootAction("reveal", "/models/foo.gguf")
	mp := updated.(ModelsPage)

	if !strings.Contains(mp.flash.Message(), "clipboard error") {
		t.Errorf("flash %q missing 'clipboard error'", mp.flash.Message())
	}
}

func TestModelsPage_HintsListPageKeys(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, nil)
	hints := page.Hints()
	for _, want := range []string{"[/]", "[R]", "[enter]", "[esc]"} {
		if !strings.Contains(hints, want) {
			t.Errorf("Hints missing %q; got %q", want, hints)
		}
	}
}

// TestModelsPage_FilterModeRescanKeyAppendsToFilter is a regression for the
// adversarial-review finding: typing uppercase 'R' inside filter mode used to
// match keys.Rescan and trigger a recursive filesystem scan. After the fix,
// runes (including 'R') must be appended to the filter buffer instead.
func TestModelsPage_FilterModeRescanKeyAppendsToFilter(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.filterMode = true
	page.scanID = 7

	updated, cmd := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	mp := updated.(ModelsPage)

	if mp.filter != "R" {
		t.Errorf("filter = %q, want %q", mp.filter, "R")
	}
	if mp.scanID != 7 {
		t.Errorf("scanID = %d, want 7 (rescan must NOT have fired)", mp.scanID)
	}
	if cmd != nil {
		// startScanCmd would be the only cmd this path produces; assert nil.
		t.Errorf("expected no cmd, got %T", cmd())
	}
}

// TestModelsPage_FilterAcceptsMultiRuneBurst is a regression for INPUT-01:
// fast typing / paste arrives as one KeyMsg carrying several runes. The old
// `len(Runes) == 1` guard dropped those, losing characters under speed.
func TestModelsPage_FilterAcceptsMultiRuneBurst(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.filterMode = true

	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Hunyuan")})
	mp := updated.(ModelsPage)

	if mp.filter != "Hunyuan" {
		t.Errorf("filter = %q, want %q (multi-rune burst must not be dropped)", mp.filter, "Hunyuan")
	}
}

// TestModelsPage_FilterAcceptsSpace is a regression for TUI_AUDIT F-02: the
// spacebar arrives as tea.KeySpace (not tea.KeyRunes) and used to be dropped,
// so a multi-word filter was impossible.
func TestModelsPage_FilterAcceptsSpace(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.filterMode = true

	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	updated, _ = updated.(ModelsPage).Update(tea.KeyMsg{Type: tea.KeySpace})
	updated, _ = updated.(ModelsPage).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	mp := updated.(ModelsPage)

	if mp.filter != "a b" {
		t.Errorf("filter = %q, want %q (space must not be dropped)", mp.filter, "a b")
	}
}

func TestModelsPage_IsCapturingInput(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, nil)

	if page.IsCapturingInput() {
		t.Error("expected IsCapturingInput=false when idle")
	}

	page.filterMode = true
	if !page.IsCapturingInput() {
		t.Error("expected IsCapturingInput=true when filterMode is active")
	}

	page.filterMode = false
	page.action = &actionMenu{}
	if !page.IsCapturingInput() {
		t.Error("expected IsCapturingInput=true when action menu is open")
	}
}

func TestModelsPage_Reload(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m1", "/m2"})
	page.files = []domain.ModelFile{{Path: "/m1/a.gguf", Name: "a.gguf"}}
	page.statusMap["/m1"] = pathStatus{state: "scanned", count: 1}
	page.statusMap["/m2"] = pathStatus{state: "error", err: "boom"}
	page.scanID = 4

	cmd := page.Reload()
	if cmd == nil {
		t.Fatal("Reload returned nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(modelsReloadMsg); !ok {
		t.Fatalf("Reload cmd produced %T, want modelsReloadMsg", msg)
	}

	updated, batched := page.Update(msg)
	if batched == nil {
		t.Fatal("handling modelsReloadMsg returned nil cmd; expected a rescan batch")
	}
	mp := updated.(ModelsPage)
	if mp.scanID != 5 {
		t.Errorf("scanID = %d, want 5 (bumped by reload)", mp.scanID)
	}
	if len(mp.files) != 0 {
		t.Errorf("files = %d, want 0 (cleared by reload)", len(mp.files))
	}
	for _, root := range []string{"/m1", "/m2"} {
		if mp.statusMap[root].state != "scanning" {
			t.Errorf("statusMap[%q].state = %q, want scanning", root, mp.statusMap[root].state)
		}
	}
	if mp.flash.Message() != "" {
		t.Errorf("flash = %q, want empty (silent reload)", mp.flash.Message())
	}
}

func TestModelsPage_RescanGuard(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.scanID = 2
	// Simulate an in-progress scan: statusMap shows "scanning".
	page.statusMap["/m"] = pathStatus{state: "scanning"}

	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	mp := updated.(ModelsPage)

	if mp.scanID != 2 {
		t.Errorf("scanID = %d, want 2 (must NOT bump while scanning)", mp.scanID)
	}
	if mp.statusMap["/m"].state != "scanning" {
		t.Errorf("status state = %q, want scanning", mp.statusMap["/m"].state)
	}
	// cmd may be a flash clear timer — that's fine; the point is scanID didn't bump.
	if mp.scanID != 2 {
		t.Errorf("scanID = %d, want 2 (must NOT bump while scanning)", mp.scanID)
	}
	if !strings.Contains(mp.flash.Message(), "already in progress") {
		t.Errorf("flash = %q, want 'already in progress'", mp.flash.Message())
	}
}

func TestModelsPage_RescanKeyShowsFlash(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.scanID = 2
	// Mark root scanned so the rescan guard allows a fresh scan.
	page.statusMap["/m"] = pathStatus{state: "scanned"}

	updated, cmd := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if cmd == nil {
		t.Fatal("R key returned nil cmd")
	}
	mp := updated.(ModelsPage)
	if mp.scanID != 3 {
		t.Errorf("scanID = %d, want 3 (bumped by R)", mp.scanID)
	}
	if mp.flash.Message() != "rescan started" {
		t.Errorf("flash = %q, want \"rescan started\"", mp.flash.Message())
	}
}

func TestModelsPage_ExistingActionOpensProfilePicker(t *testing.T) {
	store := &fakeModelsStore{profiles: []domain.Profile{
		{ID: "alpha", Name: "Alpha", Launch: domain.LaunchConfig{BackendID: "cpu"}},
		{ID: "bravo", Name: "Bravo", Launch: domain.LaunchConfig{BackendID: "cuda"}},
	}}
	page := NewModelsPage(&fakeScanner{}, []string{"/m"}).WithProfileStore(store)

	updated, cmd := page.commitRootAction("existing", "/models/foo.gguf")
	if cmd != nil {
		t.Errorf("expected nil cmd when opening picker, got non-nil")
	}
	mp := updated.(ModelsPage)
	if mp.profilePicker == nil {
		t.Fatal("expected profilePicker to be non-nil after 'existing' action")
	}
	if mp.profilePickerTargetPath != "/models/foo.gguf" {
		t.Errorf("profilePickerTargetPath = %q, want /models/foo.gguf", mp.profilePickerTargetPath)
	}
	if mp.action != nil {
		t.Error("expected action menu to be cleared when picker opens")
	}
}

func TestModelsPage_ExistingActionFlashesWhenNoProfiles(t *testing.T) {
	store := &fakeModelsStore{profiles: nil}
	page := NewModelsPage(&fakeScanner{}, []string{"/m"}).WithProfileStore(store)

	updated, _ := page.commitRootAction("existing", "/models/foo.gguf")
	mp := updated.(ModelsPage)
	if mp.profilePicker != nil {
		t.Error("expected no picker when profile list is empty")
	}
	if !strings.Contains(mp.flash.Message(), "no existing profiles") {
		t.Errorf("flash = %q, want 'no existing profiles ...'", mp.flash.Message())
	}
}

func TestModelsPage_ExistingActionFlashesOnListError(t *testing.T) {
	store := &fakeModelsStore{listErr: errors.New("disk dead")}
	page := NewModelsPage(&fakeScanner{}, []string{"/m"}).WithProfileStore(store)

	updated, _ := page.commitRootAction("existing", "/models/foo.gguf")
	mp := updated.(ModelsPage)
	if mp.profilePicker != nil {
		t.Error("expected no picker when List returns error")
	}
	if !strings.Contains(mp.flash.Message(), "load profiles:") {
		t.Errorf("flash = %q, want 'load profiles: ...'", mp.flash.Message())
	}
}

func TestModelsPage_ProfilePickedMsgUpdatesProfileModel(t *testing.T) {
	store := &fakeModelsStore{profiles: []domain.Profile{
		{ID: "alpha", Name: "Alpha", Model: "/old.gguf"},
	}}
	page := NewModelsPage(&fakeScanner{}, []string{"/m"}).WithProfileStore(store)
	picker := components.NewProfilePicker(store.profiles)
	page.profilePicker = &picker
	page.profilePickerTargetPath = "/models/new.gguf"

	updated, _ := page.Update(components.ProfilePickedMsg{ID: "alpha"})
	mp := updated.(ModelsPage)

	if mp.profilePicker != nil {
		t.Error("expected profilePicker cleared after pick")
	}
	if mp.profilePickerTargetPath != "" {
		t.Errorf("profilePickerTargetPath = %q, want empty", mp.profilePickerTargetPath)
	}
	if store.saved == nil {
		t.Fatal("expected store.Save to be called")
	}
	if store.saved.ID != "alpha" {
		t.Errorf("saved.ID = %q, want alpha", store.saved.ID)
	}
	if store.saved.Model != "/models/new.gguf" {
		t.Errorf("saved.Model = %q, want /models/new.gguf", store.saved.Model)
	}
	if !strings.Contains(mp.flash.Message(), "updated alpha") {
		t.Errorf("flash = %q, want 'updated alpha'", mp.flash.Message())
	}
}

func TestModelsPage_ProfilePickedMsgFlashesOnGetError(t *testing.T) {
	store := &fakeModelsStore{getErr: errors.New("read fail")}
	page := NewModelsPage(&fakeScanner{}, []string{"/m"}).WithProfileStore(store)
	picker := components.NewProfilePicker(nil)
	page.profilePicker = &picker
	page.profilePickerTargetPath = "/models/new.gguf"

	updated, _ := page.Update(components.ProfilePickedMsg{ID: "alpha"})
	mp := updated.(ModelsPage)
	if mp.profilePicker != nil {
		t.Error("expected profilePicker cleared even on Get error")
	}
	if !strings.Contains(mp.flash.Message(), "load profile:") {
		t.Errorf("flash = %q, want 'load profile: ...'", mp.flash.Message())
	}
}

func TestModelsPage_ProfilePickedMsgFlashesOnSaveError(t *testing.T) {
	store := &fakeModelsStore{
		profiles: []domain.Profile{{ID: "alpha", Name: "Alpha"}},
		saveErr:  errors.New("disk full"),
	}
	page := NewModelsPage(&fakeScanner{}, []string{"/m"}).WithProfileStore(store)
	picker := components.NewProfilePicker(store.profiles)
	page.profilePicker = &picker
	page.profilePickerTargetPath = "/models/new.gguf"

	updated, _ := page.Update(components.ProfilePickedMsg{ID: "alpha"})
	mp := updated.(ModelsPage)
	if !strings.Contains(mp.flash.Message(), "save profile:") {
		t.Errorf("flash = %q, want 'save profile: ...'", mp.flash.Message())
	}
}

func TestModelsPage_ProfilePickerCancelledClearsPicker(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	picker := components.NewProfilePicker([]domain.Profile{{ID: "a", Name: "A"}})
	page.profilePicker = &picker
	page.profilePickerTargetPath = "/models/foo.gguf"

	updated, cmd := page.Update(components.ProfilePickerCancelledMsg{})
	mp := updated.(ModelsPage)
	if mp.profilePicker != nil {
		t.Error("expected profilePicker cleared on cancel")
	}
	if mp.profilePickerTargetPath != "" {
		t.Errorf("profilePickerTargetPath = %q, want empty", mp.profilePickerTargetPath)
	}
	if cmd != nil {
		t.Errorf("expected nil cmd on cancel, got non-nil")
	}
}

func TestModelsPage_IsCapturingInputWhilePickerActive(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, nil)
	if page.IsCapturingInput() {
		t.Fatal("expected IsCapturingInput=false when idle")
	}
	picker := components.NewProfilePicker(nil)
	page.profilePicker = &picker
	if !page.IsCapturingInput() {
		t.Error("expected IsCapturingInput=true while profilePicker is active")
	}
}

func TestModelsPage_KeyForwardedToPicker(t *testing.T) {
	store := &fakeModelsStore{profiles: []domain.Profile{
		{ID: "alpha", Name: "Alpha"},
		{ID: "bravo", Name: "Bravo"},
	}}
	page := NewModelsPage(&fakeScanner{}, []string{"/m"}).WithProfileStore(store)
	picker := components.NewProfilePicker(store.profiles)
	page.profilePicker = &picker
	page.profilePickerTargetPath = "/models/foo.gguf"

	updated, cmd := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mp := updated.(ModelsPage)
	if mp.profilePicker == nil {
		t.Fatal("picker should still be set until message round-trips")
	}
	if cmd == nil {
		t.Fatal("expected ProfilePickedMsg cmd from picker")
	}
	msg := cmd()
	picked, ok := msg.(components.ProfilePickedMsg)
	if !ok {
		t.Fatalf("forwarded key produced %T, want ProfilePickedMsg", msg)
	}
	if picked.ID != "alpha" {
		t.Errorf("picked.ID = %q, want alpha (first item)", picked.ID)
	}
}

func TestModelsPage_ViewRendersPickerWhenActive(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	picker := components.NewProfilePicker([]domain.Profile{{ID: "alpha", Name: "Alpha 7B"}})
	page.profilePicker = &picker
	out := page.View()
	if !strings.Contains(out, "Pick a profile") {
		t.Errorf("expected picker title in view; got:\n%s", out)
	}
	if !strings.Contains(out, "Alpha 7B") {
		t.Errorf("expected profile row in view; got:\n%s", out)
	}
}

func TestModelsPage_HintsWhilePickerActive(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, nil)
	picker := components.NewProfilePicker(nil)
	page.profilePicker = &picker
	hints := page.Hints()
	for _, want := range []string{"[↑↓]", "[enter]", "[esc]"} {
		if !strings.Contains(hints, want) {
			t.Errorf("picker hints missing %q; got %q", want, hints)
		}
	}
}

func TestModelsPage_DeleteOptionInActionMenu(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.files = []domain.ModelFile{{Path: "/m/foo.gguf", Name: "foo.gguf"}}
	page.refreshRows()

	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mp := updated.(ModelsPage)
	if mp.action == nil {
		t.Fatal("expected action menu to open")
	}
	found := false
	for _, opt := range mp.action.options {
		if opt.value == "delete" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("action menu missing 'delete' option; got %v", mp.action.options)
	}
}

func TestModelsPage_DeleteRemovesFileAndRow(t *testing.T) {
	prev := fileRemover
	fileRemover = func(string) error { return nil }
	t.Cleanup(func() { fileRemover = prev })

	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.files = []domain.ModelFile{
		{Path: "/m/foo.gguf", Name: "foo.gguf"},
		{Path: "/m/bar.gguf", Name: "bar.gguf"},
	}
	page.refreshRows()

	updated, _ := page.commitRootAction("delete", "/m/foo.gguf")
	mp := updated.(ModelsPage)
	if mp.action != nil {
		t.Error("expected action menu cleared")
	}
	if !mp.deleteConfirm.Active() {
		t.Fatal("expected delete confirm to be active")
	}

	updated2, _ := mp.Update(modelDeleteConfirmedMsg{path: "/m/foo.gguf"})
	mp2 := updated2.(ModelsPage)

	if len(mp2.files) != 1 {
		t.Fatalf("files = %d, want 1", len(mp2.files))
	}
	if mp2.files[0].Name != "bar.gguf" {
		t.Errorf("remaining file = %q, want bar.gguf", mp2.files[0].Name)
	}
	if len(mp2.table.Rows()) != 1 {
		t.Errorf("rows = %d, want 1", len(mp2.table.Rows()))
	}
	if !strings.Contains(mp2.flash.Message(), "deleted") {
		t.Errorf("flash = %q, want 'deleted ...'", mp2.flash.Message())
	}
}

func TestModelsPage_DeleteCancelKeepsFile(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.files = []domain.ModelFile{{Path: "/m/foo.gguf", Name: "foo.gguf"}}
	page.refreshRows()

	updated, _ := page.commitRootAction("delete", "/m/foo.gguf")
	mp := updated.(ModelsPage)

	updated2, _ := mp.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mp2 := updated2.(ModelsPage)

	if mp2.deleteConfirm.Active() {
		t.Error("expected delete confirm to be inactive after cancel")
	}
	if len(mp2.files) != 1 {
		t.Errorf("files = %d, want 1 (must not remove on cancel)", len(mp2.files))
	}
	if mp2.flash.Message() != "" {
		t.Errorf("flash = %q, want empty after confirm-owned cancel", mp2.flash.Message())
	}
}

func TestModelsPage_DeleteErrorShowsFlash(t *testing.T) {
	prev := fileRemover
	fileRemover = func(string) error { return errors.New("permission denied") }
	t.Cleanup(func() { fileRemover = prev })

	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.files = []domain.ModelFile{{Path: "/m/foo.gguf", Name: "foo.gguf"}}

	updated, _ := page.Update(modelDeleteConfirmedMsg{path: "/m/foo.gguf"})
	mp := updated.(ModelsPage)

	if len(mp.files) != 1 {
		t.Errorf("files = %d, want 1 (must not remove on error)", len(mp.files))
	}
	if !strings.Contains(mp.flash.Message(), "delete failed") {
		t.Errorf("flash = %q, want 'delete failed: ...'", mp.flash.Message())
	}
}

func TestModelsPage_IsCapturingInputDuringDeleteConfirm(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, nil)
	page.deleteConfirm = components.NewConfirm("Delete?", "path", nil, "", "")
	if !page.IsCapturingInput() {
		t.Error("expected IsCapturingInput=true when deleteConfirm is active")
	}
}

func TestModelsPage_ViewRendersDeleteConfirmWhenActive(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.deleteConfirm = components.NewConfirm("Delete foo.gguf?", "path", nil, "", "")
	cmd := page.deleteConfirm.Init()
	if cmd != nil {
		cmd()
	}
	out := page.View()
	if !strings.Contains(out, "Delete foo.gguf?") {
		t.Errorf("expected confirm title in view; got:\n%s", out)
	}
}

func TestModelsPage_HintsWhileDeleteConfirmActive(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, nil)
	page.deleteConfirm = components.NewConfirm("Delete?", "path", nil, "", "")
	hints := page.Hints()
	for _, want := range []string{"[enter]", "[esc]"} {
		if !strings.Contains(hints, want) {
			t.Errorf("delete confirm hints missing %q; got %q", want, hints)
		}
	}
}

// TestModelsPage_DeleteConfirmEndToEnd drives the full keypress pipeline
// through the huh.Form-backed Confirm without bypassing via direct
// modelDeleteConfirmedMsg injection. Guards against the regression where
// ModelsPage.Update dropped non-tea.KeyMsg messages so the inner form
// never reached StateCompleted and onYes never fired (delete silently
// did nothing).
func TestModelsPage_DeleteConfirmEndToEnd(t *testing.T) {
	var removed string
	prev := fileRemover
	fileRemover = func(p string) error { removed = p; return nil }
	t.Cleanup(func() { fileRemover = prev })

	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.files = []domain.ModelFile{
		{Path: "/m/foo.gguf", Name: "foo.gguf"},
		{Path: "/m/bar.gguf", Name: "bar.gguf"},
	}
	page.refreshRows()

	// Open confirm via the same path the action menu uses.
	openedI, initCmd := page.commitRootAction("delete", "/m/foo.gguf")
	opened := openedI.(ModelsPage)
	if !opened.deleteConfirm.Active() {
		t.Fatal("expected deleteConfirm active after commitRootAction")
	}

	// Drain init Cmd → non-KeyMsg → must reach the form via forwardNonKey.
	drainI := tea.Model(opened)
	if initCmd != nil {
		if m := initCmd(); m != nil {
			d, _ := drainI.Update(m)
			drainI = d
		}
	}
	drained := drainI.(ModelsPage)

	// Cursor defaults to Negative ("Cancel"). Press Left to flip to
	// affirmative ("Delete"), then drain any follow-up Cmd that comes back.
	leftI, leftCmd := drained.Update(tea.KeyMsg{Type: tea.KeyLeft})
	left := leftI.(ModelsPage)
	if leftCmd != nil {
		if m := leftCmd(); m != nil {
			l2, _ := left.Update(m)
			left = l2.(ModelsPage)
		}
	}

	// Submit with Enter. huh.Form may emit multiple internal transition
	// msgs (nextFieldMsg, nextGroupMsg) before reaching StateCompleted,
	// so drain Cmds back through Update until either
	// modelDeleteConfirmedMsg appears or we hit a sanity cap.
	cur := tea.Model(left)
	cmd := tea.Cmd(func() tea.Msg { return tea.KeyMsg{Type: tea.KeyEnter} })
	var deleteMsg tea.Msg
	for i := 0; i < 16 && cmd != nil; i++ {
		m := cmd()
		if m == nil {
			break
		}
		if _, ok := m.(modelDeleteConfirmedMsg); ok {
			deleteMsg = m
			break
		}
		cur, cmd = cur.Update(m)
	}
	if deleteMsg == nil {
		t.Fatal("modelDeleteConfirmedMsg never produced — pipeline still broken")
	}

	// Replay the message back through Update — the page's own handler
	// performs the disk remove and table refresh.
	finalI, _ := cur.Update(deleteMsg)
	final := finalI.(ModelsPage)

	if removed != "/m/foo.gguf" {
		t.Errorf("fileRemover called with %q, want /m/foo.gguf", removed)
	}
	if len(final.files) != 1 || final.files[0].Name != "bar.gguf" {
		t.Errorf("files post-delete = %+v, want only bar.gguf", final.files)
	}
	if !strings.Contains(final.flash.Message(), "deleted") {
		t.Errorf("flash = %q, want 'deleted ...'", final.flash.Message())
	}
}

// TestModelsPage_ActionModalOpaqueBackground is the F-03 audit regression
// guard: with the action menu open at 120×40, only the file referenced by
// the modal title may appear in the rendered output. Any other .gguf row
// from the table indicates the body bled around the modal frame.
func TestModelsPage_ActionModalOpaqueBackground(t *testing.T) {
	target := domain.ModelFile{Path: "/m/Hunyuan-MT-7B.Q4_K_S.gguf", Name: "Hunyuan-MT-7B.Q4_K_S.gguf"}
	background := domain.ModelFile{Path: "/m/background-leak-canary.gguf", Name: "background-leak-canary.gguf"}
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.width = 120
	page.height = 40
	page.files = []domain.ModelFile{target, background}
	page.refreshRows()

	page.action = &actionMenu{
		title:      "Action for " + target.Name,
		options:    []actionOption{{label: "Use in new profile", value: "new"}},
		targetPath: target.Path,
		stage:      actionStageRoot,
	}

	out := page.View()
	if strings.Contains(out, background.Name) {
		t.Fatalf("background filename %q bled through opaque action modal\noutput:\n%s", background.Name, out)
	}
	if !strings.Contains(out, target.Name) {
		t.Fatalf("modal title with target filename %q missing from output\noutput:\n%s", target.Name, out)
	}
	lines := strings.Split(out, "\n")
	if got := len(lines); got != page.height {
		t.Fatalf("rendered height = %d lines, want %d (lipgloss.Place canvas)", got, page.height)
	}
}

// TestModelsPage_ActionModalCoversFullCanvas asserts that renderActionMenu
// emits a string sized to p.width × p.height. Without the Place wrap the
// result would be a small box and components.Overlay (or the direct return
// in View) would leak the body underneath.
func TestModelsPage_ActionModalCoversFullCanvas(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.width = 120
	page.height = 40
	page.action = &actionMenu{
		title:   "Action for some.gguf",
		options: []actionOption{{label: "Use in new profile", value: "new"}},
		stage:   actionStageRoot,
	}
	out := page.renderActionMenu()
	lines := strings.Split(out, "\n")
	if len(lines) != page.height {
		t.Fatalf("renderActionMenu height = %d lines, want %d", len(lines), page.height)
	}
	if w := lipgloss.Width(lines[0]); w != page.width {
		t.Fatalf("renderActionMenu width = %d cols, want %d", w, page.width)
	}
}

// F-02 regression: pressing `i` while the info panel is open must close it
// (toggle behaviour). Previously the handler was monotonic-open.
func TestModelsPage_IKeyTogglesInfoPanel(t *testing.T) {
	mf := domain.ModelFile{Path: "/m/q.gguf", Name: "q.gguf"}
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.store = &fakeModelsStore{}
	page.files = []domain.ModelFile{mf}
	page.refreshRows()

	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	mp := updated.(ModelsPage)
	if mp.infoPanel == nil {
		t.Fatal("first `i` did not open info panel")
	}

	updated, _ = mp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	mp = updated.(ModelsPage)
	if mp.infoPanel != nil {
		t.Fatal("second `i` did not close info panel")
	}
}

// F-02 regression: Esc must close an open info panel.
func TestModelsPage_EscClosesInfoPanel(t *testing.T) {
	mf := domain.ModelFile{Path: "/m/q.gguf", Name: "q.gguf"}
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.store = &fakeModelsStore{}
	page.files = []domain.ModelFile{mf}
	page.refreshRows()

	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	mp := updated.(ModelsPage)
	if mp.infoPanel == nil {
		t.Fatal("info panel did not open")
	}
	updated, _ = mp.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mp = updated.(ModelsPage)
	if mp.infoPanel != nil {
		t.Fatal("Esc did not close info panel")
	}
}

// TUI_AUDIT bonus regression: while the info panel is open the page MUST
// claim input. Earlier the panel was "read-only, doesn't capture" so
// global tab shortcuts stayed alive, BUT that also meant the global `esc`
// no-op in root.go ate the keystroke before ModelsPage's keys.Cancel
// handler could close the panel — making `esc` to close the info panel a
// dead key. The user closes the panel with `esc` or `i` before switching
// tabs, matching the convention of every other in-page overlay (web-edit
// modal, confirm dialogs, profile picker).
func TestModelsPage_InfoPanelCapturesInputSoEscCloses(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, nil)
	page.infoPanel = &components.InfoPanel{}
	if !page.IsCapturingInput() {
		t.Error("info panel must mark page as capturing input so esc reaches the page handler")
	}
}

// TUI-RESP Step 6: resizeColumns flex floor lowered so narrow terminals (≤56 cols)
// keep the rightmost Path column readable instead of inflating it to a hard 32-col
// minimum. The Path column may still be clipped by ClampBody below ~56 cols —
// that is the accepted degradation per the plan.
func TestModelsPage_ResizeColumnsRespectsLoweredFloor(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.resizeColumns(40)
	if page.nameColW < 8 {
		t.Errorf("nameColW = %d, want >= 8", page.nameColW)
	}
	if page.pathColW < 8 {
		t.Errorf("pathColW = %d, want >= 8", page.pathColW)
	}
	// Total flexed columns plus the fixed 10+10+8 = 28 plus 12 (padding) must
	// never exceed what a 40-col terminal can hold.
	sum := page.nameColW + page.pathColW + 28 + 12
	if sum > 88 {
		t.Errorf("sum = %d, want <= 88 (40 cols + slack)", sum)
	}
}

// TUI-RESP Step 6: renderBar(0, 0) and renderBar(0.5, 0) must return ""
// instead of panicking on a negative-width strings.Repeat. The width<1 guard
// added by the panic-fix in renderBar keeps the Downloads section safe when
// the terminal is too narrow to render a bar.
func TestModelsPage_RenderBarPanicGuard(t *testing.T) {
	if got := renderBar(0.5, 0); got != "" {
		t.Errorf("renderBar(0.5, 0) = %q, want empty", got)
	}
	if got := renderBar(0.5, -1); got != "" {
		t.Errorf("renderBar(0.5, -1) = %q, want empty", got)
	}
}

// TUI-RESP Step 6: renderDownloadRow for a failed download with a 100-char
// error string at a narrow width must not panic and must truncate the error
// tail with "…" so it never blows past the right edge of the terminal.
func TestModelsPage_RenderDownloadRowTruncatesFailedError(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	st := downloadmgr.State{
		Status: downloadmgr.StatusFailed,
		Spec:   downloadmgr.Spec{Filename: "big.gguf"},
		Err:    errors.New(strings.Repeat("E", 100)),
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("renderDownloadRow panicked at width=20: %v", r)
		}
	}()
	out := page.renderDownloadRow(st, false, 20)
	if !strings.Contains(out, "…") {
		t.Errorf("failed-download row missing ellipsis tail at width=20; got:\n%s", out)
	}
}

// TUI-RESP Step 6: relayout() budgets the table against body height while
// honouring the stacked info-panel state at narrow widths. With height=12 and
// the info panel open, the body height -6 chrome halves to 3 rows so the
// table does not push the panel off-screen.
func TestModelsPage_RelayoutStacksInfoPanelBelowNarrow(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.files = []domain.ModelFile{{Path: "/m/a.gguf", Name: "a.gguf"}}
	page.refreshRows()
	page.height = 12
	page.width = 80 // below NarrowWidthThreshold
	page.infoPanel = &components.InfoPanel{Filename: "a.gguf", Path: "/m/a.gguf"}
	page.relayout()
	// h = 12 - 6 = 6; info panel open + width<100 -> h /= 2 -> 3. The bubbles
	// table.SetHeight subtracts 1 row for the column header, so the internal
	// viewport height is 2.
	if got := page.table.Height(); got != 2 {
		t.Errorf("table.Height() = %d, want 2 (stacked info panel budget minus header)", got)
	}
}

// TUI-RESP Step 6: relayout() at the wide branch leaves the table using the
// full body budget (height - 6); only the narrow branch halves for the
// stacked info panel.
func TestModelsPage_RelayoutWideKeepsFullBudget(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.files = []domain.ModelFile{{Path: "/m/a.gguf", Name: "a.gguf"}}
	page.refreshRows()
	page.height = 30
	page.width = 160 // above NarrowWidthThreshold
	page.infoPanel = &components.InfoPanel{Filename: "a.gguf", Path: "/m/a.gguf"}
	page.relayout()
	// h = 30 - 6 = 24; wide so no half. table.SetHeight subtracts 1 for the
	// header row, so internal viewport = 23.
	if got := page.table.Height(); got != 23 {
		t.Errorf("table.Height() = %d, want 23 (full budget minus header)", got)
	}
}

func TestModelsPage_CursorMarkerTracksSelection(t *testing.T) {
	page := NewModelsPage(nil, nil)
	page.files = []domain.ModelFile{
		{Path: "/m/a.gguf", Name: "a.gguf"},
		{Path: "/m/b.gguf", Name: "b.gguf"},
		{Path: "/m/c.gguf", Name: "c.gguf"},
	}
	page.refreshRows()
	if got := page.table.Rows()[0][0]; got != "> " {
		t.Fatalf("first row marker = %q, want %q", got, "> ")
	}
	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyDown})
	page = updated.(ModelsPage)
	if got := page.table.Rows()[0][0]; got != "  " {
		t.Errorf("first row marker after down = %q, want two spaces", got)
	}
	if got := page.table.Rows()[1][0]; got != "> " {
		t.Errorf("second row marker after down = %q, want %q", got, "> ")
	}
}
