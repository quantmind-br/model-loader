package pages

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/quantmind-br/model-loader/internal/service/hfhub"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

func TestModelsPage_SubViewNavigationCycles(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, nil)
	if page.subView != mvLibrary {
		t.Fatalf("default subView = %v, want mvLibrary", page.subView)
	}

	// → advances Library → Downloads → Discover → Library.
	want := []modelsSubView{mvDownloads, mvDiscover, mvLibrary}
	cur := tea.Model(page)
	for i, w := range want {
		updated, _ := cur.Update(tea.KeyMsg{Type: tea.KeyRight})
		mp := updated.(ModelsPage)
		if mp.subView != w {
			t.Fatalf("after %d × right: subView = %v, want %v", i+1, mp.subView, w)
		}
		cur = mp
	}

	// ← from Library wraps back to Discover.
	updated, _ := cur.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if mp := updated.(ModelsPage); mp.subView != mvDiscover {
		t.Fatalf("left from Library: subView = %v, want mvDiscover", mp.subView)
	}
}

func TestModelsPage_SubViewTabsHaveColorlessActiveMarker(t *testing.T) {
	t.Cleanup(theme.RebuildStyles)
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()

	page := NewModelsPage(&fakeScanner{}, nil)
	if out := page.View(); !strings.Contains(out, "[Library]") {
		t.Fatalf("Library is not bracketed under NO_COLOR:\n%s", out)
	}
	page.subView = mvDownloads
	out := page.View()
	if !strings.Contains(out, "[Downloads") || strings.Contains(out, "[Library]") {
		t.Fatalf("Downloads is not the sole active marker under NO_COLOR:\n%s", out)
	}
}

func TestModelsPage_RightDoesNotSwitchWhileInfoPanelOpen(t *testing.T) {
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

	updated, _ = mp.Update(tea.KeyMsg{Type: tea.KeyRight})
	mp = updated.(ModelsPage)
	if mp.subView != mvLibrary {
		t.Errorf("right with info panel open switched section to %v; want mvLibrary (sizing jump instead)", mp.subView)
	}
}

func TestModelsPage_DownloadsSectionDoesNotCaptureInput(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, nil)
	page.subView = mvDownloads
	if page.IsCapturingInput() {
		t.Error("Downloads section must NOT capture input (uses only non-global keys)")
	}
}

func TestModelsPage_DiscoverSearchCapturesInput(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, nil).WithHFClient(hfhub.NewClient(nil, "test"))
	updated, _ := page.openHFSearch()
	mp := updated.(ModelsPage)
	if mp.subView != mvDiscover {
		t.Fatalf("openHFSearch subView = %v, want mvDiscover", mp.subView)
	}
	if mp.hfSearch == nil {
		t.Fatal("openHFSearch did not create the search picker")
	}
	if !mp.IsCapturingInput() {
		t.Error("active HF search must capture input")
	}
}

func TestModelsPage_OpenHFSearchWithoutClientFlashes(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, nil)
	updated, _ := page.openHFSearch()
	mp := updated.(ModelsPage)
	if mp.hfSearch != nil {
		t.Error("search picker must not open without an HF client")
	}
	if !strings.Contains(mp.flash.Message(), "not available") {
		t.Errorf("flash = %q, want 'not available'", mp.flash.Message())
	}
}

func TestModelsPage_SubTabsRendered(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.statusMap["/m"] = pathStatus{state: "scanned"}
	out := page.View()
	for _, want := range []string{"Library", "Downloads", "Discover"} {
		if !strings.Contains(out, want) {
			t.Errorf("View missing section tab %q; got:\n%s", want, out)
		}
	}
}

func TestModelsPage_DownloadsViewWithoutManager(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, nil)
	page.subView = mvDownloads
	out := page.View()
	if !strings.Contains(out, "not available") {
		t.Errorf("Downloads view without manager should explain it is unavailable; got:\n%s", out)
	}
}

func TestModelsPage_DownloadsKeyFocusSafeWithoutManager(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, nil)
	page.subView = mvDownloads
	// up/down must not panic and focus stays clamped at 0 with no downloads.
	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyDown})
	mp := updated.(ModelsPage)
	if mp.dlFocus != 0 {
		t.Errorf("dlFocus = %d, want 0 with no downloads", mp.dlFocus)
	}
}

