package proxysupervisor

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/internal/procutil"
)

// fakeSystemctl stubs runSystemctl: scripted outputs per invocation plus a
// mutable unit state machine (active/sub/pid) that start/stop/kill mutate.
type fakeSystemctl struct {
	mu     sync.Mutex
	calls  [][]string
	active string
	sub    string
	pid    int
	// failStart makes `start` return an error (unit fails to come up).
	failStart bool
	// loadState is what `show -p LoadState` reports.
	loadState string
}

func (f *fakeSystemctl) run(ctx context.Context, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]string(nil), args...)
	f.calls = append(f.calls, cp)
	joined := strings.Join(args, " ")
	switch {
	case strings.Contains(joined, "LoadState"):
		return f.loadState + "\n", nil
	case strings.Contains(joined, "ActiveState"):
		return fmt.Sprintf("%s\n%s\n%d\n", f.active, f.sub, f.pid), nil
	}
	// command verbs: args = ["--user", <verb>, ...]
	verb := ""
	for i, a := range args {
		if a == "--user" && i+1 < len(args) {
			verb = args[i+1]
			break
		}
	}
	switch verb {
	case "start":
		if f.failStart {
			return "", fmt.Errorf("job failed")
		}
		f.active, f.sub = "active", "running"
		if f.pid == 0 {
			f.pid = 424242
		}
		return "", nil
	case "stop":
		f.active, f.sub, f.pid = "inactive", "dead", 0
		return "", nil
	case "kill":
		f.active, f.sub, f.pid = "inactive", "dead", 0
		return "", nil
	case "reset-failed":
		return "", nil
	}
	return "", nil
}

func (f *fakeSystemctl) sawVerb(verb string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		for _, a := range c {
			if a == verb {
				return true
			}
		}
	}
	return false
}

// installFakeSystemctl swaps runSystemctl for the test duration.
func installFakeSystemctl(t *testing.T, f *fakeSystemctl) {
	t.Helper()
	old := runSystemctl
	runSystemctl = f.run
	t.Cleanup(func() { runSystemctl = old })
}

func addrOf(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	return strings.TrimPrefix(ts.URL, "http://")
}

func splitHostPortSup(t *testing.T, addr string) (string, int) {
	t.Helper()
	var host string
	var port int
	if _, err := fmt.Sscanf(addr, "%[^:]:%d", &host, &port); err != nil {
		// IPv6 or odd form — fall back to net.SplitHostPort
		t.Fatalf("split %q: %v", addr, err)
	}
	_ = host
	_ = port
	h, p := hostPortSplit(addr)
	return h, p
}

// hostPortSplit splits an httptest addr (127.0.0.1:port) without net import churn.
func hostPortSplit(addr string) (string, int) {
	host, portStr, ok := strings.Cut(addr, ":")
	if !ok {
		return addr, 0
	}
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	return host, port
}

// aliveSelfState builds a State pointing at our own (live) process.
func aliveSelfState() *State {
	ticks, _ := procutil.StartTicks(os.Getpid())
	return &State{PID: os.Getpid(), Host: "127.0.0.1", Port: 1, StartedAt: time.Now().UTC(), StartTicks: ticks}
}

func TestDetectSystemd_SelectsOnlyForLoadedUnit(t *testing.T) {
	for _, tc := range []struct {
		name string
		load string
		fail bool
		want bool
	}{
		{"loaded", "loaded\n", false, true},
		{"not-found", "not-found\n", false, false},
		{"empty", "", false, false},
		{"error", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSystemctl{loadState: strings.TrimSpace(tc.load)}
			old := runSystemctl
			if tc.fail {
				runSystemctl = func(ctx context.Context, args ...string) (string, error) {
					return "", fmt.Errorf("exit 1")
				}
			} else {
				runSystemctl = fake.run
			}
			defer func() { runSystemctl = old }()
			if got := detectSystemd(); got != tc.want {
				t.Fatalf("detectSystemd() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSystemdStart_IssuesStartAndWaitsHealthy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"running":true}`))
	}))
	defer ts.Close()

	fake := &fakeSystemctl{loadState: "loaded", active: "inactive", sub: "dead"}
	installFakeSystemctl(t, fake)
	s := New(Config{StatePath: filepath.Join(t.TempDir(), "s.json"), LogDir: t.TempDir()})
	host, port := hostPortSplit(addrOf(t, ts))
	s.host, s.port = host, port

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !fake.sawVerb("start") {
		t.Fatal("expected systemctl start call")
	}
	if st := s.Status(); !st.Running {
		t.Fatalf("expected Running after Start, got %+v", st)
	}
}

func TestSystemdStart_AlreadyRunning(t *testing.T) {
	fake := &fakeSystemctl{loadState: "loaded", active: "active", sub: "running", pid: 1234}
	installFakeSystemctl(t, fake)
	s := New(Config{StatePath: filepath.Join(t.TempDir(), "s.json"), LogDir: t.TempDir()})
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("expected ErrAlreadyRunning")
	} else if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("wrong error: %v", err)
	}
	if fake.sawVerb("start") {
		t.Fatal("must not issue start when already running (no duplicate proxy)")
	}
}

func TestSystemdStart_NoSilentFallbackOnFailure(t *testing.T) {
	fake := &fakeSystemctl{loadState: "loaded", active: "inactive", sub: "dead", failStart: true}
	installFakeSystemctl(t, fake)
	s := New(Config{StatePath: filepath.Join(t.TempDir(), "s.json"), LogDir: t.TempDir()})
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("expected start failure to surface, not silent fallback")
	}
	// No process fallback: the fake saw only systemctl verbs, never a spawn.
	// (A detached serve would have bound the port; nothing is listening.)
	if st := s.Status(); st.Running {
		t.Fatal("failed Start must not report running")
	}
}

