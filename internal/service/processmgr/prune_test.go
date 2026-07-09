package processmgr

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPruneBackendLogs — audit C1: keep the N newest backend logs per profile,
// always keep exempt (live/history) and non-backend files.
func TestPruneBackendLogs(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, age time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-age)
		_ = os.Chtimes(p, mt, mt)
	}
	// profile "alpha": five backend logs, decreasing age.
	write("alpha-1000.log", 10*time.Hour) // exempt (live)
	write("alpha-1001.log", 4*time.Hour)
	write("alpha-1002.log", 3*time.Hour)
	write("alpha-1003.log", 2*time.Hour)
	write("alpha-1004.log", 1*time.Hour)
	// profile "beta": single log → survives (<= keepPerProfile).
	write("beta-2001.log", 5*time.Hour)
	// non-backend files must never be touched.
	write("model-loader.log", 20*time.Hour)
	write("proxy.log", 20*time.Hour)
	write("model-loader.20260101T000000Z.log", 20*time.Hour)

	exempt := map[string]struct{}{"alpha-1000.log": {}}
	removed := PruneBackendLogs(dir, exempt, 2, nil)

	survives := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	// alpha (excluding exempt 1000): keep 2 newest (1004,1003); drop 1002,1001.
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	for _, keep := range []string{
		"alpha-1004.log", "alpha-1003.log", "alpha-1000.log", "beta-2001.log",
		"model-loader.log", "proxy.log", "model-loader.20260101T000000Z.log",
	} {
		if !survives(keep) {
			t.Errorf("%s should have survived", keep)
		}
	}
	for _, gone := range []string{"alpha-1002.log", "alpha-1001.log"} {
		if survives(gone) {
			t.Errorf("%s should have been pruned", gone)
		}
	}
}
