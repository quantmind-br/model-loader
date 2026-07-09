// Package benchmarkstore persists benchmark Run results as one JSON file per
// run under <state-dir>/benchmark/runs, mirroring the atomic-write pattern used
// by processmgr's instances.json.
package benchmarkstore

import (
	"errors"
	"fmt"
	"io/fs"
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
	// Load reads a single run by id, returning ErrNotFound when no file exists.
	Load(id string) (benchmark.Run, error)
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

// validRunID reports whether id is a safe single filename component. A run id
// maps to exactly one file inside the runs dir, so anything that could escape
// it — empty, a path separator, or a ".." segment — is rejected before it can
// reach filepath.Join and traverse out of the directory. (BR10)
func validRunID(id string) bool {
	if id == "" || strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) {
		return false
	}
	return filepath.Base(id) == id
}

func (s *fsStore) Save(run benchmark.Run) error {
	if !validRunID(run.ID) {
		return fmt.Errorf("benchmarkstore: invalid run id %q", run.ID)
	}
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
	if !validRunID(id) {
		return nil, ErrNotFound
	}
	tr, err := fsx.ReadJSON[[]benchmark.ProblemTranscript](s.TranscriptPath(id))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return tr, nil
}

// Load reads a single run by id, mapping a missing file to ErrNotFound.
func (s *fsStore) Load(id string) (benchmark.Run, error) {
	if !validRunID(id) {
		return benchmark.Run{}, ErrNotFound
	}
	run, err := fsx.ReadJSON[benchmark.Run](s.path(id))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return benchmark.Run{}, ErrNotFound
		}
		return benchmark.Run{}, err
	}
	return run, nil
}

// isRunFile reports whether name is a run JSON file, excluding the sidecar
// `.transcript.json` files that live in the same directory.
func isRunFile(name string) bool {
	return strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".transcript.json")
}

// List returns all runs sorted newest-first. Corrupt or unreadable files are
// skipped so one bad file never hides the rest.
func (s *fsStore) List() ([]benchmark.Run, error) {
	runs, err := fsx.ListJSONFiles[benchmark.Run](s.dir, isRunFile)
	if err != nil {
		return nil, err
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
	if !validRunID(id) {
		return ErrNotFound
	}
	_ = os.Remove(s.TranscriptPath(id)) // best-effort sidecar cleanup
	err := os.Remove(s.path(id))
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	return err
}
