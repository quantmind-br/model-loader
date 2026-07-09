package log

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew_CreatesLogDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "logs")
	lg, closeFn, err := New(Config{Dir: dir, Level: slog.LevelInfo})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeFn()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("log dir not created: %v", err)
	}
	lg.Info("hello", "k", "v")
	data, err := os.ReadFile(filepath.Join(dir, activeName))
	if err != nil {
		t.Fatalf("read active log: %v", err)
	}
	if !strings.Contains(string(data), `msg=hello`) {
		t.Errorf("expected msg=hello in log; got %q", string(data))
	}
}

func TestNew_RotatesExistingLog(t *testing.T) {
	dir := t.TempDir()
	// Pre-create an active log with sentinel content.
	if err := os.WriteFile(filepath.Join(dir, activeName), []byte("OLD\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, closeFn, err := New(Config{Dir: dir, Level: slog.LevelInfo, Rotate: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeFn()
	matches, _ := filepath.Glob(filepath.Join(dir, rotatedGlob))
	if len(matches) != 1 {
		t.Fatalf("expected exactly 1 rotated file, got %d: %v", len(matches), matches)
	}
	rotated, _ := os.ReadFile(matches[0])
	if string(rotated) != "OLD\n" {
		t.Errorf("expected rotated content 'OLD\\n'; got %q", string(rotated))
	}
	active, _ := os.ReadFile(filepath.Join(dir, activeName))
	if len(active) != 0 {
		t.Errorf("expected fresh active log; got %d bytes", len(active))
	}
}

func TestNew_Caps5Files(t *testing.T) {
	dir := t.TempDir()
	seed := []string{
		"model-loader.20260101T000001Z.log",
		"model-loader.20260102T000002Z.log",
		"model-loader.20260103T000003Z.log",
		"model-loader.20260104T000004Z.log",
		"model-loader.20260105T000005Z.log",
		"model-loader.20260106T000006Z.log",
	}
	for _, n := range seed {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644); err != nil {
			t.Fatalf("seed %s: %v", n, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, activeName), []byte("live"), 0o644); err != nil {
		t.Fatalf("seed active: %v", err)
	}
	_, closeFn, err := New(Config{Dir: dir, Level: slog.LevelInfo, Rotate: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeFn()
	matches, _ := filepath.Glob(filepath.Join(dir, rotatedGlob))
	if len(matches) != 5 {
		t.Fatalf("expected 5 rotated files after cap, got %d: %v", len(matches), matches)
	}
	for _, m := range matches {
		if strings.Contains(m, "20260101T000001Z") || strings.Contains(m, "20260102T000002Z") {
			t.Errorf("expected oldest archives removed, found %s", m)
		}
	}
}

func TestNew_MkdirAllFailureReturnsError(t *testing.T) {
	parent := t.TempDir()
	block := filepath.Join(parent, "block")
	if err := os.WriteFile(block, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	lg, closeFn, err := New(Config{Dir: filepath.Join(block, "logs"), Level: slog.LevelInfo})
	if err == nil {
		closeFn()
		t.Fatalf("expected error, got logger=%v", lg)
	}
	if lg != nil || closeFn != nil {
		t.Errorf("expected nil logger and nil closeFn on error, got logger=%v closeFn!=nil=%v", lg, closeFn != nil)
	}
}

func TestResolveLevel_Precedence(t *testing.T) {
	cases := []struct {
		name          string
		cli, env, cfg string
		want          slog.Level
	}{
		{"all_empty", "", "", "", slog.LevelInfo},
		{"cli_wins", "debug", "warn", "error", slog.LevelDebug},
		{"env_wins_when_no_cli", "", "warn", "error", slog.LevelWarn},
		{"cfg_wins_when_no_cli_env", "", "", "error", slog.LevelError},
		{"case_insensitive", "DEBUG", "", "", slog.LevelDebug},
		{"warning_alias", "warning", "", "", slog.LevelWarn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveLevel(tc.cli, tc.env, tc.cfg); got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestResolveLevel_UnrecognizedFallsBackToInfo(t *testing.T) {
	if got := ResolveLevel("garbage", "junk", "unknown"); got != slog.LevelInfo {
		t.Errorf("want LevelInfo for all-unrecognized, got %v", got)
	}
}

func TestNewAttemptID_8CharsBase32(t *testing.T) {
	id := NewAttemptID()
	if len(id) != 8 {
		t.Errorf("want 8 chars, got %d: %q", len(id), id)
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	for _, r := range id {
		if !strings.ContainsRune(alphabet, r) {
			t.Errorf("non-base32 rune %q in id %q", r, id)
		}
	}
}

func TestNewAttemptID_LowCollisionRate(t *testing.T) {
	seen := make(map[string]struct{}, 10_000)
	for i := 0; i < 10_000; i++ {
		id := NewAttemptID()
		if _, dup := seen[id]; dup {
			t.Fatalf("collision at i=%d: id=%q", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestNop_WritesNothing(t *testing.T) {
	lg := Nop()
	if lg == nil {
		t.Fatal("Nop returned nil")
	}
	lg.Debug("x")
	lg.Info("x")
	lg.Warn("x")
	lg.Error("x")
}

// TestNew_NoRotateLeavesActiveIntact — audit C6: an observer (Rotate:false)
// appends to the active log without renaming it, so it cannot trim/rename the
// file a live owner session holds open.
func TestNew_NoRotateLeavesActiveIntact(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, activeName), []byte("LIVE\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, closeFn, err := New(Config{Dir: dir, Level: slog.LevelInfo, Rotate: false})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeFn()
	if matches, _ := filepath.Glob(filepath.Join(dir, rotatedGlob)); len(matches) != 0 {
		t.Fatalf("Rotate:false must not create rotated archives; got %v", matches)
	}
	active, _ := os.ReadFile(filepath.Join(dir, activeName))
	if !strings.HasPrefix(string(active), "LIVE\n") {
		t.Fatalf("Rotate:false must append to the existing active log, not rename it; got %q", string(active))
	}
}
