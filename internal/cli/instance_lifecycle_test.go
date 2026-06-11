package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
)

// fakeProxy implements proxyClient for CLI tests.
type fakeProxy struct {
	ensured   int
	calls     []string // ordered: "load:<id>", "unload"
	unloads   []bool   // force flag per Unload call
	status    httpproxy.Status
	statusSeq []httpproxy.Status // optional: consumed per Status() call before falling back to status
	loadErr   error
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

func (f *fakeProxy) Status() httpproxy.Status {
	if len(f.statusSeq) > 0 {
		st := f.statusSeq[0]
		f.statusSeq = f.statusSeq[1:]
		return st
	}
	return f.status
}
func (f *fakeProxy) BaseURL() string          { return "http://127.0.0.1:9099" }

func TestStartInstance_LoadsResolvedProfileViaProxy(t *testing.T) {
	store := newTempStore(t)
	seed(t, store, "alpha", "Alpha")
	p := &fakeProxy{}
	var out strings.Builder
	if err := startInstance(context.Background(), &out, io.Discard, p, store, "alpha"); err != nil {
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
	if err := startInstance(context.Background(), &out, io.Discard, p, store, "alpha"); err == nil {
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

func TestStopInstance_UnloadsResolvedProfilePrefix(t *testing.T) {
	// "alp" is neither the pid nor the exact loaded profile id, so proxyOwnsRef
	// misses; resolveInstance prefix-resolves it to the proxy-loaded PID.
	p := &fakeProxy{status: httpproxy.Status{Running: true, LoadedProfileID: "alpha", LoadedPID: 100}}
	m := &fakeManager{running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}}}
	var out strings.Builder
	if err := stopInstance(context.Background(), &out, p, m, "alp"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(p.calls) != 1 || p.calls[0] != "unload" {
		t.Fatalf("expected unload via proxy: %+v", p.calls)
	}
	if len(m.killed) != 0 {
		t.Fatalf("must not Kill the proxy-loaded backend: %+v", m.killed)
	}
	if !strings.Contains(out.String(), "unloaded alpha (pid 100)") {
		t.Fatalf("expected unload message, got %q", out.String())
	}
}

func TestStopInstance_UnloadsResolvedProfilePrefix_JSON(t *testing.T) {
	jsonOut = true
	t.Cleanup(func() { jsonOut = false })
	p := &fakeProxy{status: httpproxy.Status{Running: true, LoadedProfileID: "alpha", LoadedPID: 100}}
	m := &fakeManager{running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}}}
	var out strings.Builder
	if err := stopInstance(context.Background(), &out, p, m, "alp"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(p.calls) != 1 || p.calls[0] != "unload" {
		t.Fatalf("expected unload via proxy: %+v", p.calls)
	}
	var st httpproxy.Status
	if err := json.Unmarshal([]byte(out.String()), &st); err != nil {
		t.Fatalf("expected JSON status output, got %q: %v", out.String(), err)
	}
	if st.LoadedPID != 0 || st.LoadedProfileID != "" {
		t.Fatalf("expected post-unload status in JSON output: %+v", st)
	}
}

func TestStopInstance_OrphanAlreadyExited(t *testing.T) {
	p := &fakeProxy{} // proxy has nothing loaded
	m := &fakeManager{
		running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}},
		killErr: processmgr.ErrUnknownPID,
	}
	var out strings.Builder
	if err := stopInstance(context.Background(), &out, p, m, "100"); err != nil {
		t.Fatalf("stop should tolerate ErrUnknownPID: %v", err)
	}
	if !strings.Contains(out.String(), "already exited") {
		t.Fatalf("expected already-exited note, got %q", out.String())
	}
}

func TestStopInstance_RefusesKillWhenProxyStatusDegraded(t *testing.T) {
	degraded := httpproxy.Status{Running: true, LastError: "status_probe_failed: connection refused"}
	p := &fakeProxy{status: degraded}
	m := &fakeManager{running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}}}
	var out strings.Builder
	err := stopInstance(context.Background(), &out, p, m, "100")
	if err == nil || !strings.Contains(err.Error(), "refusing to kill pid 100") {
		t.Fatalf("expected refusal on degraded proxy status, got: %v", err)
	}
	if len(m.killed) != 0 {
		t.Fatalf("must not kill while proxy status is unavailable: %+v", m.killed)
	}
	if len(p.calls) != 0 {
		t.Fatalf("must not touch the proxy: %+v", p.calls)
	}
}

