// Package downloadmgr orchestrates HuggingFace file downloads via detached
// worker subprocesses. The TUI calls Manager methods to start, cancel,
// and resume downloads; each Start spawns a `model-loader download
// <state-path>` subprocess in its own session (Setsid) so the worker
// survives TUI exit. Workers persist their progress to per-download JSON
// files under stateDir, which the Manager polls and converts into Event
// stream + Snapshot view consumed by the UI.
package downloadmgr

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"sync"
	"syscall"
	"time"
)

// ErrManagerClosed is returned by Start/Cancel/Resume after Close.
var ErrManagerClosed = errors.New("download manager closed")

// ErrNotResumable is returned by Resume when a record's status disallows
// re-spawning (only StatusAbandoned and StatusFailed are resumable).
var ErrNotResumable = errors.New("download not in a resumable state")

// Spawner abstracts subprocess creation so tests can substitute an
// in-process worker without exec'ing a real binary. Returns the PID of
// the freshly spawned worker.
type Spawner func(statePath, userAgent string) (int, error)

// DefaultSpawner runs `model-loader download <state-path>` detached
// (Setsid: true) so the worker survives parent (TUI) exit. The worker
// reads/writes the state file at state-path and inherits no streams.
func DefaultSpawner(statePath, userAgent string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("locate executable: %w", err)
	}
	cmd := exec.Command(exe, "download", statePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = append(os.Environ(), "MODEL_LOADER_USER_AGENT="+userAgent)
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("spawn worker: %w", err)
	}
	pid := cmd.Process.Pid
	// Reap the zombie when the worker exits — we don't wait for it,
	// but Go's exec leaves Process around until Release/Wait.
	go func() { _ = cmd.Wait() }()
	return pid, nil
}

// Manager coordinates worker subprocesses for download requests. It
// holds no goroutine for the transfer itself — workers do that — but
// runs a background poller that converts on-disk state mutations into
// Events for subscribers.
type Manager struct {
	stateDir      string
	maxConcurrent int
	userAgent     string
	spawner       Spawner
	pollInterval  time.Duration

	mu           sync.Mutex
	closed       bool
	active       map[ID]int                // id -> PID
	queue        []ID                      // FIFO of pending IDs
	lastSnapshot map[ID]DownloadRecord     // for change detection
	subscribers  []chan Event
	pollStop     chan struct{}
	pollDone     chan struct{}
}

// NewManager creates a Manager rooted at stateDir. maxConcurrent caps
// the number of worker subprocesses that may be active simultaneously
// (queued downloads wait their turn). stateDir is created on first
// Save.
func NewManager(stateDir string, maxConcurrent int) *Manager {
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}
	return &Manager{
		stateDir:      stateDir,
		maxConcurrent: maxConcurrent,
		spawner:       DefaultSpawner,
		pollInterval:  500 * time.Millisecond,
		active:        make(map[ID]int),
		lastSnapshot:  make(map[ID]DownloadRecord),
	}
}

// WithUserAgent overrides the User-Agent header that spawned workers
// receive via the MODEL_LOADER_USER_AGENT environment variable.
func (m *Manager) WithUserAgent(ua string) *Manager {
	m.userAgent = ua
	return m
}

// WithSpawner overrides the subprocess spawner (tests inject an
// in-process fake).
func (m *Manager) WithSpawner(s Spawner) *Manager {
	if s != nil {
		m.spawner = s
	}
	return m
}

// WithPollInterval overrides how frequently the poller reads state
// files. Defaults to 500ms.
func (m *Manager) WithPollInterval(d time.Duration) *Manager {
	if d > 0 {
		m.pollInterval = d
	}
	return m
}

// Start writes an initial state record and, if capacity is available,
// spawns a worker for it. Otherwise the download is queued.
func (m *Manager) Start(spec Spec) (ID, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", ErrManagerClosed
	}
	id := NewID()
	rec := RecordFromSpec(id, spec)
	if err := SaveRecord(m.stateDir, rec); err != nil {
		m.mu.Unlock()
		return "", err
	}
	m.lastSnapshot[id] = rec

	var spawnNow bool
	if len(m.active) < m.maxConcurrent {
		spawnNow = true
	} else {
		m.queue = append(m.queue, id)
	}
	m.mu.Unlock()

	m.broadcast(Event{ID: id, State: rec.ToState()})

	if spawnNow {
		if err := m.spawn(id); err != nil {
			return id, err
		}
	}
	return id, nil
}

