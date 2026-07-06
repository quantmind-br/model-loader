package components

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

type errorScanner struct{ err error }

func (e *errorScanner) Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error) {
	return nil, e.err
}

type fakePickerScanner struct {
	events []domain.ScanEvent
}

func (f *fakePickerScanner) Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error) {
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

// drainScan walks the picker through Init → ScanStarted → all events
// until the channel closes, returning the final picker state.
func drainScan(t *testing.T, p ModelPicker) ModelPicker {
	t.Helper()
	cmd := p.Init()
	if cmd == nil {
		return p
	}
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		var c tea.Cmd
		p, c = p.Update(msg)
		cmd = c
	}
	return p
}

func TestModelPicker_SurfacesScanError(t *testing.T) {
	scanner := &errorScanner{err: errors.New("permission denied: /models")}
	p := NewModelPicker(scanner, []string{"/models"})
	p.width = 120
	p = drainScan(t, p)

	out := p.View()
	if !strings.Contains(out, "scan error:") {
		t.Errorf("view missing scan error line; got:\n%s", out)
	}
	if !strings.Contains(out, "permission denied") {
		t.Errorf("view missing underlying error message; got:\n%s", out)
	}
}

func TestModelPicker_NoErrorWhenScanSucceeds(t *testing.T) {
	scanner := &fakePickerScanner{}
	p := NewModelPicker(scanner, []string{"/models"})
	p = drainScan(t, p)
	if strings.Contains(p.View(), "scan error:") {
		t.Errorf("successful scan should not show error line")
	}
}

func TestModelPicker_BoxWidthClampedToCeiling(t *testing.T) {
	if got := pickerBoxWidth(200); got != 120 {
		t.Errorf("pickerBoxWidth(200) = %d, want 120", got)
	}
}

// TUI-RESP: picker box width floor lowered for narrow terminals.
func TestModelPicker_BoxWidthHonorsFloor(t *testing.T) {
	if got := pickerBoxWidth(40); got != 36 {
		t.Errorf("pickerBoxWidth(40) = %d, want 36", got)
	}
	if got := pickerBoxWidth(20); got != 24 {
		t.Errorf("pickerBoxWidth(20) = %d, want 24 (floor)", got)
	}
}

// pickerWithFiles produces a picker pre-loaded with n synthetic files so
// the navigation tests can drive cursor moves without scanning.
func pickerWithFiles(n int) ModelPicker {
	p := ModelPicker{}
	for i := 0; i < n; i++ {
		p.files = append(p.files, domain.ModelFile{Name: "m", Path: "/p", Quant: "q4", Params: "7B"})
	}
	p.applyFilter()
	return p
}

func TestModelPicker_PgDnAdvancesByTen(t *testing.T) {
	p := pickerWithFiles(50)
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if p.cursor != 10 {
		t.Errorf("PgDn cursor = %d, want 10", p.cursor)
	}
}

func TestModelPicker_PgUpRetreatsByTen(t *testing.T) {
	p := pickerWithFiles(50)
	p.cursor = 25
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if p.cursor != 15 {
		t.Errorf("PgUp cursor = %d, want 15", p.cursor)
	}
}

func TestModelPicker_EndJumpsToLast(t *testing.T) {
	p := pickerWithFiles(50)
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if p.cursor != 49 {
		t.Errorf("End cursor = %d, want 49", p.cursor)
	}
}

func TestModelPicker_HomeJumpsToFirst(t *testing.T) {
	p := pickerWithFiles(50)
	p.cursor = 30
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyHome})
	if p.cursor != 0 {
		t.Errorf("Home cursor = %d, want 0", p.cursor)
	}
}

