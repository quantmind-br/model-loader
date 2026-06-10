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
	// The filename is the authoritative storage key: List enumerates by it,
	// path() rebuilds it, and Save/Delete address files by p.ID. A profile
	// whose JSON "id" field diverges from its filename (a manual copy or an
	// older import) would otherwise be reported under the content id and could
	// never be deleted/edited — Delete(contentID) targets dir/contentID.json,
	// a different file. Force the reported id to match the filename so every
	// store operation round-trips to the same file.
	idChanged := p.ID != id
	p.ID = id
	oldVersion := p.SchemaVersion
	MigrateProfile(&p)
	stripped := stripReservedArgs(&p)
	// Persist back when the id was healed, a migration bumped the schema
	// version, or a reserved arg was stripped, so the divergence/old
	// version/stale arg doesn't resurface.
	if idChanged || p.SchemaVersion != oldVersion || stripped {
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
	stripReservedArgs(&p)
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
			if err := json.Unmarshal(data, &current); err == nil {
				// Key the undo snapshot by the storage filename, not the
				// possibly-stale embedded id (a manual copy / pre-heal file),
				// so LoadPrevious(dir, p.ID) finds it and restore writes back
				// to the same file.
				current.ID = p.ID
				_ = SavePrevious(s.dir, current)
			}
		}
	}

	if err := fsx.WriteJSONAtomic(s.path(p.ID), p); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	return nil
}

// Create persists a brand-new profile, failing with ErrDuplicateID if a profile
// already occupies p.ID. Unlike Save (an upsert), the create is exclusive: the
// file is hard-linked into place, so a concurrent writer (another TUI, an import,
// a manual file) that already claimed the id cannot be silently overwritten —
// it loses no args/env/meta. Fills SchemaVersion, CreatedAt and UpdatedAt.
func (s *FSStore) Create(p domain.Profile) error {
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

	if err := fsx.WriteJSONExclusive(s.path(p.ID), p); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ErrDuplicateID
		}
		return fmt.Errorf("create profile: %w", err)
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
	dup.Pinned = false              // the copy starts unpinned (UX-01)
	dup.Meta = domain.ProfileMeta{} // reset timestamps; Save fills them

	if err := s.Save(dup); err != nil {
		return domain.Profile{}, err
	}
	return dup, nil
}

// Rename persists the final profile p under its new id (p.ID) and removes the
// old file at oldID. When oldID == p.ID it degrades to a plain Save.
//
// Failure semantics (Codex adversarial review):
//   - The target is written via the exclusive Create, not Save: if p.ID was
//     claimed concurrently (after the editor's stale validator snapshot) it
//     fails atomically with ErrDuplicateID instead of clobbering that profile.
//     There is no check-then-write TOCTOU gap a Stat+Save pair would leave open.
//   - Source existence is proven by the removal itself, not a prior Stat (which
//     would be its own TOCTOU window). If os.Remove finds nothing — the source
//     never existed, or was deleted concurrently after the target was written —
//     the new file is rolled back and ErrNotFound is returned, so a profile
//     deleted mid-rename is never resurrected under the new id.
//   - The new file is written BEFORE the old is removed, so a crash between the
//     two steps leaves the original profile intact (no lost edits).
//   - On any removal failure the freshly written new file is rolled back, so a
//     retry starts from a clean single-file state instead of tripping over its
//     own leftover target (ErrDuplicateID).
//
// The old profile's undo history is dropped (the renamed file starts fresh).
func (s *FSStore) Rename(oldID string, p domain.Profile) error {
	if oldID == "" || p.ID == "" {
		return ErrInvalidID
	}
	if oldID == p.ID {
		return s.Save(p)
	}

	// Exclusive write closes the concurrent-target race: a claimed target yields
	// ErrDuplicateID here, with nothing written and the source left intact.
	if err := s.Create(p); err != nil {
		return err
	}
	// The removal is the authoritative source-existence check. Removing nothing
	// means the source is gone (never existed or deleted concurrently): roll the
	// new file back and report ErrNotFound rather than resurrecting it.
	if err := os.Remove(s.path(oldID)); err != nil {
		_ = os.Remove(s.path(p.ID))
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("remove old profile: %w", err)
	}
	_ = DeletePrevious(s.dir, oldID)
	return nil
}

// reservedArgs are launch parameters owned by the process manager. They are
// removed on read and write so stored profiles can never carry them; the
// manager assigns them at launch time.
var reservedArgs = []string{"port"}

// stripReservedArgs removes manager-owned keys from p.Args, cloning the map
// first so the caller's copy is untouched. Reports whether anything changed.
func stripReservedArgs(p *domain.Profile) bool {
	hit := false
	for _, k := range reservedArgs {
		if _, ok := p.Args[k]; ok {
			hit = true
			break
		}
	}
	if !hit {
		return false
	}
	args := make(map[string]any, len(p.Args))
	for k, v := range p.Args {
		args[k] = v
	}
	for _, k := range reservedArgs {
		delete(args, k)
	}
	p.Args = args
	return true
}