// spawn runs the spawner for id, updates the record with PID + active
// status, and registers the worker in m.active. Errors during spawn
// are recorded as terminal failures so the UI sees them.
func (m *Manager) spawn(id ID) error {
	rec, err := LoadRecord(StatePath(m.stateDir, id))
	if err != nil {
		return err
	}
	pid, spawnErr := m.spawner(StatePath(m.stateDir, id), m.userAgent)
	if spawnErr != nil {
		rec.Status = StatusFailed
		rec.Err = spawnErr.Error()
		_ = SaveRecord(m.stateDir, rec)
		m.broadcast(Event{ID: id, State: rec.ToState()})
		return spawnErr
	}
	rec.PID = pid
	rec.Status = StatusActive
	rec.Err = ""
	_ = SaveRecord(m.stateDir, rec)
	m.mu.Lock()
	m.active[id] = pid
	m.lastSnapshot[id] = rec
	m.mu.Unlock()
	m.broadcast(Event{ID: id, State: rec.ToState()})
	return nil
}

// Cancel asks the worker (if any) to terminate via SIGTERM. The worker
// itself writes status=cancelled and removes its .partial file. For
// queued items (no worker yet) the record is updated directly.
func (m *Manager) Cancel(id ID) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrManagerClosed
	}
	pid, isActive := m.active[id]
	for i, qid := range m.queue {
		if qid == id {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			break
		}
	}
	m.mu.Unlock()

	if isActive {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("signal worker: %w", err)
		}
		return nil
	}
	rec, err := LoadRecord(StatePath(m.stateDir, id))
	if err != nil {
		return nil
	}
	if rec.Status.IsTerminal() {
		return nil
	}
	rec.Status = StatusCancelled
	if err := SaveRecord(m.stateDir, rec); err != nil {
		return err
	}
	m.mu.Lock()
	m.lastSnapshot[id] = rec
	m.mu.Unlock()
	m.broadcast(Event{ID: id, State: rec.ToState()})
	return nil
}

// Resume re-spawns a worker for a previously abandoned or failed
// download. The worker detects the existing .partial file and sends a
// Range header to continue from where the prior run left off.
func (m *Manager) Resume(id ID) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrManagerClosed
	}
	if _, busy := m.active[id]; busy {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	rec, err := LoadRecord(StatePath(m.stateDir, id))
	if err != nil {
		return err
	}
	if rec.Status != StatusAbandoned && rec.Status != StatusFailed {
		return ErrNotResumable
	}

	m.mu.Lock()
	canSpawn := len(m.active) < m.maxConcurrent
	if !canSpawn {
		m.queue = append(m.queue, id)
	}
	m.mu.Unlock()

	if canSpawn {
		return m.spawn(id)
	}
	return nil
}

// Snapshot returns the union of in-memory tracked records and on-disk
// records, sorted by StartedAt ascending so the UI shows oldest first.
func (m *Manager) Snapshot() []State {
	recs, _ := ListRecords(m.stateDir)
	out := make([]State, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.ToState())
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out
}

// Subscribe returns a channel that receives state-change events.
func (m *Manager) Subscribe() <-chan Event {
	ch := make(chan Event, 32)
	m.mu.Lock()
	if m.closed {
		close(ch)
	} else {
		m.subscribers = append(m.subscribers, ch)
	}
	m.mu.Unlock()
	return ch
}

