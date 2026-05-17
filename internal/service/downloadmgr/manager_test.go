package downloadmgr

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestStart_SingleDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("model data"))
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "model.gguf")
	mgr := NewManager(server.Client(), 1)
	id, err := mgr.Start(Spec{URL: server.URL, DestFile: dest})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	state := waitForStatus(t, mgr, id, StatusCompleted)
	if state.Bytes != int64(len("model data")) {
		t.Errorf("Bytes: want %d, got %d", len("model data"), state.Bytes)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(data) != "model data" {
		t.Errorf("file content: want %q, got %q", "model data", string(data))
	}
}

func TestStart_ConcurrentLimit(t *testing.T) {
	blocked := make(chan struct{})
	var active int64
	var maxSeen int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt64(&active, 1)
		for {
			max := atomic.LoadInt64(&maxSeen)
			if current <= max || atomic.CompareAndSwapInt64(&maxSeen, max, current) {
				break
			}
		}
		defer atomic.AddInt64(&active, -1)
		<-blocked
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	dir := t.TempDir()
	mgr := NewManager(server.Client(), 2)
	ids := make([]ID, 0, 5)
	for i := 0; i < 5; i++ {
		id, err := mgr.Start(Spec{URL: server.URL, DestFile: filepath.Join(dir, fmt.Sprintf("file-%d", i))})
		if err != nil {
			t.Fatalf("Start %d returned error: %v", i, err)
		}
		ids = append(ids, id)
	}

	waitForActiveCount(t, mgr, 2)
	for _, id := range ids[2:] {
		state := stateByID(t, mgr, id)
		if state.Status != StatusQueued {
			t.Errorf("queued state %s: want StatusQueued, got %v", id, state.Status)
		}
	}
	close(blocked)
	for _, id := range ids {
		waitForStatus(t, mgr, id, StatusCompleted)
	}
	if got := atomic.LoadInt64(&maxSeen); got > 2 {
		t.Errorf("max concurrent requests: want <= 2, got %d", got)
	}
}

func TestCancel_Active(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "active.bin")
	mgr := NewManager(server.Client(), 1)
	id, err := mgr.Start(Spec{URL: server.URL, DestFile: dest})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("server never received request")
	}
	if err := mgr.Cancel(id); err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	waitForStatus(t, mgr, id, StatusCancelled)
	if _, err := os.Stat(dest + ".partial"); !os.IsNotExist(err) {
		t.Errorf("partial file should be removed, stat err=%v", err)
	}
}

func TestCancel_Queued(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var requests int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requests, 1)
		if r.URL.Query().Get("id") == "first" {
			close(firstStarted)
			<-releaseFirst
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	dir := t.TempDir()
	mgr := NewManager(server.Client(), 1)
	firstID, err := mgr.Start(Spec{URL: server.URL + "?id=first", DestFile: filepath.Join(dir, "first")})
	if err != nil {
		t.Fatalf("Start first returned error: %v", err)
	}
	queuedID, err := mgr.Start(Spec{URL: server.URL + "?id=queued", DestFile: filepath.Join(dir, "queued")})
	if err != nil {
		t.Fatalf("Start queued returned error: %v", err)
	}
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first request never started")
	}
	if err := mgr.Cancel(queuedID); err != nil {
		t.Fatalf("Cancel queued returned error: %v", err)
	}
	state := waitForStatus(t, mgr, queuedID, StatusCancelled)
	if state.Bytes != 0 {
		t.Errorf("queued Bytes: want 0, got %d", state.Bytes)
	}
	close(releaseFirst)
	waitForStatus(t, mgr, firstID, StatusCompleted)
	if got := atomic.LoadInt64(&requests); got != 1 {
		t.Errorf("request count: want 1, got %d", got)
	}
}

func TestSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	dir := t.TempDir()
	mgr := NewManager(server.Client(), 1)
	id, err := mgr.Start(Spec{URL: server.URL, DestFile: filepath.Join(dir, "snapshot")})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	waitForStatus(t, mgr, id, StatusCompleted)
	snapshot := mgr.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("Snapshot len: want 1, got %d", len(snapshot))
	}
	if snapshot[0].ID != id || snapshot[0].Status != StatusCompleted {
		t.Errorf("Snapshot state: got ID=%s Status=%v", snapshot[0].ID, snapshot[0].Status)
	}
}

func TestSubscribe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("event data"))
	}))
	defer server.Close()

	mgr := NewManager(server.Client(), 1)
	ch := mgr.Subscribe()
	id, err := mgr.Start(Spec{URL: server.URL, DestFile: filepath.Join(t.TempDir(), "events")})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	waitForStatus(t, mgr, id, StatusCompleted)

	seen := map[Status]bool{}
	deadline := time.After(time.Second)
	for !seen[StatusActive] || !seen[StatusCompleted] {
		select {
		case ev := <-ch:
			if ev.ID == id {
				seen[ev.State.Status] = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for active/completed events, seen=%v", seen)
		}
	}
}

func TestClose(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	mgr := NewManager(server.Client(), 1)
	ch := mgr.Subscribe()
	id, err := mgr.Start(Spec{URL: server.URL, DestFile: filepath.Join(t.TempDir(), "close")})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("server never received request")
	}
	if err := mgr.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	state := stateByID(t, mgr, id)
	if state.Status != StatusCancelled {
		t.Errorf("Status after Close: want StatusCancelled, got %v", state.Status)
	}
	select {
	case _, ok := <-ch:
		if ok {
			for ok {
				_, ok = <-ch
			}
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber channel not closed")
	}
}

func waitForStatus(t *testing.T, mgr *Manager, id ID, want Status) State {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state := stateByID(t, mgr, id)
		if state.Status == want {
			return state
		}
		time.Sleep(10 * time.Millisecond)
	}
	state := stateByID(t, mgr, id)
	t.Fatalf("status for %s: want %v, got %v", id, want, state.Status)
	return State{}
}

func waitForActiveCount(t *testing.T, mgr *Manager, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := 0
		for _, state := range mgr.Snapshot() {
			if state.Status == StatusActive {
				got++
			}
		}
		if got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("active count: want %d", want)
}

func stateByID(t *testing.T, mgr *Manager, id ID) State {
	t.Helper()
	for _, state := range mgr.Snapshot() {
		if state.ID == id {
			return state
		}
	}
	t.Fatalf("state %s missing", id)
	return State{}
}
