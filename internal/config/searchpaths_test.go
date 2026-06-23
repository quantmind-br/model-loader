package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUpdateSearchPathsAt_RewritesAndPreservesOtherValues covers the B9
// persistence path: dropping a broken search path must rewrite
// models.search_paths while leaving unrelated config values intact.
func TestUpdateSearchPathsAt_RewritesAndPreservesOtherValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	seed := "[models]\nsearch_paths = [\"/a\", \"/b\"]\n\n[serve]\nport = 9999\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := updateSearchPathsAt(path, []string{"/a"}); err != nil {
		t.Fatalf("updateSearchPathsAt: %v", err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if len(cfg.Models.SearchPaths) != 1 || cfg.Models.SearchPaths[0] != "/a" {
		t.Errorf("SearchPaths = %v, want [/a]", cfg.Models.SearchPaths)
	}
	if cfg.Serve.Port != 9999 {
		t.Errorf("Serve.Port = %d, want 9999 (unrelated values must survive the rewrite)", cfg.Serve.Port)
	}
}