// Reconcile is called at boot, before StartPolling. It scans the state
// directory, validates that each non-terminal record's worker is still
// alive, marks orphaned ones as StatusAbandoned, and primes the active
// map for surviving workers.
func (m *Manager) Reconcile() error {
	recs, err := ListRecords(m.stateDir)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrManagerClosed
	}
	for _, r := range recs {
		switch {
		case r.Status.IsTerminal():
			m.lastSnapshot[r.ID] = r
		case r.PID > 0 && pidAlive(r.PID):
			m.active[r.ID] = r.PID
			m.lastSnapshot[r.ID] = r
		default:
			r.Status = StatusAbandoned
			if r.PID == 0 {
				// Was queued but the previous TUI exited before
				// spawning. We mark it abandoned so the user can
				// resume on demand.
				r.Err = "queued worker never spawned"
			} else {
				r.Err = "worker process exited without writing terminal status"
			}
			_ = SaveRecord(m.stateDir, r)
			m.lastSnapshot[r.ID] = r
		}
	}
	return nil
}

// StartPolling kicks off the background goroutine that watches state
// files for changes and emits Events. Safe to call once; subsequent
// calls are no-ops.
func (m *Manager) StartPolling() {
	m.mu.Lock()
	if m.closed || m.pollStop != nil {
		m.mu.Unlock()
		return
	}
	m.pollStop = make(chan struct{})
	m.pollDone = make(chan struct{})
	stop := m.pollStop
	done := m.pollDone
	interval := m.pollInterval
	m.mu.Unlock()

	go m.pollLoop(stop, done, interval)
}

// Close stops the poller and closes subscriber channels. Worker
// subprocesses keep running — that is the entire point of the
// orchestrator design.
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	stop := m.pollStop
	done := m.pollDone
	m.pollStop = nil
	for _, ch := range m.subscribers {
		close(ch)
	}
	m.subscribers = nil
	m.mu.Unlock()

	if stop != nil {
		close(stop)
		<-done
	}
	return nil
}

// pollLoop reads all state files at the configured interval, diffs
// against the prior snapshot, and emits Events on change. It also
// reconciles the active map: when a worker has transitioned to a
// terminal status (or its PID has gone away while still active), the
// next queued download is spawned to fill the slot.
func (m *Manager) pollLoop(stop <-chan struct{}, done chan<- struct{}, interval time.Duration) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			m.tick()
		}
	}
}

// tick is one iteration of pollLoop, factored out for direct unit
// testing.
func (m *Manager) tick() {
	recs, err := ListRecords(m.stateDir)
	if err != nil {
		return
	}
	type pending struct {
		id    ID
		state State
	}
	var events []pending
	var freedSlots int

	m.mu.Lock()
	for _, r := range recs {
		prev, had := m.lastSnapshot[r.ID]
		changed := !had ||
			prev.Status != r.Status ||
			prev.Bytes != r.Bytes ||
			prev.Total != r.Total ||
			prev.PID != r.PID ||
			prev.Err != r.Err
		if changed {
			m.lastSnapshot[r.ID] = r
			events = append(events, pending{id: r.ID, state: r.ToState()})
		}
		if pid, active := m.active[r.ID]; active {
			if r.Status.IsTerminal() {
				delete(m.active, r.ID)
				freedSlots++
			} else if pid > 0 && !pidAlive(pid) {
				// Worker died without writing terminal status —
				// mark abandoned ourselves so the UI is honest.
				r.Status = StatusAbandoned
				r.Err = "worker exited unexpectedly"
				_ = SaveRecord(m.stateDir, r)
				m.lastSnapshot[r.ID] = r
				events = append(events, pending{id: r.ID, state: r.ToState()})
				delete(m.active, r.ID)
				freedSlots++
			}
		}
	}
	// Promote queued downloads into freed slots.
	var toSpawn []ID
	for freedSlots > 0 && len(m.queue) > 0 && len(m.active) < m.maxConcurrent {
		next := m.queue[0]
		m.queue = m.queue[1:]
		toSpawn = append(toSpawn, next)
		freedSlots--
	}
	m.mu.Unlock()

	for _, ev := range events {
		m.broadcast(Event{ID: ev.id, State: ev.state})
	}
	for _, id := range toSpawn {
		_ = m.spawn(id)
	}
}

func (m *Manager) broadcast(ev Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	for _, ch := range m.subscribers {
		select {
		case ch <- ev:
		default:
		}
	}
}

// pidAlive checks whether pid refers to a live process. Mirrors the
// implementation in internal/service/processmgr/recover.go; we copy it
// rather than import to avoid an inter-service package cycle.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}