func TestStopInstance_TransientProbeFailureRecovers(t *testing.T) {
	degraded := httpproxy.Status{Running: true, LastError: "status_probe_failed: connection refused"}
	healthy := httpproxy.Status{Running: true} // nothing loaded
	p := &fakeProxy{statusSeq: []httpproxy.Status{degraded, healthy}}
	m := &fakeManager{running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}}}
	var out strings.Builder
	if err := stopInstance(context.Background(), &out, p, m, "100"); err != nil {
		t.Fatalf("stop after transient probe failure: %v", err)
	}
	if len(m.killed) != 1 || m.killed[0] != 100 {
		t.Fatalf("expected orphan kill after status recovered: %+v", m.killed)
	}
}

func TestRestartInstance_RefusesKillWhenProxyStatusDegraded(t *testing.T) {
	store := newTempStore(t)
	seed(t, store, "alpha", "Alpha")
	degraded := httpproxy.Status{Running: true, LastError: "status_probe_failed: connection refused"}
	p := &fakeProxy{status: degraded}
	m := &fakeManager{running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}}}
	var out strings.Builder
	err := restartInstance(context.Background(), &out, io.Discard, p, m, store, "100")
	if err == nil || !strings.Contains(err.Error(), "refusing to kill pid 100") {
		t.Fatalf("expected refusal on degraded proxy status, got: %v", err)
	}
	if len(m.killed) != 0 {
		t.Fatalf("must not kill while proxy status is unavailable: %+v", m.killed)
	}
}

func TestRestartInstance_UnloadsThenLoads(t *testing.T) {
	store := newTempStore(t)
	seed(t, store, "alpha", "Alpha")
	p := &fakeProxy{status: httpproxy.Status{Running: true, LoadedProfileID: "alpha", LoadedPID: 100}}
	m := &fakeManager{}
	var out strings.Builder
	if err := restartInstance(context.Background(), &out, io.Discard, p, m, store, "100"); err != nil {
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
	if err := restartInstance(context.Background(), &out, io.Discard, p, m, store, "100"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	// Orphan must be killed before load so VRAM is freed first.
	if len(m.killed) != 1 || m.killed[0] != 100 {
		t.Fatalf("expected orphan pid 100 to be killed before restart, got killed=%v", m.killed)
	}
	// Proxy owns nothing, so no Unload needed — only Load.
	want := []string{"load:alpha"}
	if len(p.calls) != len(want) || p.calls[0] != want[0] {
		t.Fatalf("expected proxy calls %v for orphan restart, got: %+v", want, p.calls)
	}
}

func TestStartInstance_PrintsProgressToStderr(t *testing.T) {
	store := newTempStore(t)
	seed(t, store, "my-profile", "My Profile")
	p := &fakeProxy{}
	var out strings.Builder
	var errw strings.Builder
	if err := startInstance(context.Background(), &out, &errw, p, store, "my-profile"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.Contains(errw.String(), "up to 5m0s") {
		t.Fatalf("expected 'up to 5m0s' in stderr, got: %q", errw.String())
	}
	if !strings.Contains(errw.String(), "my-profile") {
		t.Fatalf("expected profile name in stderr, got: %q", errw.String())
	}
	if strings.Contains(out.String(), "waiting for backend health") {
		t.Fatalf("progress must not appear on stdout, got: %q", out.String())
	}
}

func TestRestartInstance_PrintsUnloadAndLoadProgress(t *testing.T) {
	store := newTempStore(t)
	seed(t, store, "alpha", "Alpha")
	p := &fakeProxy{status: httpproxy.Status{Running: true, LoadedProfileID: "alpha", LoadedPID: 100}}
	m := &fakeManager{}
	var out strings.Builder
	var errw strings.Builder
	if err := restartInstance(context.Background(), &out, &errw, p, m, store, "100"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if !strings.Contains(errw.String(), "unloading") || !strings.Contains(errw.String(), "100") {
		t.Fatalf("expected unload progress with pid in stderr, got: %q", errw.String())
	}
	if !strings.Contains(errw.String(), "loading") || !strings.Contains(errw.String(), "up to 5m0s") {
		t.Fatalf("expected load progress with timeout in stderr, got: %q", errw.String())
	}
}
