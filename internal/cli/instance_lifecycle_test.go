package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
)

// fakeProxy implements proxyClient for CLI tests.
type fakeProxy struct {
	ensured int
	calls   []string // ordered: "load:<id>", "unload"
	unloads []bool   // force flag per Unload call
	status  httpproxy.Status
	loadErr error
}

func (f *fakeProxy) EnsureRunning(ctx context.Context) error { f.ensured++; return nil }

func (f *fakeProxy) Load(ctx context.Context, profileID string) (httpproxy.Status, error) {
	f.calls = append(f.calls, "load:"+profileID)
	if f.loadErr != nil {
		return httpproxy.Status{}, f.loadErr
	}
	f.status = httpproxy.Status{Running: true, LoadedProfileID: profileID, LoadedPID: 4242, LoadedPort: 8080}
	return f.status, nil
}

func (f *fakeProxy) Unload(ctx context.Context, force bool) (httpproxy.Status, error) {
	f.calls = append(f.calls, "unload")
	f.unloads = append(f.unloads, force)
	f.status.LoadedProfileID = ""
	f.status.LoadedPID = 0
	return f.status, nil
}

func (f *fakeProxy) Status() httpproxy.Status { return f.status }
func (f *fakeProxy) BaseURL() string          { return "http://127.0.0.1:9099" }

func TestStartInstance_LoadsResolvedProfileViaProxy(t *testing.T) {
	store := newTempStore(t)
	seed(t, store, "alpha", "Alpha")
	p := &fakeProxy{}
	var out strings.Builder
	if err := startInstance(context.Background(), &out, p, store, "alpha"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if p.ensured != 1 {
		t.Fatalf("expected EnsureRunning once, got %d", p.ensured)
	}
	if len(p.calls) != 1 || p.calls[0] != "load:alpha" {
		t.Fatalf("did not load alpha via proxy: %+v", p.calls)
	}
	if !strings.Contains(out.String(), "4242") || !strings.Contains(out.String(), p.BaseURL()) {
		t.Fatalf("expected pid and base URL in output: %q", out.String())
	}
}

func TestStartInstance_LoadFailure(t *testing.T) {
	store := newTempStore(t)
	seed(t, store, "alpha", "Alpha")
	p := &fakeProxy{loadErr: errors.New("boom")}
	var out strings.Builder
	if err := startInstance(context.Background(), &out, p, store, "alpha"); err == nil {
		t.Fatalf("expected error from failed load")
	}
}

func TestStopInstance_UnloadsLoadedPID(t *testing.T) {
	p := &fakeProxy{status: httpproxy.Status{Running: true, LoadedProfileID: "alpha", LoadedPID: 100}}
	m := &fakeManager{}
	var out strings.Builder
	if err := stopInstance(context.Background(), &out, p, m, "100"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(p.calls) != 1 || p.calls[0] != "unload" {
		t.Fatalf("expected unload via proxy: %+v", p.calls)
	}
	if len(p.unloads) != 1 || p.unloads[0] {
		t.Fatalf("expected Unload(force=false): %+v", p.unloads)
	}
	if len(m.killed) != 0 {
		t.Fatalf("must not Kill the proxy-loaded backend: %+v", m.killed)
	}
}

func TestStopInstance_UnloadsByLoadedProfileID(t *testing.T) {
	p := &fakeProxy{status: httpproxy.Status{Running: true, LoadedProfileID: "alpha", LoadedPID: 100}}
	m := &fakeManager{}
	var out strings.Builder
	if err := stopInstance(context.Background(), &out, p, m, "alpha"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(p.calls) != 1 || p.calls[0] != "unload" {
		t.Fatalf("expected unload via proxy: %+v", p.calls)
	}
}

func TestStopInstance_KillsOrphanPID(t *testing.T) {
	p := &fakeProxy{} // proxy has nothing loaded
	m := &fakeManager{running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}}}
	var out strings.Builder
	if err := stopInstance(context.Background(), &out, p, m, "100"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(m.killed) != 1 || m.killed[0] != 100 {
		t.Fatalf("did not kill orphan 100: %+v", m.killed)
	}
	if len(p.calls) != 0 {
		t.Fatalf("must not touch the proxy for orphans: %+v", p.calls)
	}
}

func TestRestartInstance_UnloadsThenLoads(t *testing.T) {
	store := newTempStore(t)
	seed(t, store, "alpha", "Alpha")
	p := &fakeProxy{status: httpproxy.Status{Running: true, LoadedProfileID: "alpha", LoadedPID: 100}}
	m := &fakeManager{}
	var out strings.Builder
	if err := restartInstance(context.Background(), &out, p, m, store, "100"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	want := []string{"unload", "load:alpha"}
	if len(p.calls) != 2 || p.calls[0] != want[0] || p.calls[1] != want[1] {
		t.Fatalf("expected unload then load, got: %+v", p.calls)
	}
	if len(m.killed) != 0 {
		t.Fatalf("must not Kill via manager on restart: %+v", m.killed)
	}
}

func TestRestartInstance_ResolvesOrphanViaManager(t *testing.T) {
	store := newTempStore(t)
	seed(t, store, "alpha", "Alpha")
	p := &fakeProxy{} // proxy has nothing loaded
	m := &fakeManager{running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}}}
	var out strings.Builder
	if err := restartInstance(context.Background(), &out, p, m, store, "100"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	want := []string{"unload", "load:alpha"}
	if len(p.calls) != 2 || p.calls[0] != want[0] || p.calls[1] != want[1] {
		t.Fatalf("expected unload then load, got: %+v", p.calls)
	}
}
