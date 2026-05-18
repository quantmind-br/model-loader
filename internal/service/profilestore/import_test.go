package profilestore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestImportBundle_MergeSkipsExistingProfiles(t *testing.T) {
	s, _ := newStore(t)
	existing := sampleProfile("alpha", "Existing Alpha")
	if err := s.Save(existing); err != nil {
		t.Fatalf("Save existing: %v", err)
	}

	path := writeImportBundle(t, ExportBundle{
		SchemaVersion: ExportBundleSchemaVersion,
		ExportedAt:    fixedImportTime(),
		Profiles: []domain.Profile{
			sampleProfile("alpha", "Imported Alpha"),
			sampleProfile("beta", "Beta"),
		},
	})

	result, err := ImportBundle(s, path, ConflictModeMerge)
	if err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	if result != (ImportResult{Added: 1, Skipped: 1}) {
		t.Fatalf("result = %+v, want Added=1 Skipped=1", result)
	}

	gotAlpha, err := s.Get("alpha")
	if err != nil {
		t.Fatalf("Get alpha: %v", err)
	}
	if gotAlpha.Name != "Existing Alpha" {
		t.Errorf("alpha.Name = %q, want existing profile unchanged", gotAlpha.Name)
	}
	if _, err := s.Get("beta"); err != nil {
		t.Fatalf("Get beta: %v", err)
	}
}

func TestImportBundle_OverwriteReplacesExistingProfiles(t *testing.T) {
	s, _ := newStore(t)
	if err := s.Save(sampleProfile("alpha", "Existing Alpha")); err != nil {
		t.Fatalf("Save existing: %v", err)
	}

	path := writeImportBundle(t, ExportBundle{
		SchemaVersion: ExportBundleSchemaVersion,
		ExportedAt:    fixedImportTime(),
		Profiles: []domain.Profile{
			sampleProfile("alpha", "Imported Alpha"),
			sampleProfile("beta", "Beta"),
		},
	})

	result, err := ImportBundle(s, path, ConflictModeOverwrite)
	if err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	if result != (ImportResult{Added: 1, Replaced: 1}) {
		t.Fatalf("result = %+v, want Added=1 Replaced=1", result)
	}

	gotAlpha, err := s.Get("alpha")
	if err != nil {
		t.Fatalf("Get alpha: %v", err)
	}
	if gotAlpha.Name != "Imported Alpha" {
		t.Errorf("alpha.Name = %q, want imported profile", gotAlpha.Name)
	}
}

func TestImportBundle_RenameConflictingProfiles(t *testing.T) {
	s, _ := newStore(t)
	for _, p := range []domain.Profile{
		sampleProfile("alpha", "Existing Alpha"),
		sampleProfile("alpha-imported-1", "Existing Alpha Import 1"),
	} {
		if err := s.Save(p); err != nil {
			t.Fatalf("Save existing: %v", err)
		}
	}

	path := writeImportBundle(t, ExportBundle{
		SchemaVersion: ExportBundleSchemaVersion,
		ExportedAt:    fixedImportTime(),
		Profiles: []domain.Profile{
			sampleProfile("alpha", "Imported Alpha"),
			sampleProfile("beta", "Beta"),
		},
	})

	result, err := ImportBundle(s, path, ConflictModeRename)
	if err != nil {
		t.Fatalf("ImportBundle: %v", err)
	}
	if result != (ImportResult{Added: 1, Renamed: 1}) {
		t.Fatalf("result = %+v, want Added=1 Renamed=1", result)
	}

	renamed, err := s.Get("alpha-imported-2")
	if err != nil {
		t.Fatalf("Get renamed profile: %v", err)
	}
	if renamed.Name != "Imported Alpha" {
		t.Errorf("renamed.Name = %q, want imported profile", renamed.Name)
	}
	if _, err := s.Get("beta"); err != nil {
		t.Fatalf("Get beta: %v", err)
	}
}

func TestImportBundle_RejectsNewerSchema(t *testing.T) {
	s, _ := newStore(t)
	path := writeImportBundle(t, ExportBundle{
		SchemaVersion: ExportBundleSchemaVersion + 1,
		ExportedAt:    fixedImportTime(),
		Profiles:      []domain.Profile{sampleProfile("alpha", "Alpha")},
	})

	_, err := ImportBundle(s, path, ConflictModeMerge)
	if err == nil {
		t.Fatal("ImportBundle should reject newer schema")
	}
	if !strings.Contains(err.Error(), "newer than supported") {
		t.Fatalf("err = %v, want newer schema message", err)
	}
}

func TestImportBundle_RejectsUnknownFields(t *testing.T) {
	s, _ := newStore(t)
	path := filepath.Join(t.TempDir(), "bundle.json")
	raw := `{
		"schemaVersion": 1,
		"exportedAt": "2026-05-17T12:00:00Z",
		"profiles": [],
		"unexpected": true
	}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ImportBundle(s, path, ConflictModeMerge)
	if err == nil {
		t.Fatal("ImportBundle should reject unknown fields")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("err = %v, want unknown field message", err)
	}
}

func writeImportBundle(t *testing.T, bundle ExportBundle) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bundle.json")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(bundle); err != nil {
		t.Fatal(err)
	}
	return path
}

func fixedImportTime() time.Time {
	return time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
}