func TestSystemdStop_StopsUnit(t *testing.T) {
	fake := &fakeSystemctl{loadState: "loaded", active: "active", sub: "running", pid: 1234}
	installFakeSystemctl(t, fake)
	s := New(Config{StatePath: filepath.Join(t.TempDir(), "s.json"), LogDir: t.TempDir()})
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !fake.sawVerb("stop") {
		t.Fatal("expected systemctl stop call")
	}
	if st := s.Status(); st.Running {
		t.Fatal("expected not running after Stop")
	}
}

func TestSystemdStop_NotRunning(t *testing.T) {
	fake := &fakeSystemctl{loadState: "loaded", active: "inactive", sub: "dead"}
	installFakeSystemctl(t, fake)
	s := New(Config{StatePath: filepath.Join(t.TempDir(), "s.json"), LogDir: t.TempDir()})
	if err := s.Stop(context.Background()); err == nil {
		t.Fatal("expected error when unit inactive")
	}
	if fake.sawVerb("stop") {
		t.Fatal("must not issue stop when already inactive")
	}
}

func TestSystemdForceStop_KillsCgroup(t *testing.T) {
	fake := &fakeSystemctl{loadState: "loaded", active: "active", sub: "running", pid: 1234}
	installFakeSystemctl(t, fake)
	s := New(Config{StatePath: filepath.Join(t.TempDir(), "s.json"), LogDir: t.TempDir()})
	if err := s.ForceStop(); err != nil {
		t.Fatalf("ForceStop: %v", err)
	}
	if !fake.sawVerb("kill") {
		t.Fatal("expected systemctl kill call")
	}
	// kill must target the whole cgroup with SIGKILL
	found := false
	fake.mu.Lock()
	for _, c := range fake.calls {
		j := strings.Join(c, " ")
		if strings.Contains(j, "SIGKILL") && strings.Contains(j, "--kill-whom=all") {
			found = true
		}
	}
	fake.mu.Unlock()
	if !found {
		t.Fatalf("kill must use SIGKILL --kill-whom=all; calls=%v", fake.calls)
	}
	if st := s.Status(); st.Running {
		t.Fatal("expected not running after ForceStop")
	}
}

func TestSystemdStatus_DegradedWhenHTTPDown(t *testing.T) {
	fake := &fakeSystemctl{loadState: "loaded", active: "active", sub: "running", pid: 1234}
	installFakeSystemctl(t, fake)
	s := New(Config{
		StatePath: filepath.Join(t.TempDir(), "s.json"),
		LogDir:    t.TempDir(),
		Host:      "127.0.0.1",
		Port:      44992, // nothing listening
	})
	st := s.Status()
	if !st.Running {
		t.Fatal("active unit with dead HTTP must stay Running (degraded)")
	}
	if !strings.HasPrefix(st.LastError, "status_probe_failed") {
		t.Fatalf("expected status_probe_failed LastError, got %q", st.LastError)
	}
}

func TestSystemdStatus_InactiveIsStopped(t *testing.T) {
	fake := &fakeSystemctl{loadState: "loaded", active: "inactive", sub: "dead"}
	installFakeSystemctl(t, fake)
	s := New(Config{StatePath: filepath.Join(t.TempDir(), "s.json"), LogDir: t.TempDir()})
	if st := s.Status(); st.Running {
		t.Fatal("inactive unit must report stopped")
	}
}

func TestSystemdReconcile_DropsOnlyObsoleteLegacy(t *testing.T) {
	// Obsolete (dead PID) → dropped.
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	if err := saveState(path, &State{PID: 999999, Host: "127.0.0.1", Port: 1}); err != nil {
		t.Fatal(err)
	}
	fake := &fakeSystemctl{loadState: "loaded"}
	installFakeSystemctl(t, fake)
	s := New(Config{StatePath: path, LogDir: dir})
	if err := s.Reconcile(); err != nil {
		t.Fatal(err)
	}
	if st, _ := loadState(path); st != nil {
		t.Fatal("obsolete legacy state must be dropped")
	}

	// Live PID (ours) → kept on disk, never adopted.
	path2 := filepath.Join(dir, "s2.json")
	if err := saveState(path2, aliveSelfState()); err != nil {
		t.Fatal(err)
	}
	s2 := New(Config{StatePath: path2, LogDir: dir})
	if err := s2.Reconcile(); err != nil {
		t.Fatal(err)
	}
	if st, _ := loadState(path2); st == nil {
		t.Fatal("live legacy state must be left alone (may be a manual serve)")
	}
	if s2.Status().Running {
		t.Fatal("systemd Status must reflect the unit, not adopted disk state")
	}
}

func TestSystemdStatus_QueryErrorFallsBackToHTTP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"running":true}`))
	}))
	defer ts.Close()

	old := runSystemctl
	runSystemctl = func(ctx context.Context, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "LoadState") {
			return "loaded\n", nil
		}
		return "", fmt.Errorf("transient dbus wobble")
	}
	defer func() { runSystemctl = old }()

	s := New(Config{StatePath: filepath.Join(t.TempDir(), "s.json"), LogDir: t.TempDir()})
	host, port := hostPortSplit(addrOf(t, ts))
	s.host, s.port = host, port

	// Healthy HTTP beats a failed control query: no UI flap.
	if st := s.Status(); !st.Running || st.LastError != "" {
		t.Fatalf("expected healthy fallback, got %+v", st)
	}
}
