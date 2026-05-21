package profilestore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func newStore(t *testing.T) (*FSStore, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewFSStore(dir)
	if err != nil {
		t.Fatalf("NewFSStore: %v", err)
	}
	return s, dir
}

func sampleProfile(id, name string) domain.Profile {
	return domain.Profile{
		ID:    id,
		Name:  name,
		Model: "/tmp/model.gguf",
		Args: map[string]any{
			"ngl":      float64(99),
			"ctx-size": float64(8192),
			"port":     float64(8080),
		},
		Launch: domain.LaunchConfig{DefaultBackground: true},
	}
}

func TestFSStore_SaveAndGet(t *testing.T) {
	s, _ := newStore(t)
	p := sampleProfile("qwen", "Qwen")

	if err := s.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Get("qwen")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Qwen" {
		t.Errorf("Name = %q, want %q", got.Name, "Qwen")
	}
	if got.SchemaVersion != domain.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, domain.SchemaVersion)
	}
	if got.Meta.CreatedAt.IsZero() || got.Meta.UpdatedAt.IsZero() {
		t.Errorf("Save did not stamp timestamps: %+v", got.Meta)
	}
}

func TestFSStore_SaveAndGet_PreservesEnv(t *testing.T) {
	s, _ := newStore(t)
	p := sampleProfile("with-env", "WithEnv")
	p.Launch.Env = []domain.EnvVar{
		{Key: "GGML_CUDA_FORCE_CUBLAS_COMPUTE_16F", Value: "1"},
		{Key: "CUDA_VISIBLE_DEVICES", Value: "0,1"},
	}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Get("with-env")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Launch.Env) != 2 {
		t.Fatalf("Env len = %d, want 2", len(got.Launch.Env))
	}
	if got.Launch.Env[0].Key != "GGML_CUDA_FORCE_CUBLAS_COMPUTE_16F" || got.Launch.Env[0].Value != "1" {
		t.Errorf("Env[0] = %+v", got.Launch.Env[0])
	}
	if got.Launch.Env[1].Key != "CUDA_VISIBLE_DEVICES" || got.Launch.Env[1].Value != "0,1" {
		t.Errorf("Env[1] = %+v", got.Launch.Env[1])
	}
}

