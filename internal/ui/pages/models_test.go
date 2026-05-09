package pages

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
)

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
	if !strings.Contains(mp.flash, "copied") {
		t.Errorf("flash %q missing 'copied'", mp.flash)
	}
}

func TestModelsPage_RevealHandlesClipboardError(t *testing.T) {
	prev := clipboardWriter
	clipboardWriter = func(string) error { return errors.New("no display") }
	t.Cleanup(func() { clipboardWriter = prev })

	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	updated, _ := page.commitRootAction("reveal", "/models/foo.gguf")
	mp := updated.(ModelsPage)

	if !strings.Contains(mp.flash, "clipboard error") {
		t.Errorf("flash %q missing 'clipboard error'", mp.flash)
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
