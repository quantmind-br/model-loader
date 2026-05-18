package components

import (
	"context"
	"strings"
	"testing"
	"time"

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

func TestProxyPanel_Hints(t *testing.T) {
	p := NewProxyPanel(&fakeHTTPProxy{})
	if h := p.Hints(); h != "[s] start  [x] stop" {
		t.Errorf("Hints = %q, want default proxy hints", h)
	}
}

func TestProxyPanel_SKeyStartsProxy(t *testing.T) {
	f := &fakeHTTPProxy{}
	p := NewProxyPanel(f)
	cmd, consumed := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if !consumed {
		t.Fatal("s must be consumed")
	}
	if cmd == nil {
		t.Fatal("s must return a command")
	}
	msg := cmd()
	p.Update(msg)
	if !f.started {
		t.Error("s did not start proxy")
	}
}

func TestProxyPanel_XKeyStopsProxy(t *testing.T) {
	f := &fakeHTTPProxy{status: httpproxy.Status{Running: true}}
	p := NewProxyPanel(f)
	cmd, consumed := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if !consumed {
		t.Fatal("x must be consumed")
	}
	if cmd == nil {
		t.Fatal("x must return a command")
	}
	msg := cmd()
	p.Update(msg)
	if !f.stopped {
		t.Error("x did not stop proxy")
	}
}

func TestProxyPanel_StartFailureShowsFlash(t *testing.T) {
	f := &fakeHTTPProxy{startErr: context.DeadlineExceeded}
	p := NewProxyPanel(f)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	p.Update(ProxyActionResultMsg{action: "start", err: context.DeadlineExceeded})
	if p.flash.Message() == "" {
		t.Error("expected flash message after start failure")
	}
}

func TestProxyPanel_StopFailureShowsFlash(t *testing.T) {
	f := &fakeHTTPProxy{stopErr: context.Canceled}
	p := NewProxyPanel(f)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	p.Update(ProxyActionResultMsg{action: "stop", err: context.Canceled})
	if p.flash.Message() == "" {
		t.Error("expected flash message after stop failure")
	}
}

func TestProxyPanel_NilProxy(t *testing.T) {
	p := NewProxyPanel(nil)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	_ = p.flash.Message()
}

func TestProxyPanel_PendingStartIgnoresDuplicate(t *testing.T) {
	f := &fakeHTTPProxy{}
	p := NewProxyPanel(f)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if p.pending != pendingStart {
		t.Fatalf("pending = %q, want start", p.pending)
	}
	cmd, consumed := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd != nil {
		t.Error("duplicate s key must not return a command while pending")
	}
	if !consumed {
		t.Error("duplicate s key should still be consumed")
	}
	if p.pending != pendingStart {
		t.Errorf("pending changed on duplicate key; got %q", p.pending)
	}
}

func TestProxyPanel_PendingStopIgnoresDuplicate(t *testing.T) {
	f := &fakeHTTPProxy{status: httpproxy.Status{Running: true}}
	p := NewProxyPanel(f)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if p.pending != pendingStop {
		t.Fatalf("pending = %q, want stop", p.pending)
	}
	cmd, consumed := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd != nil {
		t.Error("duplicate x key must not return a command while pending")
	}
	if !consumed {
		t.Error("duplicate x key should still be consumed")
	}
	if p.pending != pendingStop {
		t.Errorf("pending changed on duplicate key; got %q", p.pending)
	}
}

func TestProxyPanel_PendingClearedAfterResult(t *testing.T) {
	f := &fakeHTTPProxy{}
	p := NewProxyPanel(f)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if p.pending != pendingStart {
		t.Fatalf("pending = %q, want start", p.pending)
	}
	p.Update(ProxyActionResultMsg{action: "start"})
	if p.pending != pendingNone {
		t.Errorf("pending = %q, want none after result", p.pending)
	}
}

func TestProxyPanel_ViewEmptyWhenNilProxy(t *testing.T) {
	p := NewProxyPanel(nil)
	v := p.View()
	if v != "" {
		t.Errorf("View = %q, want empty string when srv is nil", v)
	}
}

func TestProxyPanel_ViewShowsRunningStatus(t *testing.T) {
	f := &fakeHTTPProxy{status: httpproxy.Status{Running: true, Addr: "http://localhost:8080", LoadedProfileID: "qwen"}}
	p := NewProxyPanel(f)
	v := p.View()
	if !strings.Contains(v, "● RUNNING") {
		t.Errorf("View missing RUNNING indicator:\n%s", v)
	}
	if !strings.Contains(v, "http://localhost:8080") {
		t.Errorf("View missing addr:\n%s", v)
	}
	if !strings.Contains(v, "profile=qwen") {
		t.Errorf("View missing profile:\n%s", v)
	}
}

func TestProxyPanel_ViewShowsStoppedStatus(t *testing.T) {
	f := &fakeHTTPProxy{status: httpproxy.Status{Running: false}}
	p := NewProxyPanel(f)
	v := p.View()
	if !strings.Contains(v, "○ STOPPED") {
		t.Errorf("View missing STOPPED indicator:\n%s", v)
	}
}

func TestProxyPanel_ViewShowsLastSwapAndInflight(t *testing.T) {
	f := &fakeHTTPProxy{status: httpproxy.Status{
		Running:           true,
		Addr:              "http://localhost:8080",
		LoadedProfileID:   "qwen",
		LastSwapAt:        time.Now().Add(-2 * time.Second),
		LastSwapDur:       140 * time.Millisecond,
		InflightRequests:  3,
		LastError:         "some error",
		LastErrorAt:       time.Now().Add(-5 * time.Second),
	}}
	p := NewProxyPanel(f)
	v := p.View()
	if !strings.Contains(v, "Last swap") {
		t.Errorf("View missing Last swap:\n%s", v)
	}
	if !strings.Contains(v, "Inflight: 3") {
		t.Errorf("View missing Inflight:\n%s", v)
	}
	if !strings.Contains(v, "some error") {
		t.Errorf("View missing Last error:\n%s", v)
	}
}

func TestProxyPanel_ViewShowsPendingStart(t *testing.T) {
	f := &fakeHTTPProxy{}
	p := NewProxyPanel(f)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	v := p.View()
	if !strings.Contains(v, "Starting") {
		t.Errorf("View missing pending start:\n%s", v)
	}
}

func TestProxyPanel_ViewShowsPendingStop(t *testing.T) {
	f := &fakeHTTPProxy{status: httpproxy.Status{Running: true}}
	p := NewProxyPanel(f)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	v := p.View()
	if !strings.Contains(v, "Stopping") {
		t.Errorf("View missing pending stop:\n%s", v)
	}
}

func TestProxyPanel_TickUpdatesStatus(t *testing.T) {
	f := &fakeHTTPProxy{status: httpproxy.Status{Running: true}}
	p := NewProxyPanel(f)
	p.Update(ProxyTickMsg{})
	if !p.status.Running {
		t.Error("tick did not update status")
	}
}

func TestProxyPanel_WindowSize(t *testing.T) {
	p := NewProxyPanel(&fakeHTTPProxy{})
	p.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if p.width != 100 {
		t.Errorf("width = %d, want 100", p.width)
	}
}

func TestProxyPanel_SetWidth(t *testing.T) {
	p := NewProxyPanel(&fakeHTTPProxy{})
	p.SetWidth(80)
	if p.width != 80 {
		t.Errorf("width = %d, want 80", p.width)
	}
}

func TestProxyPanel_StatusExposed(t *testing.T) {
	f := &fakeHTTPProxy{status: httpproxy.Status{Running: true, Addr: "a"}}
	p := NewProxyPanel(f)
	if p.Status().Addr != "a" {
		t.Errorf("Status().Addr = %q, want a", p.Status().Addr)
	}
}

func TestProxyPanel_FlashClearMsg(t *testing.T) {
	f := &fakeHTTPProxy{startErr: context.DeadlineExceeded}
	p := NewProxyPanel(f)
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	p.Update(ProxyActionResultMsg{action: "start", err: context.DeadlineExceeded})
	if p.flash.Message() == "" {
		t.Fatal("flash should be set")
	}
	at := p.flash.At()
	p.Update(FlashClearMsg{Tag: "proxy", At: at})
	if p.flash.Message() != "" {
		t.Errorf("flash should be cleared after FlashClearMsg; got %q", p.flash.Message())
	}
}

func TestProxyPanel_NilProxyViewIsEmpty(t *testing.T) {
	p := NewProxyPanel(nil)
	if v := p.View(); v != "" {
		t.Errorf("nil proxy View() = %q, want empty", v)
	}
}
