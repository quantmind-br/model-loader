package processmgr

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestHistory_LoadMissingReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := loadHistory(filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if got != nil && len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

func TestHistory_LoadCorruptReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadHistory(path); err == nil {
		t.Fatal("expected error for corrupt history, got nil")
	}
}

func TestHistory_SaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.json")

	want := []domain.ExitedInstance{
		{ProfileID: "qwen", PID: 4521, Port: 8080,
			StartedAt: time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second),
			ExitedAt:  time.Now().UTC().Truncate(time.Second),
			DurationSeconds: 7200, Background: true, ExitReason: "exit:0"},
	}
	if err := saveHistory(path, want); err != nil {
		t.Fatalf("saveHistory: %v", err)
	}
	got, err := loadHistory(path)
	if err != nil {
		t.Fatalf("loadHistory: %v", err)
	}
	if len(got) != 1 || got[0].PID != 4521 || got[0].ProfileID != "qwen" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestHistory_SaveAtomicNoTmpLeftover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.json")
	if err := saveHistory(path, []domain.ExitedInstance{{ProfileID: "a", PID: 1, Port: 9}}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover tmp: %s", e.Name())
		}
	}
}

func TestHistory_CapLimitsToLatest(t *testing.T) {
	entries := []domain.ExitedInstance{
		{PID: 1, ExitedAt: time.Now().UTC().Add(-3 * time.Hour)},
		{PID: 2, ExitedAt: time.Now().UTC().Add(-2 * time.Hour)},
		{PID: 3, ExitedAt: time.Now().UTC().Add(-1 * time.Hour)},
	}
	got := capHistory(entries, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].PID != 3 {
		t.Fatalf("expected newest PID 3, got %d", got[0].PID)
	}
	if got[1].PID != 2 {
		t.Fatalf("expected second newest PID 2, got %d", got[1].PID)
	}
}

func TestHistory_CapDefaultLimit(t *testing.T) {
	entries := make([]domain.ExitedInstance, 60)
	for i := range entries {
		entries[i] = domain.ExitedInstance{PID: i, ExitedAt: time.Now().UTC().Add(time.Duration(-i) * time.Minute)}
	}
	got := capHistory(entries, 0)
	if len(got) != defaultHistoryLimit {
		t.Fatalf("expected default limit %d, got %d", defaultHistoryLimit, len(got))
	}
}

func TestHistory_AppendDedupByPID(t *testing.T) {
	mgr, _ := newTestManager(t)
	mgr.mu.Lock()
	inst := domain.RunningInstance{ProfileID: "p", PID: 100, Port: 8080, StartedAt: time.Now().UTC().Add(-time.Minute)}
	appended := mgr.appendHistoryLocked(inst, "exit:0", time.Now().UTC())
	if !appended {
		t.Fatal("expected first append to succeed")
	}
	appended2 := mgr.appendHistoryLocked(inst, "killed", time.Now().UTC())
	if appended2 {
		t.Fatal("expected second append for same PID to be deduped")
	}
	mgr.mu.Unlock()
	if len(mgr.History()) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(mgr.History()))
	}
}

func TestHistory_PersistAndReload(t *testing.T) {
	mgr, _ := newTestManager(t)
	mgr.mu.Lock()
	inst := domain.RunningInstance{ProfileID: "p", PID: 200, Port: 8080, StartedAt: time.Now().UTC().Add(-time.Minute)}
	mgr.appendHistoryLocked(inst, "exit:0", time.Now().UTC())
	mgr.mu.Unlock()
	if err := mgr.persistHistory(); err != nil {
		t.Fatalf("persistHistory: %v", err)
	}

	// Simulate restart: new manager loading the same history file.
	mgr2 := New(Config{
		Resolver:     mgr.resolver,
		LogDir:       mgr.logDir,
		RegistryPath: mgr.registryPath,
		HistoryPath:  mgr.historyPath,
	})
	if len(mgr2.History()) != 1 || mgr2.History()[0].PID != 200 {
		t.Fatalf("expected history to reload with 1 entry for PID 200, got %+v", mgr2.History())
	}
}