func TestModelsPage_ClearDoneKeyDeletesHistoryFromDisk(t *testing.T) {
	dir := t.TempDir()
	id := downloadmgr.ID("rec-done")
	if err := downloadmgr.SaveRecord(dir, downloadmgr.DownloadRecord{
		ID:       id,
		URL:      "http://x",
		DestFile: filepath.Join(dir, "out.gguf"),
		Status:   downloadmgr.StatusCompleted,
	}); err != nil {
		t.Fatal(err)
	}

	mgr := downloadmgr.NewManager(dir, 1)
	page := NewModelsPage(&fakeScanner{}, nil).WithDownloadManager(mgr)
	page.subView = mvDownloads

	// [C] now opens a confirm (default Cancel) instead of clearing immediately
	// so a slipped Shift on the reversible [c] hide-done can't wipe history
	// (DESTRUCT-02).
	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	mp := updated.(ModelsPage)
	if !mp.clearDoneConfirm.Active() {
		t.Fatal("expected clear-done confirm to be active after [C]")
	}
	if !mp.IsCapturingInput() {
		t.Fatal("page should capture input while the clear-done confirm is open")
	}
	// Nothing is cleared until the user confirms.
	if _, err := downloadmgr.LoadRecord(downloadmgr.StatePath(dir, id)); err != nil {
		t.Errorf("record should still exist before confirmation: %v", err)
	}

	// Affirmative completion: mirror Confirm.Update clearing the form and
	// inject the msg its onYes callback would emit.
	mp.clearDoneConfirm = components.Confirm{}
	updated, _ = mp.Update(downloadClearConfirmedMsg{})
	mp = updated.(ModelsPage)

	// The finished record is gone from disk — it will NOT come back on restart.
	if _, err := downloadmgr.LoadRecord(downloadmgr.StatePath(dir, id)); err == nil {
		t.Error("completed download still on disk after confirmed clear")
	}
	if !strings.Contains(mp.flash.Message(), "cleared 1") {
		t.Errorf("flash = %q, want it to confirm 'cleared 1'", mp.flash.Message())
	}
}

func TestModelsPage_HintsPerSection(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, nil)

	page.subView = mvDownloads
	if h := page.Hints(); !strings.Contains(h, "[x]") || !strings.Contains(h, "[←/→]") {
		t.Errorf("Downloads hints missing keys; got %q", h)
	}

	page.subView = mvDiscover
	if h := page.Hints(); !strings.Contains(h, "search") {
		t.Errorf("Discover hints missing search; got %q", h)
	}
}

func TestFmtBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{5 * (1 << 30), "5.00 GB"},
		{1536 * (1 << 20), "1.50 GB"},
		{700 * (1 << 20), "700.0 MB"},
	}
	for _, c := range cases {
		if got := fmtBytes(c.in); got != c.want {
			t.Errorf("fmtBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFmtSpeed(t *testing.T) {
	if got := fmtSpeed(0); got != "-- MB/s" {
		t.Errorf("fmtSpeed(0) = %q, want '-- MB/s'", got)
	}
	if got := fmtSpeed(10 * (1 << 20)); got != "10.0 MB/s" {
		t.Errorf("fmtSpeed(10MB) = %q, want '10.0 MB/s'", got)
	}
}

func TestFmtETA(t *testing.T) {
	// 100 MB remaining at 10 MB/s = 10s.
	if got := fmtETA(0, 100*(1<<20), 10*(1<<20)); got != "0:10" {
		t.Errorf("fmtETA = %q, want '0:10'", got)
	}
	if got := fmtETA(0, 0, 0); got != "--" {
		t.Errorf("fmtETA(zero) = %q, want '--'", got)
	}
	if got := fmtETA(100, 100, 50); got != "--" {
		t.Errorf("fmtETA(complete) = %q, want '--'", got)
	}
}

func TestModelsPage_PathChooserCapturesInput(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/a", "/b"})
	page.pathChooser = &pathChooser{paths: page.paths}
	if !page.IsCapturingInput() {
		t.Error("path chooser must capture input")
	}
}

func TestModelsPage_PathChooserNavigationAndCancel(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/a", "/b", "/c"})
	page.pathChooser = &pathChooser{paths: page.paths}
	page.pendingDL = &pendingDownload{}

	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyDown})
	mp := updated.(ModelsPage)
	if mp.pathChooser == nil || mp.pathChooser.cursor != 1 {
		t.Fatalf("down should move cursor to 1; got %+v", mp.pathChooser)
	}

	updated, _ = mp.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mp = updated.(ModelsPage)
	if mp.pathChooser != nil || mp.pendingDL != nil {
		t.Error("esc must clear both the chooser and the parked selection")
	}
}

