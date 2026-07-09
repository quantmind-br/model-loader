package downloadmgr

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/internal/fsx"
)

// inProcessSpawner runs RunWorker in a goroutine and returns the current
// process PID (always alive) so pidAlive checks pass during the test.
// It captures the worker's expected user-agent.
func inProcessSpawner(t *testing.T, client *http.Client, wg *sync.WaitGroup) Spawner {
	t.Helper()
	return func(statePath, userAgent string) (int, error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = RunWorker(WorkerConfig{
				StatePath:          statePath,
				Client:             client,
				UserAgent:          userAgent,
				CheckpointBytes:    1024,
				CheckpointInterval: 10 * time.Millisecond,
			})
		}()
		return syscall.Getpid(), nil
	}
}

func TestManager_StartSpawnsWorkerAndCompletes(t *testing.T) {
	body := "model bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	var wg sync.WaitGroup
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.gguf")
	mgr := NewManager(dir, 1).
		WithSpawner(inProcessSpawner(t, srv.Client(), &wg)).
		WithPollInterval(20 * time.Millisecond)
	mgr.StartPolling()
	defer mgr.Close()

	id, err := mgr.Start(Spec{URL: srv.URL, DestFile: dest})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForStatus(t, mgr, id, StatusCompleted)
	wg.Wait()
}

