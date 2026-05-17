package pages

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
)

type fakeHTTPProxy struct {
	started  bool
	stopped  bool
	status   httpproxy.Status
	startErr error
	stopErr  error
}

func (f *fakeHTTPProxy) Start(ctx context.Context) error {
	f.started = true
	f.status.Running = true
	return f.startErr
}

func (f *fakeHTTPProxy) Stop(ctx context.Context) error {
	f.stopped = true
	f.status.Running = false
	return f.stopErr
}

func (f *fakeHTTPProxy) Status() httpproxy.Status {
	return f.status
}

func TestServerPage_Hints(t *testing.T) {
	p := NewServerPage(&fakeHTTPProxy{})
	if h := p.Hints(); h != "[s] start  [x] stop  [r] refresh" {
		t.Errorf("Hints = %q, want default server hints", h)
	}
}

func TestServerPage_SKeyStartsProxy(t *testing.T) {
	f := &fakeHTTPProxy{}
	p := NewServerPage(f)
	updated, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatal("s must return a command")
	}
	msg := cmd()
	updated, _ = updated.(*ServerPage).Update(msg)
	if !f.started {
		t.Error("s did not start proxy")
	}
	_ = updated
}

func TestServerPage_XKeyStopsProxy(t *testing.T) {
	f := &fakeHTTPProxy{status: httpproxy.Status{Running: true}}
	p := NewServerPage(f)
	updated, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd == nil {
		t.Fatal("x must return a command")
	}
	msg := cmd()
	updated, _ = updated.(*ServerPage).Update(msg)
	if !f.stopped {
		t.Error("x did not stop proxy")
	}
	_ = updated
}

func TestServerPage_RKeyRefreshesStatus(t *testing.T) {
	f := &fakeHTTPProxy{status: httpproxy.Status{Running: true, Addr: "old"}}
	p := NewServerPage(f)
	f.status.Addr = "new"
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if p.status.Addr != "new" {
		t.Errorf("r did not refresh status; got %q, want %q", p.status.Addr, "new")
	}
}

func TestServerPage_StartFailureShowsFlash(t *testing.T) {
	f := &fakeHTTPProxy{startErr: context.DeadlineExceeded}
	p := NewServerPage(f)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	p.Update(serverActionResultMsg{action: "start", err: context.DeadlineExceeded})
	if p.flash.Message() == "" {
		t.Error("expected flash message after start failure")
	}
}

func TestServerPage_StopFailureShowsFlash(t *testing.T) {
	f := &fakeHTTPProxy{stopErr: context.Canceled}
	p := NewServerPage(f)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	p.Update(serverActionResultMsg{action: "stop", err: context.Canceled})
	if p.flash.Message() == "" {
		t.Error("expected flash message after stop failure")
	}
}

func TestServerPage_NilProxy(t *testing.T) {
	p := NewServerPage(nil)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	_ = p.flash.Message()
}

func TestServerPage_PendingStartIgnoresDuplicate(t *testing.T) {
	f := &fakeHTTPProxy{}
	p := NewServerPage(f)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if p.pending != pendingStart {
		t.Fatalf("pending = %q, want start", p.pending)
	}
	updated, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd != nil {
		t.Error("duplicate s key must not return a command while pending")
	}
	up := updated.(*ServerPage)
	if up.pending != pendingStart {
		t.Errorf("pending changed on duplicate key; got %q", up.pending)
	}
}

func TestServerPage_PendingStopIgnoresDuplicate(t *testing.T) {
	f := &fakeHTTPProxy{status: httpproxy.Status{Running: true}}
	p := NewServerPage(f)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if p.pending != pendingStop {
		t.Fatalf("pending = %q, want stop", p.pending)
	}
	updated, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd != nil {
		t.Error("duplicate x key must not return a command while pending")
	}
	up := updated.(*ServerPage)
	if up.pending != pendingStop {
		t.Errorf("pending changed on duplicate key; got %q", up.pending)
	}
}

func TestServerPage_PendingClearedAfterResult(t *testing.T) {
	f := &fakeHTTPProxy{}
	p := NewServerPage(f)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if p.pending != pendingStart {
		t.Fatalf("pending = %q, want start", p.pending)
	}
	p.Update(serverActionResultMsg{action: "start"})
	if p.pending != pendingNone {
		t.Errorf("pending = %q, want none after result", p.pending)
	}
}

func TestServerPage_ViewShowsNotConfigured(t *testing.T) {
	p := NewServerPage(nil)
	v := p.View()
	if !strings.Contains(v, "not configured") {
		t.Errorf("View = %q, want 'not configured' text", v)
	}
}

func TestServerPage_ViewShowsStartHintWhenStopped(t *testing.T) {
	f := &fakeHTTPProxy{status: httpproxy.Status{Running: false}}
	p := NewServerPage(f)
	v := p.View()
	if !strings.Contains(v, "Press [s] to start") {
		t.Errorf("View = %q, want 'Press [s] to start' hint", v)
	}
}

func TestServerPage_TickUpdatesStatus(t *testing.T) {
	f := &fakeHTTPProxy{status: httpproxy.Status{Running: true}}
	p := NewServerPage(f)
	p.Update(serverTickMsg{})
	if !p.status.Running {
		t.Error("tick did not update status")
	}
}

func TestServerPage_WindowSize(t *testing.T) {
	p := NewServerPage(&fakeHTTPProxy{})
	p.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if p.width != 100 || p.height != 30 {
		t.Errorf("size = %dx%d, want 100x30", p.width, p.height)
	}
}
