package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeSnapshotter struct {
	states []DownloadState
}

func (f *fakeSnapshotter) Snapshot() []DownloadState {
	return f.states
}

func TestDownloadProgress_InitReturnsNil(t *testing.T) {
	p := NewDownloadProgress(&fakeSnapshotter{}, 80)
	if cmd := p.Init(); cmd != nil {
		t.Errorf("Init() = %v, want nil", cmd)
	}
}

func TestDownloadProgress_HiddenByDefault(t *testing.T) {
	p := NewDownloadProgress(&fakeSnapshotter{}, 80)
	if p.IsVisible() {
		t.Error("DownloadProgress should be hidden by default")
	}
}

func TestDownloadProgress_DTogglesVisibility(t *testing.T) {
	p := NewDownloadProgress(&fakeSnapshotter{}, 80)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if !p.IsVisible() {
		t.Error("visibility should be true after first 'd'")
	}
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if p.IsVisible() {
		t.Error("visibility should be false after second 'd'")
	}
}

func TestDownloadProgress_ViewEmptyWhenHidden(t *testing.T) {
	snap := &fakeSnapshotter{states: []DownloadState{
		{ID: "1", Name: "model.gguf", Status: "active", Bytes: 512, Total: 1024},
	}}
	p := NewDownloadProgress(snap, 80)
	if p.View() != "" {
		t.Errorf("view should be empty when hidden; got %q", p.View())
	}
}

func TestDownloadProgress_ViewEmptyWhenNoSnapshot(t *testing.T) {
	p := NewDownloadProgress(&fakeSnapshotter{}, 80)
	p.visible = true
	if p.View() != "" {
		t.Errorf("view should be empty with no downloads; got %q", p.View())
	}
}

func TestDownloadProgress_ViewRendersActiveDownload(t *testing.T) {
	snap := &fakeSnapshotter{states: []DownloadState{
		{ID: "1", Name: "model.gguf", Status: "active", Bytes: 512, Total: 1024},
	}}
	p := NewDownloadProgress(snap, 80)
	p.visible = true
	out := p.View()
	if !strings.Contains(out, "model.gguf") {
		t.Errorf("view missing download name; got %q", out)
	}
	if !strings.Contains(out, "active") {
		t.Errorf("view missing status; got %q", out)
	}
	if !strings.Contains(out, "50%") {
		t.Errorf("view missing 50%% progress; got %q", out)
	}
}

func TestDownloadProgress_ViewMarksFocusedRow(t *testing.T) {
	snap := &fakeSnapshotter{states: []DownloadState{
		{ID: "1", Name: "a.gguf", Status: "active", Bytes: 1, Total: 10},
		{ID: "2", Name: "b.gguf", Status: "active", Bytes: 2, Total: 10},
	}}
	p := NewDownloadProgress(snap, 80)
	p.visible = true
	out := p.View()
	if !strings.HasPrefix(out, "> ") {
		t.Errorf("focused row should be prefixed with '> '; got first line: %q", strings.SplitN(out, "\n", 2)[0])
	}
}

func TestDownloadProgress_ViewShowsErrorForFailed(t *testing.T) {
	snap := &fakeSnapshotter{states: []DownloadState{
		{ID: "1", Name: "model.gguf", Status: "failed", Bytes: 0, Total: 1024, Err: "network down"},
	}}
	p := NewDownloadProgress(snap, 80)
	p.visible = true
	out := p.View()
	if !strings.Contains(out, "network down") {
		t.Errorf("failed download should show error; got %q", out)
	}
}

func TestDownloadProgress_UpDownMovesFocusWhenVisible(t *testing.T) {
	snap := &fakeSnapshotter{states: []DownloadState{
		{ID: "1"}, {ID: "2"}, {ID: "3"},
	}}
	p := NewDownloadProgress(snap, 80)
	p.visible = true

	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if p.focusIndex != 1 {
		t.Errorf("focusIndex after Down = %d, want 1", p.focusIndex)
	}
	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if p.focusIndex != 2 {
		t.Errorf("focusIndex should clamp at end; got %d, want 2", p.focusIndex)
	}
	p.Update(tea.KeyMsg{Type: tea.KeyUp})
	if p.focusIndex != 1 {
		t.Errorf("focusIndex after Up = %d, want 1", p.focusIndex)
	}
}

func TestDownloadProgress_UpDownIgnoredWhenHidden(t *testing.T) {
	snap := &fakeSnapshotter{states: []DownloadState{{ID: "1"}, {ID: "2"}}}
	p := NewDownloadProgress(snap, 80)

	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if p.focusIndex != 0 {
		t.Errorf("focusIndex should not move while hidden; got %d", p.focusIndex)
	}
}

func TestDownloadProgress_XEmitsCancelMsgForActive(t *testing.T) {
	snap := &fakeSnapshotter{states: []DownloadState{
		{ID: "abc", Status: "active"},
	}}
	p := NewDownloadProgress(snap, 80)
	p.visible = true

	cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd == nil {
		t.Fatal("pressing 'x' on active download must return a cancel Cmd")
	}
	msg := cmd()
	cancel, ok := msg.(DownloadCancelMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want DownloadCancelMsg", msg)
	}
	if cancel.ID != "abc" {
		t.Errorf("DownloadCancelMsg.ID = %q, want %q", cancel.ID, "abc")
	}
}

func TestDownloadProgress_XIgnoredForInactiveDownload(t *testing.T) {
	snap := &fakeSnapshotter{states: []DownloadState{
		{ID: "abc", Status: "completed"},
	}}
	p := NewDownloadProgress(snap, 80)
	p.visible = true

	cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd != nil {
		t.Errorf("pressing 'x' on completed download should not emit cancel Cmd")
	}
}

func TestDownloadProgress_TogglingOffResetsFocus(t *testing.T) {
	snap := &fakeSnapshotter{states: []DownloadState{{ID: "1"}, {ID: "2"}}}
	p := NewDownloadProgress(snap, 80)
	p.visible = true
	p.focusIndex = 1

	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if p.focusIndex != 0 {
		t.Errorf("toggling off should reset focus to 0; got %d", p.focusIndex)
	}
}

func TestDownloadProgress_SetWidthUpdates(t *testing.T) {
	p := NewDownloadProgress(&fakeSnapshotter{}, 80)
	p.SetWidth(120)
	if p.width != 120 {
		t.Errorf("width = %d, want 120", p.width)
	}
}

func TestDownloadProgress_IsFocusVisibleRequiresVisible(t *testing.T) {
	p := NewDownloadProgress(&fakeSnapshotter{}, 80)
	if p.IsFocusVisible() {
		t.Error("hidden progress should not report focus-visible")
	}
	p.visible = true
	if !p.IsFocusVisible() {
		t.Error("visible progress with focusIndex>=0 should report focus-visible")
	}
}

func TestHumanBytes_Formatting(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{1023, "1023B"},
		{1024, "1.00KB"},
		{1024 * 1024, "1.00MB"},
		{1024 * 1024 * 1024, "1.00GB"},
		{1024 * 1024 * 1024 * 1024, "1.00TB"},
	}
	for _, c := range cases {
		got := humanBytes(c.in)
		if got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
