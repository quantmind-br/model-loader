package downloadmgr

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// ID uniquely identifies a download.
type ID string

// NewID generates a new download ID. Format: <unix-nano>-<8hex> so it is
// sortable by time, filesystem-safe (no colons/T like RFC3339Nano), and
// collision-resistant when multiple downloads start in the same nanosecond.
func NewID() ID {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return ID(fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(b[:])))
}

// Spec describes what to download and where to put it.
type Spec struct {
	RepoID     string
	Filename   string
	URL        string
	DestDir    string
	DestFile   string
	IsSnapshot bool
	Logger     *slog.Logger
}

// Status represents the lifecycle state of a download.
type Status int

const (
	StatusQueued Status = iota
	StatusActive
	StatusCompleted
	StatusFailed
	StatusCancelled
	// StatusAbandoned marks a download whose worker subprocess died
	// without writing a terminal status. Detected by Reconcile when
	// the recorded PID is no longer alive. Resumable via Manager.Resume.
	StatusAbandoned
)

// String returns the JSON / state-file representation of Status. Using
// strings (not ints) for on-disk persistence keeps the format forward-
// compatible if new statuses are inserted.
func (s Status) String() string {
	switch s {
	case StatusQueued:
		return "queued"
	case StatusActive:
		return "active"
	case StatusCompleted:
		return "completed"
	case StatusFailed:
		return "failed"
	case StatusCancelled:
		return "cancelled"
	case StatusAbandoned:
		return "abandoned"
	default:
		return "unknown"
	}
}

// ParseStatus is the inverse of String. Unknown strings yield StatusFailed
// to keep recovery code on the safe side.
func ParseStatus(s string) Status {
	switch s {
	case "queued":
		return StatusQueued
	case "active":
		return StatusActive
	case "completed":
		return StatusCompleted
	case "failed":
		return StatusFailed
	case "cancelled":
		return StatusCancelled
	case "abandoned":
		return StatusAbandoned
	default:
		return StatusFailed
	}
}

// IsTerminal reports whether the status will not transition further on
// its own (completed / failed / cancelled / abandoned).
func (s Status) IsTerminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusCancelled, StatusAbandoned:
		return true
	default:
		return false
	}
}

func (s Status) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

func (s *Status) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*s = ParseStatus(raw)
	return nil
}

// State is a point-in-time snapshot of a download.
type State struct {
	ID        ID
	Spec      Spec
	Status    Status
	Bytes     int64
	Total     int64
	Err       error
	StartedAt time.Time
}

// Event is emitted whenever a download's state changes.
type Event struct {
	ID    ID
	State State
}
