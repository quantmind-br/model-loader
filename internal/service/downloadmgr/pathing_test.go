package downloadmgr

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDest(t *testing.T) {
	t.Run("happy individual file nests under publisher/repo", func(t *testing.T) {
		dir := t.TempDir()
		destDir, destFile, err := ResolveDest(dir, "org/repo", "model.gguf", false)
		if err != nil {
			t.Fatalf("expected nil err, got %v", err)
		}
		wantDir := filepath.Join(dir, "org", "repo")
		if destDir != wantDir {
			t.Errorf("destDir: want %q, got %q", wantDir, destDir)
		}
		wantFile := filepath.Join(wantDir, "model.gguf")
		if destFile != wantFile {
			t.Errorf("destFile: want %q, got %q", wantFile, destFile)
		}
	})

	t.Run("happy snapshot nests under publisher/repo", func(t *testing.T) {
		dir := t.TempDir()
		destDir, destFile, err := ResolveDest(dir, "org/repo", "config.json", true)
		if err != nil {
			t.Fatalf("expected nil err, got %v", err)
		}
		wantDir := filepath.Join(dir, "org", "repo")
		if destDir != wantDir {
			t.Errorf("destDir: want %q, got %q", wantDir, destDir)
		}
		wantFile := filepath.Join(wantDir, "config.json")
		if destFile != wantFile {
			t.Errorf("destFile: want %q, got %q", wantFile, destFile)
		}
	})

	t.Run("snapshot preserves subdir in rfilename", func(t *testing.T) {
		dir := t.TempDir()
		destDir, destFile, err := ResolveDest(dir, "org/repo", "subdir/file.json", true)
		if err != nil {
			t.Fatalf("expected nil err, got %v", err)
		}
		wantDir := filepath.Join(dir, "org", "repo")
		if destDir != wantDir {
			t.Errorf("destDir: want %q, got %q", wantDir, destDir)
		}
		wantFile := filepath.Join(wantDir, "subdir", "file.json")
		if destFile != wantFile {
			t.Errorf("destFile: want %q, got %q", wantFile, destFile)
		}
	})

	t.Run("empty searchPath returns ErrNoSearchPath", func(t *testing.T) {
		_, _, err := ResolveDest("", "org/repo", "model.gguf", false)
		if !errors.Is(err, ErrNoSearchPath) {
			t.Errorf("want ErrNoSearchPath, got %v", err)
		}
	})

	t.Run("missing dir returns ErrSearchPathMissing", func(t *testing.T) {
		base := t.TempDir()
		missing := filepath.Join(base, "does-not-exist")
		_, _, err := ResolveDest(missing, "org/repo", "model.gguf", false)
		if !errors.Is(err, ErrSearchPathMissing) {
			t.Errorf("want ErrSearchPathMissing, got %v", err)
		}
	})

	t.Run("searchPath that is a file returns ErrSearchPathMissing", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "not-a-dir")
		if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err := ResolveDest(filePath, "org/repo", "model.gguf", false)
		if !errors.Is(err, ErrSearchPathMissing) {
			t.Errorf("want ErrSearchPathMissing, got %v", err)
		}
	})

	t.Run("existing file in individual mode returns ErrAlreadyExists", func(t *testing.T) {
		dir := t.TempDir()
		nested := filepath.Join(dir, "org", "repo")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		existing := filepath.Join(nested, "model.gguf")
		if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err := ResolveDest(dir, "org/repo", "model.gguf", false)
		if !errors.Is(err, ErrAlreadyExists) {
			t.Errorf("want ErrAlreadyExists, got %v", err)
		}
	})

	t.Run("existing snapshot dir returns ErrAlreadyExists", func(t *testing.T) {
		dir := t.TempDir()
		existingDir := filepath.Join(dir, "org", "repo")
		if err := os.MkdirAll(existingDir, 0o755); err != nil {
			t.Fatal(err)
		}
		_, _, err := ResolveDest(dir, "org/repo", "config.json", true)
		if !errors.Is(err, ErrAlreadyExists) {
			t.Errorf("want ErrAlreadyExists, got %v", err)
		}
	})

	t.Run("repoID with .. is rejected as traversal", func(t *testing.T) {
		dir := t.TempDir()
		_, _, err := ResolveDest(dir, "../../etc", "model.gguf", false)
		if !errors.Is(err, ErrPathTraversal) {
			t.Errorf("want ErrPathTraversal, got %v", err)
		}
	})

	t.Run("snapshot path traversal returns ErrPathTraversal", func(t *testing.T) {
		dir := t.TempDir()
		_, _, err := ResolveDest(dir, "org/repo", "../../../etc/passwd", true)
		if !errors.Is(err, ErrPathTraversal) {
			t.Errorf("want ErrPathTraversal, got %v", err)
		}
	})
}
