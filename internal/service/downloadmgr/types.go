package downloadmgr

import (
	"log/slog"
	"time"
)

// ID uniquely identifies a download.
type ID string

// NewID generates a new download ID from the current timestamp.
func NewID() ID {
	return ID(time.Now().Format(time.RFC3339Nano))
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
)

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
