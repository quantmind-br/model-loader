package downloadmgr

import (
	"errors"
	"net/http"
)

// Manager orchestrates concurrent file downloads.
type Manager struct {
	httpClient    *http.Client
	maxConcurrent int
}

// NewManager creates a download manager with the given HTTP client and
// concurrency limit.
func NewManager(httpClient *http.Client, maxConcurrent int) *Manager {
	return &Manager{
		httpClient:    httpClient,
		maxConcurrent: maxConcurrent,
	}
}

// Start enqueues a new download described by spec.
func (m *Manager) Start(spec Spec) (ID, error) {
	return "", errors.New("not implemented")
}

// Cancel aborts the download identified by id.
func (m *Manager) Cancel(id ID) error {
	return errors.New("not implemented")
}

// Snapshot returns the current state of all known downloads.
func (m *Manager) Snapshot() []State {
	return nil
}

// Subscribe returns a channel that receives state-change events.
func (m *Manager) Subscribe() <-chan Event {
	return nil
}

// Close shuts down the manager and waits for in-progress downloads.
func (m *Manager) Close() error {
	return errors.New("not implemented")
}
