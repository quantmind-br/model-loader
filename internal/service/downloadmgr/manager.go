package downloadmgr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrManagerClosed = errors.New("download manager closed")

// Manager orchestrates concurrent file downloads.
type Manager struct {
	httpClient    *http.Client
	maxConcurrent int
	userAgent     string
	mu            sync.Mutex
	states        map[ID]*State
	queue         []ID
	active        map[ID]context.CancelFunc
	subscribers   []chan Event
	closed        bool
}

// NewManager creates a download manager with the given HTTP client and
// concurrency limit.
func NewManager(httpClient *http.Client, maxConcurrent int) *Manager {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}

	return &Manager{
		httpClient:    httpClient,
		maxConcurrent: maxConcurrent,
		states:        make(map[ID]*State),
		active:        make(map[ID]context.CancelFunc),
	}
}

// WithUserAgent sets the User-Agent header for all download requests.
func (m *Manager) WithUserAgent(ua string) *Manager {
	m.userAgent = ua
	return m
}

// Start enqueues a new download described by spec.
func (m *Manager) Start(spec Spec) (ID, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", ErrManagerClosed
	}

	id := NewID()
	state := &State{ID: id, Spec: spec, Status: StatusQueued}
	m.states[id] = state
	if len(m.active) < m.maxConcurrent {
		m.startLocked(id, state)
	} else {
		m.queue = append(m.queue, id)
	}
	event := Event{ID: id, State: *state}
	m.mu.Unlock()

	m.broadcast(event)
	return id, nil
}

// Cancel aborts the download identified by id.
func (m *Manager) Cancel(id ID) error {
	m.mu.Lock()
	state, ok := m.states[id]
	if !ok {
		m.mu.Unlock()
		return nil
	}

	if cancel, ok := m.active[id]; ok {
		delete(m.active, id)
		state.Status = StatusCancelled
		event := Event{ID: id, State: *state}
		cancel()
		m.startNextLocked()
		m.mu.Unlock()
		m.broadcast(event)
		return nil
	}

	for i, queuedID := range m.queue {
		if queuedID == id {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			break
		}
	}
	if state.Status == StatusQueued || state.Status == StatusActive {
		state.Status = StatusCancelled
	}
	event := Event{ID: id, State: *state}
	m.mu.Unlock()

	m.broadcast(event)
	return nil
}

// Snapshot returns the current state of all known downloads.
func (m *Manager) Snapshot() []State {
	m.mu.Lock()
	defer m.mu.Unlock()

	snapshot := make([]State, 0, len(m.states))
	for _, state := range m.states {
		snapshot = append(snapshot, *state)
	}
	return snapshot
}

// Subscribe returns a channel that receives state-change events.
func (m *Manager) Subscribe() <-chan Event {
	ch := make(chan Event, 16)
	m.mu.Lock()
	if m.closed {
		close(ch)
	} else {
		m.subscribers = append(m.subscribers, ch)
	}
	m.mu.Unlock()
	return ch
}

// Close shuts down the manager and waits for in-progress downloads.
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true

	for id, cancel := range m.active {
		if state, ok := m.states[id]; ok {
			state.Status = StatusCancelled
		}
		cancel()
	}
	m.active = make(map[ID]context.CancelFunc)
	for _, id := range m.queue {
		if state, ok := m.states[id]; ok {
			state.Status = StatusCancelled
		}
	}
	m.queue = nil
	for _, ch := range m.subscribers {
		close(ch)
	}
	m.subscribers = nil
	m.mu.Unlock()
	return nil
}

func (m *Manager) startLocked(id ID, state *State) {
	ctx, cancel := context.WithCancel(context.Background())
	m.active[id] = cancel
	state.Status = StatusActive
	state.StartedAt = time.Now()
	go m.runDownload(ctx, state)
}

func (m *Manager) runDownload(ctx context.Context, state *State) {
	m.broadcast(Event{ID: state.ID, State: m.snapshotState(state.ID)})

	if err := m.download(ctx, state); err != nil {
		m.setTerminal(state.ID, statusForError(ctx, err), err)
		m.finish(state.ID)
		return
	}
	m.setTerminal(state.ID, StatusCompleted, nil)
	m.finish(state.ID)
}

func (m *Manager) download(ctx context.Context, state *State) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, state.Spec.URL, nil)
	if err != nil {
		return err
	}
	if m.userAgent != "" {
		req.Header.Set("User-Agent", m.userAgent)
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}
	m.setTotal(state.ID, resp.ContentLength)

	partialPath := state.Spec.DestFile + ".partial"
	if err := os.MkdirAll(filepath.Dir(partialPath), 0o755); err != nil {
		return err
	}
	file, err := os.Create(partialPath)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(file, &progressReader{
		reader: resp.Body,
		onProgress: func(bytes int64) {
			m.setBytes(state.ID, bytes)
		},
	})
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(partialPath)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(partialPath)
		return closeErr
	}
	if err := os.Rename(partialPath, state.Spec.DestFile); err != nil {
		_ = os.Remove(partialPath)
		return err
	}
	return nil
}

func (m *Manager) setBytes(id ID, bytes int64) {
	m.mu.Lock()
	state, ok := m.states[id]
	if ok {
		state.Bytes = bytes
	}
	event := Event{}
	if ok {
		event = Event{ID: id, State: *state}
	}
	m.mu.Unlock()
	if ok {
		m.broadcast(event)
	}
}

func (m *Manager) setTotal(id ID, total int64) {
	m.mu.Lock()
	if state, ok := m.states[id]; ok {
		state.Total = total
	}
	m.mu.Unlock()
}

func (m *Manager) setTerminal(id ID, status Status, err error) {
	m.mu.Lock()
	state, ok := m.states[id]
	if ok {
		if state.Status == StatusCancelled {
			status = StatusCancelled
		}
		state.Status = status
		state.Err = err
	}
	event := Event{}
	if ok {
		event = Event{ID: id, State: *state}
	}
	m.mu.Unlock()
	if ok {
		m.broadcast(event)
	}
}

func (m *Manager) finish(id ID) {
	m.mu.Lock()
	_, wasActive := m.active[id]
	if wasActive {
		delete(m.active, id)
	}
	if wasActive {
		m.startNextLocked()
	}
	m.mu.Unlock()
}

func (m *Manager) startNextLocked() {
	for !m.closed && len(m.active) < m.maxConcurrent && len(m.queue) > 0 {
		id := m.queue[0]
		m.queue = m.queue[1:]
		state, ok := m.states[id]
		if !ok || state.Status != StatusQueued {
			continue
		}
		m.startLocked(id, state)
		event := Event{ID: id, State: *state}
		go m.broadcast(event)
	}
}

func (m *Manager) snapshotState(id ID) State {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state, ok := m.states[id]; ok {
		return *state
	}
	return State{ID: id, Status: StatusFailed, Err: errors.New("download state missing")}
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

func statusForError(ctx context.Context, err error) Status {
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return StatusCancelled
	}
	return StatusFailed
}

type progressReader struct {
	reader     io.Reader
	bytes      int64
	onProgress func(int64)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.bytes += int64(n)
		r.onProgress(r.bytes)
	}
	return n, err
}
