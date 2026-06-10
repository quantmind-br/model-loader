package benchmark

import (
	"context"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
)

// fakeProxyCtl is an in-memory ProxyController: Load records the requested
// profile IDs and flips the status to "loaded".
type fakeProxyCtl struct {
	base   string
	loaded []string
	st     httpproxy.Status
}

func (f *fakeProxyCtl) EnsureRunning(context.Context) error { return nil }

func (f *fakeProxyCtl) Load(_ context.Context, id string) (httpproxy.Status, error) {
	f.loaded = append(f.loaded, id)
	f.st = httpproxy.Status{Running: true, LoadedProfileID: id, LoadedPID: 7, LoadedLogPath: "/tmp/b.log"}
	return f.st, nil
}

func (f *fakeProxyCtl) Status() httpproxy.Status { return f.st }
func (f *fakeProxyCtl) BaseURL() string          { return f.base }

func TestEnsureLoaded_UsesProxy(t *testing.T) {
	fp := &fakeProxyCtl{base: "http://127.0.0.1:9999"}
	r := &Runner{proxy: fp}

	base, logPath, pid, reused, err := r.ensureLoaded(context.Background(), domain.Profile{ID: "p1"})
	if err != nil {
		t.Fatalf("ensureLoaded: %v", err)
	}
	if base != "http://127.0.0.1:9999" {
		t.Errorf("base = %q, want proxy base URL", base)
	}
	if logPath != "/tmp/b.log" {
		t.Errorf("logPath = %q, want /tmp/b.log", logPath)
	}
	if pid != 7 {
		t.Errorf("pid = %d, want 7", pid)
	}
	if reused {
		t.Error("reused = true on first load, want false")
	}
	if len(fp.loaded) != 1 || fp.loaded[0] != "p1" {
		t.Errorf("proxy loads = %v, want [p1]", fp.loaded)
	}

	// Second call: the profile is already loaded in the proxy → warm run.
	_, _, _, reused, err = r.ensureLoaded(context.Background(), domain.Profile{ID: "p1"})
	if err != nil {
		t.Fatalf("ensureLoaded (second): %v", err)
	}
	if !reused {
		t.Error("reused = false on second load of the same profile, want true")
	}
}

func TestEnsureLoaded_NilProxy(t *testing.T) {
	r := &Runner{}
	if _, _, _, _, err := r.ensureLoaded(context.Background(), domain.Profile{ID: "p1"}); err == nil {
		t.Fatal("ensureLoaded with nil proxy: want error, got nil")
	}
}
