package downloadmgr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/internal/fsx"
)

// DownloadRecord is the on-disk shape of a single download's state. One
// file per download lives at <stateDir>/<id>.json so each worker
// subprocess can write its own progress without racing peers. The TUI
// reads all records to populate the progress footer.
type DownloadRecord struct {
	ID         ID        `json:"id"`
	PID        int       `json:"pid"`
	RepoID     string    `json:"repo_id,omitempty"`
	Filename   string    `json:"filename,omitempty"`
	URL        string    `json:"url"`
	DestDir    string    `json:"dest_dir,omitempty"`
	DestFile   string    `json:"dest_file"`
	IsSnapshot bool      `json:"is_snapshot,omitempty"`
	Status     Status    `json:"status"`
	Bytes      int64     `json:"bytes"`
	Total      int64     `json:"total"`
	StartedAt  time.Time `json:"started_at,omitzero"`
	UpdatedAt  time.Time `json:"updated_at,omitzero"`
	Err        string    `json:"err,omitempty"`
}

// ToState materializes the public State view used by Snapshot/Event so
// callers don't need to know the on-disk layout.
func (r DownloadRecord) ToState() State {
	st := State{
		ID:        r.ID,
		Status:    r.Status,
		Bytes:     r.Bytes,
		Total:     r.Total,
		StartedAt: r.StartedAt,
		Spec: Spec{
			RepoID:     r.RepoID,
			Filename:   r.Filename,
			URL:        r.URL,
			DestDir:    r.DestDir,
			DestFile:   r.DestFile,
			IsSnapshot: r.IsSnapshot,
		},
	}
	if r.Err != "" {
		st.Err = errors.New(r.Err)
	}
	return st
}

// RecordFromSpec seeds a fresh record from a Spec at queue time.
func RecordFromSpec(id ID, spec Spec) DownloadRecord {
	now := time.Now().UTC()
	return DownloadRecord{
		ID:         id,
		RepoID:     spec.RepoID,
		Filename:   spec.Filename,
		URL:        spec.URL,
		DestDir:    spec.DestDir,
		DestFile:   spec.DestFile,
		IsSnapshot: spec.IsSnapshot,
		Status:     StatusQueued,
		StartedAt:  now,
		UpdatedAt:  now,
	}
}

// stateFilename returns the filename portion of the per-download JSON.
// The leading "dl-" prefix lets us list-and-filter the directory without
// matching unrelated dotfiles. ID is already filesystem-safe (see NewID).
func stateFilename(id ID) string {
	return "dl-" + string(id) + ".json"
}

// StatePath returns the full path of the per-download JSON inside stateDir.
func StatePath(stateDir string, id ID) string {
	return filepath.Join(stateDir, stateFilename(id))
}

// SaveRecord writes r atomically to <stateDir>/<id>.json, refreshing
// UpdatedAt to now.
func SaveRecord(stateDir string, r DownloadRecord) error {
	r.UpdatedAt = time.Now().UTC()
	return fsx.WriteJSONAtomic(StatePath(stateDir, r.ID), r)
}

// LoadRecord reads a per-download JSON file. A missing file yields
// (zero, fs.ErrNotExist) so callers can distinguish "never queued" from
// "queued, but read failed".
func LoadRecord(path string) (DownloadRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DownloadRecord{}, err
	}
	var r DownloadRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return DownloadRecord{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return r, nil
}

// ListRecords returns every download record stored under stateDir, in
// undefined order. A missing directory yields (nil, nil) — first run.
// Malformed individual files are skipped with a best-effort policy
// (their content is preserved on disk for inspection).
func ListRecords(stateDir string) ([]DownloadRecord, error) {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read state dir: %w", err)
	}
	out := make([]DownloadRecord, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "dl-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		r, err := LoadRecord(filepath.Join(stateDir, name))
		if err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// DeleteRecord removes the per-download JSON. Missing file is not an error.
func DeleteRecord(stateDir string, id ID) error {
	err := os.Remove(StatePath(stateDir, id))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
