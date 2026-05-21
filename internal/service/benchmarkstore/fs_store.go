// Package benchmarkstore persists benchmark Run results as one JSON file per
// run under <state-dir>/benchmark/runs, mirroring the atomic-write pattern used
// by processmgr's instances.json.
package benchmarkstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/service/internal/fsx"
)

// ErrNotFound is returned when a run id has no file.
var ErrNotFound = errors.New("benchmark run not found")

// Store persists and queries benchmark runs.
type Store interface {
	Save(run benchmark.Run) error
	List() ([]benchmark.Run, error)
	ListByProfile(profileID string) ([]benchmark.Run, error)
	Delete(id string) error
	// LoadTranscript returns the raw per-problem I/O captured for a run, or
	// ErrNotFound when transcripts were not saved.
	LoadTranscript(id string) ([]benchmark.ProblemTranscript, error)
	// TranscriptPath returns where a run's transcript file lives (may not exist).
	TranscriptPath(id string) string
}

type fsStore struct {
	dir string
}

// New returns a filesystem-backed Store rooted at dir. The directory is created
// lazily on first Save.
func New(dir string) Store {
	return &fsStore{dir: dir}
}

func (s *fsStore) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

// TranscriptPath returns where a run's transcript file lives.
func (s *fsStore) TranscriptPath(id string) string {
	return filepath.Join(s.dir, id+".transcript.json")
}

func (s *fsStore) Save(run benchmark.Run) error {
	if err := fsx.WriteJSONAtomic(s.path(run.ID), run); err != nil {
		return err
	}
	if len(run.Transcript) > 0 {
		return fsx.WriteJSONAtomic(s.TranscriptPath(run.ID), run.Transcript)
	}
	return nil
}

// LoadTranscript reads the raw I/O captured for a run.
func (s *fsStore) LoadTranscript(id string) ([]benchmark.ProblemTranscript, error) {
	b, err := os.ReadFile(s.TranscriptPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var tr []benchmark.ProblemTranscript
	if err := json.Unmarshal(b, &tr); err != nil {
		return nil, err
	}
	return tr, nil
}

// List returns all runs sorted newest-first. Corrupt or unreadable files are
// skipped so one bad file never hides the rest.
func (s *fsStore) List() ([]benchmark.Run, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	runs := make([]benchmark.Run, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if strings.HasSuffix(e.Name(), ".transcript.json") {
			continue // sidecar transcript, not a run
		}
		b, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var run benchmark.Run
		if err := json.Unmarshal(b, &run); err != nil {
			continue
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].StartedAt.After(runs[j].StartedAt) })
	return runs, nil
}

func (s *fsStore) ListByProfile(profileID string) ([]benchmark.Run, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, r := range all {
		if r.ProfileID == profileID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *fsStore) Delete(id string) error {
	_ = os.Remove(s.TranscriptPath(id)) // best-effort sidecar cleanup
	err := os.Remove(s.path(id))
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	return err
}
