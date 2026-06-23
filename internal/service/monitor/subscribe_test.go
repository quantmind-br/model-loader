package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestSubscribe_FansInLogsSlotsAndHealth(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "server.log")
	if err := os.WriteFile(logPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/slots":
			_ = json.NewEncoder(w).Encode([]Slot{{ID: 0, State: "idle", NCtxMax: 4096}})
		}
	}))
	defer srv.Close()

	mgr := New(Config{
		SlotsTickInterval: 50 * time.Millisecond,
		GPUTickInterval:   200 * time.Millisecond,
		LogRingSize:       100,
		MetricsWindow:     time.Second,
	})

	port := mustPort(t, srv.URL)
	ch, cancel, err := mgr.Subscribe(99999, port, logPath)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	go func() {
		time.Sleep(60 * time.Millisecond)
		_ = appendLine(logPath, "hello")
	}()

	gotLog, gotSlots, gotHealth := false, false, false
	deadline := time.After(3 * time.Second)
	for !gotLog || !gotSlots || !gotHealth {
		select {
		case ev := <-ch:
			switch ev.Source {
			case SourceLogs:
				gotLog = true
			case SourceSlots:
				gotSlots = true
			case SourceHealth:
				gotHealth = true
			}
		case <-deadline:
			t.Fatalf("timeout: log=%v slots=%v health=%v", gotLog, gotSlots, gotHealth)
		}
	}
}

func TestSubscribe_RejectsEmptyLogPath(t *testing.T) {
	mgr := New(Config{})
	if _, _, err := mgr.Subscribe(123, 8080, ""); err == nil {
		t.Fatal("expected ErrLogPathEmpty")
	}
}

// TestSubscribe_DropsOnBackpressure pins the backpressure contract (BUGS.md
// T2): every pump sends to the event channel non-blockingly
// (select { case out <- ev: default: }), so a slow/absent consumer causes
// drops, never a stall. We deliberately never drain the channel, fire a burst
// of writes far larger than the 256-deep buffers, and assert that cancel()
// still returns promptly. If a pump blocked on a full channel instead of
// dropping, it would never reach its ctx.Done() case and cancel() would
// deadlock.
func TestSubscribe_DropsOnBackpressure(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "server.log")
	if err := os.WriteFile(logPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/slots":
			_ = json.NewEncoder(w).Encode([]Slot{{ID: 0, State: "idle", NCtxMax: 4096}})
		}
	}))
	defer srv.Close()

	mgr := New(Config{
		SlotsTickInterval: 10 * time.Millisecond,
		GPUTickInterval:   200 * time.Millisecond,
		LogRingSize:       100,
		MetricsWindow:     time.Second,
	})

	port := mustPort(t, srv.URL)
	_, cancel, err := mgr.Subscribe(99999, port, logPath)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	// Deliberately do NOT drain the returned channel — force backpressure.

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	for range 2000 {
		if _, err := f.WriteString("line\n"); err != nil {
			t.Fatal(err)
		}
	}
	_ = f.Close()

	// Let the pumps process/drop the burst against the undrained channel.
	time.Sleep(100 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		_ = cancel()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel() did not return; a pump blocked on backpressure instead of dropping")
	}
}

// helpers
func mustPort(t *testing.T, urlStr string) int {
	t.Helper()
	u, err := url.Parse(urlStr)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}
