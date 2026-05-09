package processmgr

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestRegistry_SaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instances.json")

	want := []domain.RunningInstance{
		{ProfileID: "qwen", PID: 4521, Port: 8080,
			LogPath: "/log/qwen.log", StartedAt: time.Now().UTC().Truncate(time.Second), Background: true},
	}
	if err := saveRegistry(path, want); err != nil {
		t.Fatalf("saveRegistry: %v", err)
	}
	got, err := loadRegistry(path)
	if err != nil {
		t.Fatalf("loadRegistry: %v", err)
	}
	if len(got) != 1 || got[0].PID != 4521 || got[0].Port != 8080 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestRegistry_LoadMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := loadRegistry(filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if got != nil && len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

func TestRegistry_SaveAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instances.json")
	if err := saveRegistry(path, []domain.RunningInstance{{ProfileID: "a", PID: 1, Port: 9}}); err != nil {
		t.Fatal(err)
	}
	// confirm no .tmp leftover
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover tmp: %s", e.Name())
		}
	}
}

func TestRegistry_LoadCorruptReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instances.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRegistry(path); err == nil {
		t.Fatal("expected error for corrupt registry, got nil")
	}
}
