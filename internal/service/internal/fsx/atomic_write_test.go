package fsx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestWriteJSONAtomic(t *testing.T) {
	t.Run("creates_parent_dir", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "nested", "deep", "file.json")
		v := map[string]string{"key": "value"}

		if err := WriteJSONAtomic(path, v); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected file to exist: %v", err)
		}
	})

	t.Run("writes_indented_json", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.json")
		v := map[string]string{"key": "value"}

		if err := WriteJSONAtomic(path, v); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		want := "{\n  \"key\": \"value\"\n}"
		if string(data) != want {
			t.Fatalf("got %q, want %q", string(data), want)
		}
	})

	t.Run("atomically_replaces", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.json")
		if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}

		v := map[string]string{"key": "new"}
		if err := WriteJSONAtomic(path, v); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		if string(data) == "old" {
			t.Fatal("expected file to be replaced")
		}
	})

	t.Run("removes_tmp_on_rename_failure", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.json")
		// Create a directory with the same name as the target file so rename fails.
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}

		v := map[string]string{"key": "value"}
		if err := WriteJSONAtomic(path, v); err == nil {
			t.Fatal("expected error, got nil")
		}

		// Unique-named temp must be cleaned up: no leftover temp files remain.
		leftovers, _ := filepath.Glob(filepath.Join(dir, ".file.json.tmp-*"))
		if len(leftovers) != 0 {
			t.Fatalf("expected temp files removed, found %v", leftovers)
		}
	})

	t.Run("sets_permissions", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "file.json")
		v := map[string]string{"key": "value"}

		if err := WriteJSONAtomic(path, v); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat file: %v", err)
		}
		mode := info.Mode().Perm()
		if mode != 0o644 {
			t.Fatalf("got permissions %o, want %o", mode, 0o644)
		}
	})
}

// TestWriteJSONAtomic_ConcurrentWritersNeverCorrupt guards audit P-C9: unique
// temp names let concurrent writers to the same path never rename each other's
// half-written temp, so the final file is always one whole payload.
func TestWriteJSONAtomic_ConcurrentWritersNeverCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.json")

	payloads := []map[string]string{{"writer": "A"}, {"writer": "B"}}
	var wg sync.WaitGroup
	for _, p := range payloads {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if err := WriteJSONAtomic(path, p); err != nil {
					t.Errorf("write: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("file is corrupt (not valid JSON): %v\n%s", err, data)
	}
	if got["writer"] != "A" && got["writer"] != "B" {
		t.Fatalf("file = %v, want one whole payload", got)
	}
}
