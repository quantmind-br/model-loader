package profilestore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestExportAll_WritesBundleWithValidProfiles(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Save(sampleProfile("alpha", "Alpha")); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(sampleProfile("beta", "Beta")); err != nil {
		t.Fatal(err)
	}

	exportDir := t.TempDir()
	bundle, err := ExportAll(s, exportDir)
	if err != nil {
		t.Fatalf("ExportAll: %v", err)
	}

	if bundle.SchemaVersion != ExportBundleSchemaVersion {
		t.Errorf("bundle.SchemaVersion = %d, want %d", bundle.SchemaVersion, ExportBundleSchemaVersion)
	}
	if bundle.ExportedAt.IsZero() {
		t.Error("bundle.ExportedAt is zero")
	}
	if len(bundle.Profiles) != 2 {
		t.Fatalf("bundle.Profiles len = %d, want 2", len(bundle.Profiles))
	}
	if bundle.Profiles[0].Name != "Alpha" || bundle.Profiles[1].Name != "Beta" {
		t.Errorf("bundle.Profiles names = %v, want [Alpha Beta]", names(bundle.Profiles))
	}

	entries, err := os.ReadDir(exportDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("export dir entries = %d, want 1 (%v)", len(entries), entries)
	}
	name := entries[0].Name()
	re := regexp.MustCompile(`^profiles-export-\d{8}-\d{6}\.json$`)
	if !re.MatchString(name) {
		t.Errorf("filename %q does not match profiles-export-YYYYMMDD-HHMMSS.json", name)
	}

	raw, err := os.ReadFile(filepath.Join(exportDir, name))
	if err != nil {
		t.Fatal(err)
	}
	var disk ExportBundle
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("unmarshal export file: %v", err)
	}
	if disk.SchemaVersion != ExportBundleSchemaVersion {
		t.Errorf("disk.SchemaVersion = %d, want %d", disk.SchemaVersion, ExportBundleSchemaVersion)
	}
	if len(disk.Profiles) != 2 {
		t.Errorf("disk.Profiles len = %d, want 2", len(disk.Profiles))
	}

	if !strings.Contains(string(raw), "\n  ") {
		t.Errorf("export file is not indented JSON: %s", string(raw))
	}
}

func TestExportAll_ExcludesCorruptEntries(t *testing.T) {
	s, dir := newStore(t)
	if err := s.Save(sampleProfile("good", "Good")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	exportDir := t.TempDir()
	bundle, err := ExportAll(s, exportDir)
	if err != nil {
		t.Fatalf("ExportAll: %v", err)
	}

	if len(bundle.Profiles) != 1 {
		t.Fatalf("bundle.Profiles len = %d, want 1 (corrupt must be excluded)", len(bundle.Profiles))
	}
	if bundle.Profiles[0].ID != "good" {
		t.Errorf("bundle.Profiles[0].ID = %q, want good", bundle.Profiles[0].ID)
	}
}

func TestExportAll_EmptyStoreProducesEmptyProfilesArray(t *testing.T) {
	s, _ := newStore(t)
	exportDir := t.TempDir()

	bundle, err := ExportAll(s, exportDir)
	if err != nil {
		t.Fatalf("ExportAll: %v", err)
	}
	if bundle.Profiles == nil {
		t.Error("bundle.Profiles is nil, want non-nil empty slice")
	}
	if len(bundle.Profiles) != 0 {
		t.Errorf("bundle.Profiles len = %d, want 0", len(bundle.Profiles))
	}

	entries, err := os.ReadDir(exportDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("export dir entries = %d, want 1", len(entries))
	}

	raw, err := os.ReadFile(filepath.Join(exportDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"profiles": []`) {
		t.Errorf("expected empty profiles array in JSON; got: %s", string(raw))
	}
}

func TestExportAll_EmptyDirReturnsError(t *testing.T) {
	s, _ := newStore(t)
	_, err := ExportAll(s, "")
	if err == nil {
		t.Fatal("ExportAll with empty dir should error")
	}
}

func TestExportAll_ListErrorPropagates(t *testing.T) {
	wantErr := errors.New("boom")
	store := errorStore{listErr: wantErr}
	_, err := ExportAll(store, t.TempDir())
	if err == nil {
		t.Fatal("ExportAll should propagate List error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want wraps %v", err, wantErr)
	}
}

func TestExportFilename_UTCFormat(t *testing.T) {
	at := time.Date(2026, 5, 17, 14, 30, 45, 0, time.UTC)
	got := ExportFilename("/tmp/exports", at)
	want := filepath.Join("/tmp/exports", "profiles-export-20260517-143045.json")
	if got != want {
		t.Errorf("ExportFilename = %q, want %q", got, want)
	}
}

type errorStore struct {
	listErr error
}

func (e errorStore) List() ([]domain.Profile, error) { return nil, e.listErr }
func (e errorStore) ListWithDiagnostics() ([]domain.Profile, []ListDiagnostic, error) {
	return nil, nil, e.listErr
}
func (e errorStore) Get(string) (domain.Profile, error)    { return domain.Profile{}, ErrNotFound }
func (e errorStore) Save(domain.Profile) error             { return nil }
func (e errorStore) Delete(string) error                   { return nil }
func (e errorStore) Duplicate(string, string) (domain.Profile, error) {
	return domain.Profile{}, nil
}