func TestModelsPage_PathChooserEnterClearsAndStarts(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/a", "/b"})
	page.pathChooser = &pathChooser{paths: page.paths, cursor: 1}
	page.pendingDL = &pendingDownload{} // empty selection: no Start, no manager needed

	updated, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mp := updated.(ModelsPage)
	if mp.pathChooser != nil || mp.pendingDL != nil {
		t.Error("enter must consume the chooser and parked selection")
	}
	if !strings.Contains(mp.flash.Message(), "starting 0 download") {
		t.Errorf("flash = %q, want 'starting 0 download(s)'", mp.flash.Message())
	}
}

func TestModelsPage_PathChooserRendered(t *testing.T) {
	page := NewModelsPage(&fakeScanner{}, []string{"/a/models", "/b/models"})
	page.width, page.height = 100, 30
	page.pathChooser = &pathChooser{paths: page.paths}
	out := page.View()
	if !strings.Contains(out, "which path") {
		t.Errorf("path chooser title missing; got:\n%s", out)
	}
	if !strings.Contains(out, "/a/models") || !strings.Contains(out, "/b/models") {
		t.Errorf("path options missing; got:\n%s", out)
	}
}

func TestRenderBar(t *testing.T) {
	bar := renderBar(0.5, 10)
	if !strings.HasPrefix(bar, "[") || !strings.HasSuffix(bar, "]") {
		t.Errorf("renderBar not bracketed: %q", bar)
	}
	full := strings.Count(bar, "█")
	empty := strings.Count(bar, "░")
	if full+empty != 10 {
		t.Errorf("bar glyph count = %d, want 10 (full=%d empty=%d)", full+empty, full, empty)
	}
	if full != 5 {
		t.Errorf("renderBar(0.5,10) filled = %d, want 5", full)
	}
}

// UIUX-021: terminal download events (completed/failed) raise a tab
// attention badge for the Models tab; non-terminal events stay silent.
func TestModelsPage_DownloadTerminalEventsRaiseTabAttention(t *testing.T) {
	hasAttention := func(msgs []tea.Msg) bool {
		for _, m := range msgs {
			if a, ok := m.(TabAttentionMsg); ok {
				if a.Page != AttentionModels {
					t.Fatalf("attention page = %q, want %q", a.Page, AttentionModels)
				}
				return true
			}
		}
		return false
	}

	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	_, cmd := page.handleDownloadEvent(downloadmgr.Event{State: downloadmgr.State{
		Status: downloadmgr.StatusCompleted, Spec: downloadmgr.Spec{Filename: "a.gguf"},
	}})
	if !hasAttention(drainCmd(cmd)) {
		t.Error("completed download did not raise TabAttentionMsg")
	}

	page = NewModelsPage(&fakeScanner{}, []string{"/m"})
	_, cmd = page.handleDownloadEvent(downloadmgr.Event{State: downloadmgr.State{
		Status: downloadmgr.StatusFailed, Spec: downloadmgr.Spec{Filename: "b.gguf"},
	}})
	if !hasAttention(drainCmd(cmd)) {
		t.Error("failed download did not raise TabAttentionMsg")
	}

	page = NewModelsPage(&fakeScanner{}, []string{"/m"})
	_, cmd = page.handleDownloadEvent(downloadmgr.Event{State: downloadmgr.State{
		Status: downloadmgr.StatusActive, Spec: downloadmgr.Spec{Filename: "c.gguf"},
	}})
	if hasAttention(drainCmd(cmd)) {
		t.Error("active (non-terminal) download raised TabAttentionMsg")
	}
}
