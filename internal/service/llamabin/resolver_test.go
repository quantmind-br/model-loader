package llamabin

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEffective(t *testing.T) {
	tests := []struct {
		name      string
		profile   string
		global    string
		want      string
	}{
		{"profile overrides global", "/opt/a", "/opt/b", "/opt/a"},
		{"global when no profile", "", "/opt/b", "/opt/b"},
		{"default when neither", "", "", DefaultName},
		{"profile overrides empty global", "/opt/a", "", "/opt/a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Effective(tt.profile, tt.global)
			if got != tt.want {
				t.Errorf("Effective(%q, %q) = %q, want %q", tt.profile, tt.global, got, tt.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	tmp := t.TempDir()

	// Create an executable file
	execFile := filepath.Join(tmp, "llama-server")
	if err := os.WriteFile(execFile, []byte("#!/bin/sh\necho hello"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a non-executable file
	nonExec := filepath.Join(tmp, "non-exec")
	if err := os.WriteFile(nonExec, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a directory
	dirPath := filepath.Join(tmp, "dir")
	if err := os.Mkdir(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		 raw    string
		wantErr error
	}{
		{"empty returns default", "", nil},
		{"absolute executable", execFile, nil},
		{"absolute non-executable", nonExec, ErrBinaryNotExecutable},
		{"absolute directory", dirPath, ErrBinaryIsDirectory},
		{"non-existent absolute", filepath.Join(tmp, "missing"), ErrBinaryNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Resolve(tt.raw)
			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("Resolve(%q) expected error %v, got nil", tt.raw, tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Resolve(%q) error = %v, want %v", tt.raw, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("Resolve(%q) unexpected error: %v", tt.raw, err)
			}
		})
	}
}

func TestResolveInPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH test skipped on windows")
	}

	tmp := t.TempDir()
	execFile := filepath.Join(tmp, "llama-server")
	if err := os.WriteFile(execFile, []byte("#!/bin/sh\necho hello"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Save and modify PATH
	oldPATH := os.Getenv("PATH")
	os.Setenv("PATH", tmp)
	defer os.Setenv("PATH", oldPATH)

	resolved, err := Resolve("llama-server")
	if err != nil {
		t.Fatalf("Resolve(\"llama-server\") error: %v", err)
	}
	if resolved != execFile {
		t.Errorf("Resolve(\"llama-server\") = %q, want %q", resolved, execFile)
	}

	// Missing binary in PATH
	os.Setenv("PATH", "")
	_, err = Resolve("llama-server")
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Errorf("Resolve(\"llama-server\") with empty PATH expected ErrBinaryNotFound, got %v", err)
	}
}
