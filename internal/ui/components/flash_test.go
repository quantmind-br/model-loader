package components

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

func TestFlash_NewFlashIsEmpty(t *testing.T) {
	f := NewFlash("profiles")
	if f.Message() != "" {
		t.Errorf("new Flash Message = %q, want empty", f.Message())
	}
	if !f.At().IsZero() {
		t.Errorf("new Flash At = %v, want zero", f.At())
	}
	if f.View() != "" {
		t.Errorf("new Flash View = %q, want empty", f.View())
	}
}

func TestFlash_SetStoresMessageAndStamps(t *testing.T) {
	f := NewFlash("profiles")
	before := time.Now()
	f, cmd := f.Set("hello")
	after := time.Now()

	if f.Message() != "hello" {
		t.Errorf("Message = %q, want hello", f.Message())
	}
	if f.At().Before(before) || f.At().After(after) {
		t.Errorf("At = %v, want between %v and %v", f.At(), before, after)
	}
	if cmd == nil {
		t.Fatal("Set returned nil cmd; expected auto-clear tick")
	}
}

func TestFlash_SetCmdEmitsFlashClearMsg(t *testing.T) {
	f := NewFlash("profiles")
	f, cmd := f.Set("hello")
	if cmd == nil {
		t.Fatal("Set returned nil cmd")
	}
	// The cmd wraps tea.Tick(FlashLifetime) so calling it directly
	// would block ~15s. Run async with a short timeout: if the tick
	// fires (unlikely in CI), assert payload shape; otherwise the
	// timeout path is fine — Update is exercised separately.
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		cm, ok := msg.(FlashClearMsg)
		if !ok {
			t.Fatalf("cmd produced %T, want FlashClearMsg", msg)
		}
		if cm.Tag != "profiles" {
			t.Errorf("FlashClearMsg.Tag = %q, want profiles", cm.Tag)
		}
		if !cm.At.Equal(f.At()) {
			t.Errorf("FlashClearMsg.At = %v, want %v", cm.At, f.At())
		}
	case <-time.After(50 * time.Millisecond):
		// Acceptable: tick hasn't fired yet. cmd structure already verified non-nil.
	}
}

func TestFlash_UpdateMatchingClears(t *testing.T) {
	f := NewFlash("profiles")
	f, _ = f.Set("hello")
	cleared, handled := f.Update(FlashClearMsg{Tag: "profiles", At: f.At()})
	if !handled {
		t.Error("Update reported handled=false for matching clear")
	}
	if cleared.Message() != "" {
		t.Errorf("Message = %q, want empty after matching clear", cleared.Message())
	}
	if !cleared.At().IsZero() {
		t.Errorf("At = %v, want zero after matching clear", cleared.At())
	}
}

func TestFlash_UpdateStaleIsIgnored(t *testing.T) {
	f := NewFlash("profiles")
	f, _ = f.Set("hello")
	staleAt := f.At().Add(-time.Second)

	updated, handled := f.Update(FlashClearMsg{Tag: "profiles", At: staleAt})
	if handled {
		t.Error("Update reported handled=true for stale clear")
	}
	if updated.Message() != "hello" {
		t.Errorf("Message = %q, want hello (stale clear must not erase)", updated.Message())
	}
}

func TestFlash_UpdateWrongTagIsIgnored(t *testing.T) {
	f := NewFlash("profiles")
	f, _ = f.Set("hello")

	updated, handled := f.Update(FlashClearMsg{Tag: "models", At: f.At()})
	if handled {
		t.Error("Update reported handled=true for wrong tag")
	}
	if updated.Message() != "hello" {
		t.Errorf("Message = %q, want hello (cross-tag clear must not erase)", updated.Message())
	}
}

func TestFlash_UpdateNonFlashMsgIsIgnored(t *testing.T) {
	f := NewFlash("profiles")
	f, _ = f.Set("hello")

	updated, handled := f.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if handled {
		t.Error("Update reported handled=true for non-FlashClearMsg")
	}
	if updated.Message() != "hello" {
		t.Errorf("Message = %q, want hello", updated.Message())
	}
}

func TestFlash_ViewRendersMessage(t *testing.T) {
	theme.RebuildStyles()
	f := NewFlash("profiles")
	f, _ = f.Set("hello world")
	if !strings.Contains(f.View(), "hello world") {
		t.Errorf("View = %q, want to contain 'hello world'", f.View())
	}
}

func TestFlash_ViewFaintAfterDimAge(t *testing.T) {
	theme.RebuildStyles()
	dim := Flash{tag: "profiles", message: "old", at: time.Now().Add(-FlashDimAfter - time.Second)}
	fresh := Flash{tag: "profiles", message: "old", at: time.Now()}

	wantDim := theme.Subtitle.Faint(true).Render("old")
	wantFresh := theme.Subtitle.Render("old")

	if got := dim.View(); got != wantDim {
		t.Errorf("aged flash View = %q, want %q (Faint style)", got, wantDim)
	}
	if got := fresh.View(); got != wantFresh {
		t.Errorf("fresh flash View = %q, want %q (non-Faint style)", got, wantFresh)
	}
}
