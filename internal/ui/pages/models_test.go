package pages

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
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
	if !strings.Contains(out, "no .gguf files") {
		t.Errorf("scanned-empty Models view missing hint; got:\n%s", out)
	}
}

func TestModelsPage_NoEmptyStateWhileScanning(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/models"})
	// statusMap initialized to "scanning" — empty hint must not show yet.
	out := page.View()
	if strings.Contains(out, "no .gguf files") {
		t.Errorf("scanning Models view should not show empty hint yet; got:\n%s", out)
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

func TestModelsPage_RescanKeyShowsFlash(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.scanID = 2

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