func TestFSStore_GetNotFound(t *testing.T) {
	s, _ := newStore(t)
	_, err := s.Get("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestFSStore_GetInvalidJSON(t *testing.T) {
	s, dir := newStore(t)
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get("broken")
	if !errors.Is(err, ErrInvalidJSON) {
		t.Errorf("err = %v, want ErrInvalidJSON", err)
	}
}

func TestFSStore_List_SkipsCorrupt(t *testing.T) {
	s, dir := newStore(t)
	if err := s.Save(sampleProfile("a", "Alpha")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{}{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(sampleProfile("b", "Beta")); err != nil {
		t.Fatal(err)
	}

	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List len = %d, want 2 (got names: %v)", len(got), names(got))
	}
	if got[0].Name != "Alpha" || got[1].Name != "Beta" {
		t.Errorf("List unsorted: %v", names(got))
	}
}

func TestFSStore_ListWithDiagnostics_ReportsCorrupt(t *testing.T) {
	s, dir := newStore(t)
	if err := s.Save(sampleProfile("good", "Good")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	profiles, diags, err := s.ListWithDiagnostics()
	if err != nil {
		t.Fatalf("ListWithDiagnostics: %v", err)
	}
	if len(profiles) != 1 || profiles[0].ID != "good" {
		t.Fatalf("profiles = %+v, want exactly one good", profiles)
	}
	if len(diags) != 1 {
		t.Fatalf("diags = %d, want 1", len(diags))
	}
	if diags[0].ID != "bad" {
		t.Errorf("diags[0].ID = %q, want bad", diags[0].ID)
	}
	if !errors.Is(diags[0].Err, ErrInvalidJSON) {
		t.Errorf("diags[0].Err = %v, want ErrInvalidJSON", diags[0].Err)
	}
}

func TestFSStore_Delete(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Save(sampleProfile("x", "X")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("x"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Delete("x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete err = %v, want ErrNotFound", err)
	}
}

// TestFSStore_FilenameIsAuthoritativeForID reproduces the delete-never-works
// bug: when a profile file's name diverges from the "id" embedded in its JSON
// (e.g. a manual copy / old import), the store must report the FILENAME-derived
// id from List/Get so that Delete(reportedID) -> os.Remove(dir/reportedID.json)
// hits the real file. Before the fix, List reported the content id, Delete
// removed the wrong (or non-existent) file, and the profile could never be
// deleted through the UI.
func TestFSStore_FilenameIsAuthoritativeForID(t *testing.T) {
	s, dir := newStore(t)
	// File named "actual-file.json" but content claims id "other-id".
	raw := []byte(`{"id":"other-id","name":"Mismatch","schema_version":3,"model":"/m.gguf"}`)
	if err := os.WriteFile(filepath.Join(dir, "actual-file.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	profiles, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("List len = %d, want 1", len(profiles))
	}
	if profiles[0].ID != "actual-file" {
		t.Fatalf("reported ID = %q, want filename-derived %q", profiles[0].ID, "actual-file")
	}

	// Deleting via the id the store reported must remove the real file.
	if err := s.Delete(profiles[0].ID); err != nil {
		t.Fatalf("Delete(%q): %v", profiles[0].ID, err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "actual-file.json")); !os.IsNotExist(statErr) {
		t.Errorf("file still present after delete: stat err = %v", statErr)
	}
}

// TestFSStore_DuplicateIDsAcrossFilesAreIndependentlyDeletable covers the
// exact production scenario: two files share the same content id but have
// distinct filenames. Each must be addressable and deletable on its own.
func TestFSStore_DuplicateIDsAcrossFilesAreIndependentlyDeletable(t *testing.T) {
	s, dir := newStore(t)
	body := `{"id":"dup","name":"Dup","schema_version":3,"model":"/m.gguf"}`
	if err := os.WriteFile(filepath.Join(dir, "dup.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dup-copy.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	profiles, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("List len = %d, want 2", len(profiles))
	}

	// The two list entries must report DISTINCT ids (their filenames) so the
	// UI can target each independently. Before the fix both reported "dup".
	ids := map[string]bool{profiles[0].ID: true, profiles[1].ID: true}
	if len(ids) != 2 || !ids["dup"] || !ids["dup-copy"] {
		t.Fatalf("reported ids = %v, want distinct {dup, dup-copy}", ids)
	}

	// Deleting by the reported id removes exactly that file, leaving the other.
	if err := s.Delete("dup-copy"); err != nil {
		t.Fatalf("Delete(dup-copy): %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "dup-copy.json")); !os.IsNotExist(statErr) {
		t.Errorf("dup-copy.json still present after delete")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "dup.json")); statErr != nil {
		t.Errorf("dup.json wrongly removed: %v", statErr)
	}
}

func TestFSStore_Duplicate(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Save(sampleProfile("orig", "Original")); err != nil {
		t.Fatal(err)
	}

	dup, err := s.Duplicate("orig", "orig-copy")
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	if dup.ID != "orig-copy" {
		t.Errorf("dup.ID = %q, want orig-copy", dup.ID)
	}
	if dup.Name != "Original (copy)" {
		t.Errorf("dup.Name = %q, want %q", dup.Name, "Original (copy)")
	}

	// Existing target -> ErrDuplicateID
	_, err = s.Duplicate("orig", "orig-copy")
	if !errors.Is(err, ErrDuplicateID) {
		t.Errorf("err = %v, want ErrDuplicateID", err)
	}

	// Missing source -> ErrNotFound
	_, err = s.Duplicate("nope", "anywhere")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// renamed returns the source profile re-keyed to newID with an edited name,
// mimicking what the editor commits: final content under the new id.
func renamed(src domain.Profile, newID, newName string) domain.Profile {
	src.ID = newID
	src.Name = newName
	return src
}

func TestFSStore_Rename(t *testing.T) {
	s, dir := newStore(t)
	if err := s.Save(sampleProfile("orig", "Original")); err != nil {
		t.Fatal(err)
	}
	src := mustGet(t, s, "orig")
	created := src.Meta.CreatedAt

	if err := s.Rename("orig", renamed(src, "renamed", "Edited")); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	// Old file gone.
	if _, err := s.Get("orig"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(orig) err = %v, want ErrNotFound", err)
	}
	// New file present with the FINAL edited content; Meta.CreatedAt preserved.
	got := mustGet(t, s, "renamed")
	if got.ID != "renamed" {
		t.Errorf("got.ID = %q, want renamed", got.ID)
	}
	if got.Name != "Edited" {
		t.Errorf("got.Name = %q, want Edited (final content)", got.Name)
	}
	if !got.Meta.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want preserved %v", got.Meta.CreatedAt, created)
	}
	if _, err := os.Stat(filepath.Join(dir, "orig.json")); !os.IsNotExist(err) {
		t.Errorf("orig.json still exists: %v", err)
	}
}

func TestFSStore_RenameCollision(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Save(sampleProfile("a", "A")); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(sampleProfile("b", "B")); err != nil {
		t.Fatal(err)
	}
	src := mustGet(t, s, "a")
	if err := s.Rename("a", renamed(src, "b", "A")); !errors.Is(err, ErrDuplicateID) {
		t.Errorf("err = %v, want ErrDuplicateID", err)
	}
	// Both profiles untouched after a rejected rename (no data loss).
	if _, err := s.Get("a"); err != nil {
		t.Errorf("source removed after rejected rename: %v", err)
	}
	if got := mustGet(t, s, "b"); got.Name != "B" {
		t.Errorf("collision target overwritten: Name = %q, want B", got.Name)
	}
}

func TestFSStore_RenameDropsOldHistory(t *testing.T) {
	s, dir := newStore(t)
	if err := s.Save(sampleProfile("orig", "Original")); err != nil {
		t.Fatal(err)
	}
	// A second Save creates the .previous.json history snapshot under orig.
	if err := s.Save(sampleProfile("orig", "Original v2")); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := LoadPrevious(dir, "orig"); !ok {
		t.Fatal("precondition: expected history snapshot for orig")
	}

	src := mustGet(t, s, "orig")
	if err := s.Rename("orig", renamed(src, "renamed", "Edited")); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	// Old history removed; the renamed file starts fresh (no stale undo).
	if _, ok, _ := LoadPrevious(dir, "orig"); ok {
		t.Error("old history snapshot should be gone")
	}
	if _, ok, _ := LoadPrevious(dir, "renamed"); ok {
		t.Error("renamed profile should start without history")
	}
}

func TestFSStore_RenameSameIDSaves(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Save(sampleProfile("orig", "Original")); err != nil {
		t.Fatal(err)
	}
	src := mustGet(t, s, "orig")
	if err := s.Rename("orig", renamed(src, "orig", "Edited")); err != nil {
		t.Errorf("Rename same id: %v", err)
	}
	got := mustGet(t, s, "orig")
	if got.Name != "Edited" {
		t.Errorf("same-id rename did not save edit: Name = %q, want Edited", got.Name)
	}
}

func mustGet(t *testing.T, s *FSStore, id string) domain.Profile {
	t.Helper()
	p, err := s.Get(id)
	if err != nil {
		t.Fatalf("Get(%q): %v", id, err)
	}
	return p
}

// TestFSStore_HealKeepsHistoryUnderFilename guards the read-repair path: a
// file whose embedded id diverges from its filename (a manual copy) must not,
// when healed, write its undo snapshot under the stale embedded id and clobber
// the unrelated profile that legitimately owns that id.
func TestFSStore_HealKeepsHistoryUnderFilename(t *testing.T) {
	s, dir := newStore(t)

	// Real profile "bar" with an undo snapshot of its own content.
	if err := s.Save(sampleProfile("bar", "Bar v1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(sampleProfile("bar", "Bar v2")); err != nil {
		t.Fatal(err)
	}

	// A divergent file foo.json whose embedded id is the stale "bar".
	stray := sampleProfile("bar", "Stray content")
	data, err := json.Marshal(stray)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "foo.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Get("foo") heals the id and re-saves; must not touch bar's history.
	if _, err := s.Get("foo"); err != nil {
		t.Fatalf("Get(foo): %v", err)
	}

	barPrev, ok, _ := LoadPrevious(dir, "bar")
	if !ok {
		t.Fatal("bar history snapshot disappeared")
	}
	if barPrev.Name != "Bar v1" {
		t.Errorf("bar history clobbered: Name = %q, want Bar v1", barPrev.Name)
	}
	// foo's own snapshot (if any) is keyed by its filename, not the stale id.
	if fooPrev, ok, _ := LoadPrevious(dir, "foo"); ok && fooPrev.ID != "foo" {
		t.Errorf("foo snapshot id = %q, want foo", fooPrev.ID)
	}
}

func TestFSStore_Create(t *testing.T) {
	s, dir := newStore(t)
	if err := s.Create(sampleProfile("fresh", "Fresh")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "fresh.json")); err != nil {
		t.Errorf("fresh.json not written: %v", err)
	}
	got := mustGet(t, s, "fresh")
	if got.Meta.CreatedAt.IsZero() {
		t.Error("Create did not stamp CreatedAt")
	}
}

// TestFSStore_CreateRejectsExisting is the Codex finding-1 guard: a new-profile
// commit must never silently overwrite a profile that claimed the id after the
// editor's stale validator snapshot was taken. Create is exclusive.
func TestFSStore_CreateRejectsExisting(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Save(sampleProfile("dup", "Original")); err != nil {
		t.Fatal(err)
	}
	clobber := sampleProfile("dup", "Clobber")
	if err := s.Create(clobber); !errors.Is(err, ErrDuplicateID) {
		t.Errorf("Create over existing id: err = %v, want ErrDuplicateID", err)
	}
	// The original profile's content must survive untouched.
	if got := mustGet(t, s, "dup"); got.Name != "Original" {
		t.Errorf("existing profile clobbered: Name = %q, want Original", got.Name)
	}
}

// TestFSStore_RenameMissingSource is the Codex finding-2 guard (resurrection /
// source-deletion race): renaming a profile whose source is absent — never
// existed, or deleted out from under an open editor mid-rename — must fail with
// ErrNotFound and roll back the target, never recreating it under the new id.
// This drives the post-Create removal path: Create writes the target, the
// source removal returns ErrNotExist, and the target is rolled back.
func TestFSStore_RenameMissingSource(t *testing.T) {
	s, dir := newStore(t)
	ghost := sampleProfile("ghost", "Ghost")
	if err := s.Rename("ghost", renamed(ghost, "revived", "Revived")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Rename of missing source: err = %v, want ErrNotFound", err)
	}
	if _, err := s.Get("revived"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(revived) err = %v, want ErrNotFound (must not resurrect)", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "revived.json")); !os.IsNotExist(err) {
		t.Errorf("revived.json should not exist: %v", err)
	}
}

// TestFSStore_RenameRollsBackOnRemoveFailure is the Codex finding-2 guard
// (split-state): if removing the old file fails after the new file is written,
// the new file is rolled back so the store is not left holding two copies and
// a retry does not trip over its own leftover target (ErrDuplicateID).
func TestFSStore_RenameRollsBackOnRemoveFailure(t *testing.T) {
	s, dir := newStore(t)
	// Make the "old" path a non-empty directory: it exists (passes the source
	// check) but os.Remove fails with ENOTEMPTY, forcing the rollback branch.
	oldPath := filepath.Join(dir, "orig.json")
	if err := os.Mkdir(oldPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldPath, "blocker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := s.Rename("orig", sampleProfile("renamed", "Edited"))
	if err == nil {
		t.Fatal("Rename should fail when the old file cannot be removed")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "renamed.json")); !os.IsNotExist(statErr) {
		t.Errorf("new file not rolled back after remove failure: %v", statErr)
	}
}

func TestFSStore_DuplicateClearsPin(t *testing.T) {
	s, _ := newStore(t)
	src := sampleProfile("orig", "Original")
	src.Pinned = true
	if err := s.Save(src); err != nil {
		t.Fatal(err)
	}
	dup, err := s.Duplicate("orig", "orig-copy")
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	if dup.Pinned {
		t.Error("duplicate of a pinned profile must start unpinned (UX-01)")
	}
}

func TestFSStore_DuplicateBumpsPortWhenInUse(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Save(sampleProfile("a", "A")); err != nil {
		t.Fatal(err)
	}
	dup, err := s.Duplicate("a", "a-copy")
	if err != nil {
		t.Fatalf("Duplicate: %v", err)
	}
	got, ok := dup.Args["port"].(float64)
	if !ok {
		t.Fatalf("dup.Args[port] type = %T, want float64", dup.Args["port"])
	}
	if int(got) != 8081 {
		t.Errorf("dup port = %d, want 8081 (bumped from 8080)", int(got))
	}
	// Origin profile must remain unchanged.
	orig, _ := s.Get("a")
	if origPort, _ := orig.Args["port"].(float64); int(origPort) != 8080 {
		t.Errorf("orig port mutated to %d", int(origPort))
	}
}

func TestNextFreePort_FallsBackWhenExhausted(t *testing.T) {
	used := make(map[int]struct{})
	for p := 65000; p < 65536; p++ {
		used[p] = struct{}{}
	}
	if got := nextFreePort(used, 65500); got != 65500 {
		t.Errorf("nextFreePort exhausted = %d, want fallback 65500", got)
	}
}

func TestFSStore_AtomicWrite_NoLeftoverTmp(t *testing.T) {
	s, dir := newStore(t)
	if err := s.Save(sampleProfile("a", "A")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("found leftover tmp file: %s", e.Name())
		}
	}
}

func TestFSStore_MarkLastUsed(t *testing.T) {
	dir := t.TempDir()
	s, err := NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(sampleProfile("u", "U")); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	if err := s.MarkLastUsed("u", now); err != nil {
		t.Fatalf("MarkLastUsed: %v", err)
	}
	got, _ := s.Get("u")
	if got.Meta.LastUsedAt == nil || !got.Meta.LastUsedAt.Equal(now) {
		t.Errorf("LastUsedAt = %v, want %v", got.Meta.LastUsedAt, now)
	}
}

func TestFSStore_MarkLastUsed_NotFoundIsNoop(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewFSStore(dir)
	if err := s.MarkLastUsed("missing", time.Now()); err != nil {
		t.Errorf("expected nil for missing, got %v", err)
	}
}

func names(ps []domain.Profile) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}