func TestModelPicker_GAndCapitalGOnlyOutsideFilterMode(t *testing.T) {
	p := pickerWithFiles(50)

	// Lowercase g goes to top.
	p.cursor = 30
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if p.cursor != 0 {
		t.Errorf("g (top) cursor = %d, want 0", p.cursor)
	}

	// Capital G goes to bottom.
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if p.cursor != 49 {
		t.Errorf("G (bottom) cursor = %d, want 49", p.cursor)
	}

	// In filter mode, g should be eaten as a filter character — verified by
	// checking p.filter, not cursor (the filter likely empties the list and
	// applyFilter snaps the cursor to 0 as a side-effect).
	p.filterMode = true
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if p.filter != "g" {
		t.Errorf("g in filterMode should append to filter; got %q", p.filter)
	}
}

func TestModelPicker_SelectEmitsModelPickedMsg(t *testing.T) {
	mf := domain.ModelFile{Path: "/m/a.gguf", Name: "a.gguf"}
	scanner := &fakePickerScanner{events: []domain.ScanEvent{
		{Type: domain.ScanEventFile, File: &mf},
		{Type: domain.ScanEventDone},
	}}

	p := NewModelPicker(scanner, []string{"/m"})
	p = drainScan(t, p)

	// Trigger Enter and capture the resulting Cmd's message.
	_, c := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if c == nil {
		t.Fatal("expected ModelPickedMsg cmd, got nil")
	}
	got := c()
	picked, ok := got.(ModelPickedMsg)
	if !ok {
		t.Fatalf("msg type = %T, want ModelPickedMsg", got)
	}
	if picked.Path != "/m/a.gguf" {
		t.Fatalf("path = %q", picked.Path)
	}
}

func TestModelPicker_EscEmitsCancel(t *testing.T) {
	scanner := &fakePickerScanner{events: []domain.ScanEvent{{Type: domain.ScanEventDone}}}
	p := NewModelPicker(scanner, nil)
	p = drainScan(t, p)

	_, c := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if c == nil {
		t.Fatal("expected ModelPickerCancelledMsg cmd")
	}
	got := c()
	if _, ok := got.(ModelPickerCancelledMsg); !ok {
		t.Fatalf("msg type = %T, want ModelPickerCancelledMsg", got)
	}
}

func TestModelPicker_FilterReducesList(t *testing.T) {
	files := []domain.ModelFile{
		{Path: "/m/qwen.gguf", Name: "qwen.gguf"},
		{Path: "/m/llama.gguf", Name: "llama.gguf"},
	}
	scanner := &fakePickerScanner{}
	p := NewModelPicker(scanner, nil)
	p.files = files
	p.applyFilter()
	if len(p.filtered) != 2 {
		t.Fatalf("filtered = %d", len(p.filtered))
	}
	p.filter = "qwen"
	p.applyFilter()
	if len(p.filtered) != 1 {
		t.Fatalf("filtered after 'qwen' = %d", len(p.filtered))
	}
	if p.filtered[0].Name != "qwen.gguf" {
		t.Fatalf("filtered[0] = %q", p.filtered[0].Name)
	}
}

func TestModelPicker_SurfacesScanEventError(t *testing.T) {
	scanner := &fakePickerScanner{events: []domain.ScanEvent{
		{Type: domain.ScanEventError, Root: "/models", Error: errors.New("permission denied")},
		{Type: domain.ScanEventDone},
	}}
	p := NewModelPicker(scanner, []string{"/models"})
	p = drainScan(t, p)

	if p.err != "permission denied" {
		t.Errorf("picker.err = %q, want 'permission denied'", p.err)
	}
	out := p.View()
	if !strings.Contains(out, "scan error:") {
		t.Errorf("view missing scan error line; got:\n%s", out)
	}
}

func TestModelPicker_SelectedPrefixUnderNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	theme.RebuildStyles()
	defer func() {
		t.Setenv("NO_COLOR", "")
		theme.RebuildStyles()
	}()

	p := pickerWithFiles(3)
	p.cursor = 1
	out := p.View()
	// Non-color prefix "> " marks the selected row when NO_COLOR strips
	// foreground/background colors.
	if !strings.Contains(out, "> m") {
		t.Errorf("NO_COLOR picker view missing cursor prefix on selected row; got:\n%s", out)
	}
}
