package profilestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/fsx"
)

// FSStore persists profiles as one JSON file per profile under a directory.
type FSStore struct {
	dir string
}

func (s *FSStore) Dir() string { return s.dir }

// NewFSStore returns a Store rooted at dir. The directory is created if missing.
func NewFSStore(dir string) (*FSStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir profiles dir: %w", err)
	}
	return &FSStore{dir: dir}, nil
}

// path returns the absolute filesystem path for a profile JSON file.
func (s *FSStore) path(id string) string {
	return filepath.Join(s.dir, id+".json")
}

// List returns all valid profiles, ignoring corrupt entries.
func (s *FSStore) List() ([]domain.Profile, error) {
	profiles, _, err := s.ListWithDiagnostics()
	return profiles, err
}

// ListWithDiagnostics retorna profiles válidos + lista de entries corruptas.
// O agregado nunca aborta a varredura inteira por uma entry quebrada;
// erros de I/O do diretório raiz, esses sim, retornam err != nil.
func (s *FSStore) ListWithDiagnostics() ([]domain.Profile, []ListDiagnostic, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read profiles dir: %w", err)
	}
	profiles := make([]domain.Profile, 0, len(entries))
	var diags []ListDiagnostic
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		p, err := s.Get(id)
		if err != nil {
			diags = append(diags, ListDiagnostic{ID: id, Err: err})
			continue
		}
		profiles = append(profiles, p)
	}
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Name < profiles[j].Name
	})
	return profiles, diags, nil
}

// Get reads a single profile by id. Returns ErrNotFound if the file does not exist.
func (s *FSStore) Get(id string) (domain.Profile, error) {
	if id == "" {
		return domain.Profile{}, ErrInvalidID
	}
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return domain.Profile{}, ErrNotFound
		}
		return domain.Profile{}, fmt.Errorf("read profile: %w", err)
	}
	var p domain.Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return domain.Profile{}, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}
	oldVersion := p.SchemaVersion
	MigrateProfile(&p)
	if p.SchemaVersion != oldVersion {
		_ = s.Save(p)
	}
	return p, nil
}

// Save persists a profile to disk as JSON. Performs an atomic write
// (write to temp + rename) to avoid corrupting the file on crash.
// Fills SchemaVersion, CreatedAt and UpdatedAt if empty.
func (s *FSStore) Save(p domain.Profile) error {
	if p.ID == "" {
		return ErrInvalidID
	}
	if p.SchemaVersion == 0 {
		p.SchemaVersion = domain.SchemaVersion
	}
	now := time.Now().UTC()
	if p.Meta.CreatedAt.IsZero() {
		p.Meta.CreatedAt = now
	}
	p.Meta.UpdatedAt = now

	if _, err := os.Stat(s.path(p.ID)); err == nil {
		var current domain.Profile
		if data, err := os.ReadFile(s.path(p.ID)); err == nil {
			_ = json.Unmarshal(data, &current)
			if current.ID != "" {
				_ = SavePrevious(s.dir, current)
			}
		}
	}

	if err := fsx.WriteJSONAtomic(s.path(p.ID), p); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	return nil
}

// MarkLastUsed updates Meta.LastUsedAt for the given profile id and persists.
// No-op if the profile does not exist.
func (s *FSStore) MarkLastUsed(id string, at time.Time) error {
	p, err := s.Get(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	p.Meta.LastUsedAt = &at
	return s.Save(p)
}

// Delete removes a profile JSON file from disk.
func (s *FSStore) Delete(id string) error {
	if id == "" {
		return ErrInvalidID
	}
	if err := os.Remove(s.path(id)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("remove profile: %w", err)
	}
	_ = DeletePrevious(s.dir, id)
	return nil
}

// Duplicate clones an existing profile under a new id. Resets timestamps
// and appends " (copy)" to the name. Returns ErrDuplicateID if newID already exists.
func (s *FSStore) Duplicate(srcID, newID string) (domain.Profile, error) {
	if newID == "" {
		return domain.Profile{}, ErrInvalidID
	}
	if _, err := os.Stat(s.path(newID)); err == nil {
		return domain.Profile{}, ErrDuplicateID
	}
	src, err := s.Get(srcID)
	if err != nil {
		return domain.Profile{}, err
	}

	dup := src
	dup.ID = newID
	dup.Name = src.Name + " (copy)"
	dup.Meta = domain.ProfileMeta{} // reset timestamps; Save fills them

	if dup.Args != nil {
		if port, ok := portAsInt(dup.Args["port"]); ok {
			used := s.usedPorts()
			dup.Args = cloneArgs(dup.Args)
			dup.Args["port"] = float64(nextFreePort(used, port))
		}
	}

	if err := s.Save(dup); err != nil {
		return domain.Profile{}, err
	}
	return dup, nil
}

// usedPorts collects every "port" arg currently persisted in the store.
// I/O errors degrade gracefully — caller treats the returned set as a
// best-effort hint, not a guarantee.
func (s *FSStore) usedPorts() map[int]struct{} {
	used := make(map[int]struct{})
	profiles, _, err := s.ListWithDiagnostics()
	if err != nil {
		return used
	}
	for _, p := range profiles {
		if port, ok := portAsInt(p.Args["port"]); ok {
			used[port] = struct{}{}
		}
	}
	return used
}

// nextFreePort returns the smallest port > start not present in used.
// Falls back to start when the entire upper range is exhausted.
func nextFreePort(used map[int]struct{}, start int) int {
	for p := start + 1; p < 65536; p++ {
		if _, taken := used[p]; !taken {
			return p
		}
	}
	return start
}

func portAsInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	}
	return 0, false
}

func cloneArgs(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
