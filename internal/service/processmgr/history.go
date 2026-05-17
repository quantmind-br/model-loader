package processmgr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/fsx"
)

// historyFile is the on-disk shape of instances-history.json.
type historyFile struct {
	Instances []domain.ExitedInstance `json:"instances"`
}

// defaultHistoryLimit is the cap on persisted exit-history entries.
const defaultHistoryLimit = 50

// loadHistory reads path and returns its instances. A missing file yields
// (nil, nil). A malformed file yields (nil, error).
func loadHistory(path string) ([]domain.ExitedInstance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read history: %w", err)
	}
	var hf historyFile
	if err := json.Unmarshal(data, &hf); err != nil {
		return nil, fmt.Errorf("parse history: %w", err)
	}
	return hf.Instances, nil
}

// saveHistory writes the slice atomically to path. Parent dir is created
// if absent.
func saveHistory(path string, entries []domain.ExitedInstance) error {
	if err := fsx.WriteJSONAtomic(path, historyFile{Instances: entries}); err != nil {
		return fmt.Errorf("save history: %w", err)
	}
	return nil
}

// capHistory returns the latest limit entries sorted by ExitedAt descending.
func capHistory(entries []domain.ExitedInstance, limit int) []domain.ExitedInstance {
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	if len(entries) <= limit {
		return entries
	}
	// Sort by ExitedAt descending (newest first).
	sorted := make([]domain.ExitedInstance, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ExitedAt.After(sorted[j].ExitedAt)
	})
	return sorted[:limit]
}

// appendHistoryLocked mutates in-memory history while m.mu is held.
// It returns false when the PID was already recorded (dedup).
func (m *fsManager) appendHistoryLocked(inst domain.RunningInstance, reason string, exitedAt time.Time) bool {
	if _, ok := m.historyRecorded[inst.PID]; ok {
		return false
	}

	dur := int64(exitedAt.Sub(inst.StartedAt).Round(time.Second).Seconds())
	if dur < 0 {
		dur = 0
	}

	entry := domain.ExitedInstance{
		ProfileID:       inst.ProfileID,
		PID:             inst.PID,
		Port:            inst.Port,
		LogPath:         inst.LogPath,
		BinaryPath:      inst.BinaryPath,
		StartedAt:       inst.StartedAt,
		ExitedAt:        exitedAt,
		DurationSeconds: dur,
		Background:      inst.Background,
		Crashed:         inst.Crashed,
		ExitCode:        inst.ExitCode,
		ExitSignal:      inst.ExitSignal,
		ExitReason:      reason,
		StderrTail:      inst.StderrTail,
	}

	m.history = append(m.history, entry)
	m.historyRecorded[inst.PID] = struct{}{}
	m.history = capHistory(m.history, m.historyLimit)
	return true
}

// persistHistory copies the latest in-memory history under m.mu, then writes
// it to disk outside the lock.
func (m *fsManager) persistHistory() error {
	m.historySaveMu.Lock()
	defer m.historySaveMu.Unlock()

	m.mu.Lock()
	snap := make([]domain.ExitedInstance, len(m.history))
	copy(snap, m.history)
	m.mu.Unlock()

	return saveHistory(m.historyPath, snap)
}

// History returns a defensive snapshot of the exit history.
func (m *fsManager) History() []domain.ExitedInstance {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.ExitedInstance, len(m.history))
	copy(out, m.history)
	return out
}
