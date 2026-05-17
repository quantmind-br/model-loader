// Package proxysupervisor manages the HTTP proxy server as a detached OS
// process, with persistent state so the TUI can discover a running proxy
// across restarts.
package proxysupervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/internal/fsx"
)

// stateFile is the on-disk shape of proxy-state.json. Wrapped in an object
// so we can add fields later without breaking parse compatibility.
type stateFile struct {
	State *State `json:"state,omitempty"`
}

// State captures everything needed to reconnect to a running proxy process.
type State struct {
	PID       int       `json:"pid"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"started_at"`
}

// loadState reads path and returns the stored state. A missing file yields
// (nil, nil). A malformed file yields (nil, error).
func loadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read proxy state: %w", err)
	}
	var sf stateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("parse proxy state: %w", err)
	}
	return sf.State, nil
}

// saveState persists state atomically to path. A nil state deletes the file.
func saveState(path string, s *State) error {
	if s == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove proxy state: %w", err)
		}
		return nil
	}
	if err := fsx.WriteJSONAtomic(path, stateFile{State: s}); err != nil {
		return fmt.Errorf("save proxy state: %w", err)
	}
	return nil
}