func TestManager_QueueBeyondConcurrency(t *testing.T) {
	release := make(chan struct{})
	var inflight int64
	var maxSeen int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := atomic.AddInt64(&inflight, 1)
		for {
			m := atomic.LoadInt64(&maxSeen)
			if now <= m || atomic.CompareAndSwapInt64(&maxSeen, m, now) {
				break
			}
		}
		defer atomic.AddInt64(&inflight, -1)
		<-release
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	var wg sync.WaitGroup
	dir := t.TempDir()
	mgr := NewManager(dir, 2).
		WithSpawner(inProcessSpawner(t, srv.Client(), &wg)).
		WithPollInterval(20 * time.Millisecond)
	mgr.StartPolling()
	defer mgr.Close()

	ids := make([]ID, 0, 4)
	for i := 0; i < 4; i++ {
		id, err := mgr.Start(Spec{URL: srv.URL, DestFile: filepath.Join(dir, "f"+string(rune('a'+i)))})
		if err != nil {
			t.Fatalf("Start %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	// Two should be active concurrently; rest queued.
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt64(&inflight) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&inflight); got != 2 {
		t.Errorf("inflight = %d, want 2", got)
	}
	close(release)
	for _, id := range ids {
		waitForStatus(t, mgr, id, StatusCompleted)
	}
	if got := atomic.LoadInt64(&maxSeen); got > 2 {
		t.Errorf("max concurrent = %d, want <= 2", got)
	}
	wg.Wait()
}

func TestManager_CancelQueuedRecord(t *testing.T) {
	// Active-worker SIGTERM is verified end-to-end in the tmux smoke
	// test (sending SIGTERM to the in-process worker would kill the
	// test runner). This test covers the queued-record branch: cancel
	// before a spawn happens → record flipped to StatusCancelled and
	// no worker is ever started.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	var wg sync.WaitGroup
	dir := t.TempDir()
	mgr := NewManager(dir, 1).
		WithSpawner(inProcessSpawner(t, srv.Client(), &wg)).
		WithPollInterval(20 * time.Millisecond)
	mgr.StartPolling()
	defer mgr.Close()
	// Declared last so it runs FIRST (LIFO): unblock any in-flight handler before
	// srv.Close()/mgr.Close() wait on connections, avoiding a teardown deadlock.
	defer close(release)

	first, _ := mgr.Start(Spec{URL: srv.URL, DestFile: filepath.Join(dir, "first")})
	queued, _ := mgr.Start(Spec{URL: srv.URL, DestFile: filepath.Join(dir, "queued")})

	if err := mgr.Cancel(queued); err != nil {
		t.Fatalf("Cancel queued: %v", err)
	}
	state := waitForStatus(t, mgr, queued, StatusCancelled)
	if state.Bytes != 0 {
		t.Errorf("queued cancel must not have downloaded bytes; got %d", state.Bytes)
	}
	_ = first
}

func TestManager_ReconcileMarksDeadWorkersAbandoned(t *testing.T) {
	dir := t.TempDir()
	id := ID("rec-dead")
	rec := DownloadRecord{
		ID:       id,
		PID:      99999999, // very likely dead
		URL:      "http://example",
		DestFile: filepath.Join(dir, "x"),
		Status:   StatusActive,
		Bytes:    100,
		Total:    1000,
	}
	if err := SaveRecord(dir, rec); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(dir, 1)
	if err := mgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got, _ := LoadRecord(StatePath(dir, id))
	if got.Status != StatusAbandoned {
		t.Errorf("Status = %v, want StatusAbandoned", got.Status)
	}
}

func TestManager_ReconcileKeepsLiveWorkers(t *testing.T) {
	dir := t.TempDir()
	id := ID("rec-live")
	rec := DownloadRecord{
		ID:       id,
		PID:      syscall.Getpid(), // current process is alive
		URL:      "http://example",
		DestFile: filepath.Join(dir, "x"),
		Status:   StatusActive,
		Bytes:    100,
	}
	if err := SaveRecord(dir, rec); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(dir, 1)
	if err := mgr.Reconcile(); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadRecord(StatePath(dir, id))
	if got.Status != StatusActive {
		t.Errorf("Status = %v, want StatusActive (live PID)", got.Status)
	}
}

func TestManager_ReconcileSpawnsQueuedWhenCapacityAvailable(t *testing.T) {
	body := "model bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	id := ID("rec-queued")
	rec := DownloadRecord{
		ID:       id,
		PID:      0,
		URL:      srv.URL,
		DestFile: filepath.Join(dir, "x"),
		Status:   StatusQueued,
	}
	if err := SaveRecord(dir, rec); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	mgr := NewManager(dir, 1).
		WithSpawner(inProcessSpawner(t, srv.Client(), &wg)).
		WithPollInterval(20 * time.Millisecond)
	if err := mgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	mgr.StartPolling()
	defer mgr.Close()

	waitForStatus(t, mgr, id, StatusCompleted)
	wg.Wait()
}

func TestManager_ReconcileLeavesQueuedWhenAtCapacity(t *testing.T) {
	dir := t.TempDir()

	activeID := ID("rec-active")
	active := DownloadRecord{
		ID:       activeID,
		PID:      syscall.Getpid(), // current process is alive, occupies the only slot
		URL:      "http://example",
		DestFile: filepath.Join(dir, "active"),
		Status:   StatusActive,
	}
	if err := SaveRecord(dir, active); err != nil {
		t.Fatal(err)
	}

	queuedID := ID("rec-queued")
	queued := DownloadRecord{
		ID:       queuedID,
		PID:      0,
		URL:      "http://example",
		DestFile: filepath.Join(dir, "queued"),
		Status:   StatusQueued,
	}
	if err := SaveRecord(dir, queued); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(dir, 1)
	if err := mgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got, _ := LoadRecord(StatePath(dir, queuedID))
	if got.Status != StatusQueued {
		t.Errorf("Status = %v, want StatusQueued (no free slot, must not be abandoned or spawned)", got.Status)
	}
	if got.PID != 0 {
		t.Errorf("PID = %d, want 0 (must not have been spawned)", got.PID)
	}
}

// TestManager_ReconcilePromotesOnlyFreeSlotsWhenPartiallyBusy pins the
// maxConcurrent>=2-with-some-active-workers case: promoteQueued never
// mutates m.active itself (spawn() does, after the lock is released), so
// passing the raw cap instead of the actual free-slot count would spawn
// every queued record regardless of how many workers are already active.
func TestManager_ReconcilePromotesOnlyFreeSlotsWhenPartiallyBusy(t *testing.T) {
	dir := t.TempDir()

	activeID := ID("rec-active")
	active := DownloadRecord{
		ID:       activeID,
		PID:      syscall.Getpid(), // alive, occupies 1 of the 2 slots
		URL:      "http://example",
		DestFile: filepath.Join(dir, "active"),
		Status:   StatusActive,
	}
	if err := SaveRecord(dir, active); err != nil {
		t.Fatal(err)
	}

	queuedIDs := []ID{"rec-queued-a", "rec-queued-b"}
	for _, id := range queuedIDs {
		rec := DownloadRecord{
			ID:       id,
			PID:      0,
			URL:      "http://example",
			DestFile: filepath.Join(dir, string(id)),
			Status:   StatusQueued,
		}
		if err := SaveRecord(dir, rec); err != nil {
			t.Fatal(err)
		}
	}

	var spawnCount int32
	countingSpawner := func(statePath, userAgent string) (int, error) {
		atomic.AddInt32(&spawnCount, 1)
		return syscall.Getpid(), nil
	}

	mgr := NewManager(dir, 2).WithSpawner(countingSpawner)
	if err := mgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if got := atomic.LoadInt32(&spawnCount); got != 1 {
		t.Fatalf("spawn count = %d, want 1 (maxConcurrent=2, 1 already active -> only 1 free slot)", got)
	}

	var stillQueued, nowActive int
	for _, id := range queuedIDs {
		rec, err := LoadRecord(StatePath(dir, id))
		if err != nil {
			t.Fatal(err)
		}
		switch rec.Status {
		case StatusQueued:
			stillQueued++
		case StatusActive:
			nowActive++
		default:
			t.Errorf("record %s: unexpected status %v", id, rec.Status)
		}
	}
	if stillQueued != 1 || nowActive != 1 {
		t.Fatalf("queued=%d active=%d, want exactly one of each", stillQueued, nowActive)
	}
}

// TestManager_ResumeDedupesQueue guards audit N-C10: two Resume calls for the
// same id while the slot is busy must queue it exactly once, so promotion
// spawns exactly one worker (a duplicate would respawn over the same partial).
func TestManager_ResumeDedupesQueue(t *testing.T) {
	dir := t.TempDir()

	activeID := ID("rec-active")
	if err := SaveRecord(dir, DownloadRecord{
		ID: activeID, PID: syscall.Getpid(), URL: "http://example",
		DestFile: filepath.Join(dir, "active"), Status: StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	failedID := ID("rec-failed")
	if err := SaveRecord(dir, DownloadRecord{
		ID: failedID, PID: 0, URL: "http://example",
		DestFile: filepath.Join(dir, "failed"), Status: StatusFailed,
	}); err != nil {
		t.Fatal(err)
	}

	var spawnCount int32
	countingSpawner := func(statePath, userAgent string) (int, error) {
		atomic.AddInt32(&spawnCount, 1)
		return syscall.Getpid(), nil
	}

	mgr := NewManager(dir, 1).WithSpawner(countingSpawner)
	if err := mgr.Reconcile(); err != nil { // populates m.active with rec-active
		t.Fatalf("Reconcile: %v", err)
	}

	// Slot is full: both Resume calls must queue failedID, but only once.
	if err := mgr.Resume(failedID); err != nil {
		t.Fatalf("Resume #1: %v", err)
	}
	if err := mgr.Resume(failedID); err != nil {
		t.Fatalf("Resume #2: %v", err)
	}
	mgr.mu.Lock()
	queued := 0
	for _, q := range mgr.queue {
		if q == failedID {
			queued++
		}
	}
	mgr.mu.Unlock()
	if queued != 1 {
		t.Fatalf("queue contains failedID %d times, want 1 (dedup)", queued)
	}

	// Free the slot and drive a poll: exactly one worker spawns for failedID.
	active, err := LoadRecord(StatePath(dir, activeID))
	if err != nil {
		t.Fatal(err)
	}
	active.Status = StatusCompleted
	if err := SaveRecord(dir, active); err != nil {
		t.Fatal(err)
	}
	mgr.tick()

	if got := atomic.LoadInt32(&spawnCount); got != 1 {
		t.Fatalf("spawn count = %d, want 1 (deduped resume spawns once)", got)
	}
}

// TestManager_ReconcileSkipsSpawnWhenAlreadyClaimed pins the cross-process
// guard: two separate Manager instances (e.g. the long-lived TUI and a
// one-shot CLI invocation) share the same stateDir with no other
// coordination, so a still-queued record's claim marker — created by
// whichever instance's spawn() call wins — must stop every other instance
// from also spawning a worker for it.
func TestManager_ReconcileSkipsSpawnWhenAlreadyClaimed(t *testing.T) {
	dir := t.TempDir()
	id := ID("rec-queued")
	rec := DownloadRecord{
		ID:       id,
		PID:      0,
		URL:      "http://example",
		DestFile: filepath.Join(dir, "x"),
		Status:   StatusQueued,
	}
	if err := SaveRecord(dir, rec); err != nil {
		t.Fatal(err)
	}
	// Simulate another Manager instance's spawn() already having won the
	// claim (e.g. a concurrent CLI invocation reconciling the same id).
	if err := fsx.WriteJSONExclusive(claimPath(dir, id), struct{}{}); err != nil {
		t.Fatalf("seed claim: %v", err)
	}

	var spawnCount int32
	countingSpawner := func(statePath, userAgent string) (int, error) {
		atomic.AddInt32(&spawnCount, 1)
		return syscall.Getpid(), nil
	}

	mgr := NewManager(dir, 1).WithSpawner(countingSpawner)
	if err := mgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if got := atomic.LoadInt32(&spawnCount); got != 0 {
		t.Fatalf("spawn count = %d, want 0 (id already claimed by another instance)", got)
	}
	got, err := LoadRecord(StatePath(dir, id))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusQueued || got.PID != 0 {
		t.Fatalf("record = %+v, want unchanged StatusQueued/PID:0", got)
	}
}

// TestManager_SpawnRemovesClaimAfterCompleting ensures the claim marker is
// cleaned up once a spawn attempt finishes (success or failure) — leaving
// it behind would permanently block any future Resume/Reconcile from ever
// retrying that id, which would be a worse regression than the race the
// claim itself closes.
func TestManager_SpawnRemovesClaimAfterCompleting(t *testing.T) {
	dir := t.TempDir()
	id := ID("rec-queued")
	rec := DownloadRecord{
		ID:       id,
		PID:      0,
		URL:      "http://example",
		DestFile: filepath.Join(dir, "x"),
		Status:   StatusQueued,
	}
	if err := SaveRecord(dir, rec); err != nil {
		t.Fatal(err)
	}

	countingSpawner := func(statePath, userAgent string) (int, error) {
		return syscall.Getpid(), nil
	}
	mgr := NewManager(dir, 1).WithSpawner(countingSpawner)
	if err := mgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if _, err := os.Stat(claimPath(dir, id)); !os.IsNotExist(err) {
		t.Fatalf("claim file still exists after spawn completed: err = %v", err)
	}
}

func TestManager_ClearTerminalRemovesFinishedRecords(t *testing.T) {
	dir := t.TempDir()
	live := ID("rec-live")
	terminal := map[ID]Status{
		"rec-done":      StatusCompleted,
		"rec-failed":    StatusFailed,
		"rec-cancelled": StatusCancelled,
		"rec-abandoned": StatusAbandoned,
	}
	for id, status := range terminal {
		if err := SaveRecord(dir, DownloadRecord{ID: id, URL: "http://x", DestFile: filepath.Join(dir, string(id)), Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	// A genuinely running download (live PID) must survive the clear.
	if err := SaveRecord(dir, DownloadRecord{ID: live, PID: syscall.Getpid(), URL: "http://x", DestFile: filepath.Join(dir, "live"), Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(dir, 1)
	if err := mgr.Reconcile(); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	removed, err := mgr.ClearTerminal()
	if err != nil {
		t.Fatalf("ClearTerminal: %v", err)
	}
	if removed != len(terminal) {
		t.Errorf("removed = %d, want %d", removed, len(terminal))
	}

	// Terminal records are gone from disk and from the snapshot.
	for id := range terminal {
		if _, err := LoadRecord(StatePath(dir, id)); err == nil {
			t.Errorf("record %s still on disk after ClearTerminal", id)
		}
	}
	remaining := make(map[ID]bool)
	for _, st := range mgr.Snapshot() {
		remaining[st.ID] = true
	}
	if len(remaining) != 1 || !remaining[live] {
		t.Errorf("Snapshot after clear = %v, want only the live download", remaining)
	}
}

func TestManager_ResumeRespawnsAbandoned(t *testing.T) {
	body := "abc12345"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			// Respect range: serve remainder.
			w.Header().Set("Content-Range", "bytes 3-7/8")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte(body[3:]))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	var wg sync.WaitGroup
	dir := t.TempDir()
	dest := filepath.Join(dir, "resume.bin")

	// Seed an abandoned record with a partial file.
	id := ID("res-test")
	if err := SaveRecord(dir, DownloadRecord{
		ID: id, URL: srv.URL, DestFile: dest,
		Status: StatusAbandoned, Bytes: 3, Total: int64(len(body)),
	}); err != nil {
		t.Fatal(err)
	}
	// Place a .partial with the first 3 bytes already on disk.
	if err := os.WriteFile(dest+".partial", []byte(body[:3]), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(dir, 1).
		WithSpawner(inProcessSpawner(t, srv.Client(), &wg)).
		WithPollInterval(20 * time.Millisecond)
	mgr.StartPolling()
	defer mgr.Close()

	if err := mgr.Resume(id); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	waitForStatus(t, mgr, id, StatusCompleted)
	wg.Wait()
}

func TestManager_ResumeRejectsActive(t *testing.T) {
	dir := t.TempDir()
	id := ID("res-active")
	_ = SaveRecord(dir, DownloadRecord{
		ID: id, URL: "u", DestFile: filepath.Join(dir, "x"), Status: StatusActive,
	})
	mgr := NewManager(dir, 1)
	if err := mgr.Resume(id); err != ErrNotResumable {
		t.Errorf("err = %v, want ErrNotResumable", err)
	}
}

func TestManager_SubscribeReceivesEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	defer srv.Close()

	var wg sync.WaitGroup
	dir := t.TempDir()
	mgr := NewManager(dir, 1).
		WithSpawner(inProcessSpawner(t, srv.Client(), &wg)).
		WithPollInterval(10 * time.Millisecond)
	mgr.StartPolling()
	defer mgr.Close()

	ch := mgr.Subscribe()
	id, _ := mgr.Start(Spec{URL: srv.URL, DestFile: filepath.Join(dir, "evt.bin")})

	seen := map[Status]bool{}
	deadline := time.After(2 * time.Second)
	for !seen[StatusActive] || !seen[StatusCompleted] {
		select {
		case ev := <-ch:
			if ev.ID == id {
				seen[ev.State.Status] = true
			}
		case <-deadline:
			t.Fatalf("missing events; seen=%v", seen)
		}
	}
	wg.Wait()
}

func TestManager_CloseDoesNotKillWorker(t *testing.T) {
	// The whole point of the orchestrator: Close stops polling and
	// closes subscriber channels but leaves the worker PID alone.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	var wg sync.WaitGroup
	dir := t.TempDir()
	mgr := NewManager(dir, 1).
		WithSpawner(inProcessSpawner(t, srv.Client(), &wg)).
		WithPollInterval(20 * time.Millisecond)
	mgr.StartPolling()

	id, _ := mgr.Start(Spec{URL: srv.URL, DestFile: filepath.Join(dir, "c.bin")})
	waitForStatus(t, mgr, id, StatusCompleted)
	if err := mgr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := mgr.Close(); err != nil {
		t.Errorf("second Close errored: %v", err)
	}
	wg.Wait()
}

// waitForStatus polls the manager Snapshot until id reaches want or the
// deadline expires.
func waitForStatus(t *testing.T, mgr *Manager, id ID, want Status) State {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, st := range mgr.Snapshot() {
			if st.ID == id && st.Status == want {
				return st
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, st := range mgr.Snapshot() {
		if st.ID == id {
			t.Fatalf("status for %s: want %v, got %v (err=%v)", id, want, st.Status, st.Err)
		}
	}
	t.Fatalf("record %s never appeared in snapshot", id)
	return State{}
}

