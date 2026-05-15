// Package log wires a stdlib log/slog logger that writes ONLY to a session-rotated
// file under cfg.Paths.LogDir. The handler is intentionally file-only because
// bubbletea owns stdout/stderr while the TUI alt-screen is active — any write to
// stderr during prog.Run() corrupts the framebuffer.
//
// The writer is unbuffered (direct *os.File.Write) so callers that hit os.Exit(1)
// preserve every log line via the kernel page cache without explicit Flush ceremony.
package log

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maxRotatedFiles caps how many timestamped logs are kept (newest-first).
const maxRotatedFiles = 5

// activeName is the live log file name; rotated files are activeName + "." + ts + ".log".
const activeName = "model-loader.log"

// rotatedGlob matches the timestamped archives (excludes the active file).
const rotatedGlob = "model-loader.*.log"

// Config drives New. Dir is the directory the logger writes into (created if missing).
// Level is the slog level — typically the result of ResolveLevel.
type Config struct {
	Dir   string
	Level slog.Level
}

// New opens a session-rotated log file under cfg.Dir and returns a *slog.Logger
// writing text-format records to it. The returned closeFn closes the file; call
// it via defer in main. An MkdirAll or OpenFile failure returns (nil, nil, err)
// — main.go is expected to write the error to stderr (TUI is not yet active)
// and os.Exit(1).
func New(cfg Config) (*slog.Logger, func(), error) {
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("mkdir log dir: %w", err)
	}
	rotate(cfg.Dir)
	active := filepath.Join(cfg.Dir, activeName)
	f, err := os.OpenFile(active, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}
	handler := slog.NewTextHandler(f, &slog.HandlerOptions{Level: cfg.Level})
	logger := slog.New(handler)
	closeFn := func() { _ = f.Close() }
	return logger, closeFn, nil
}

// rotate renames the existing active log to a timestamped name and trims the
// archive to maxRotatedFiles entries (newest-first). Best-effort: any error is
// swallowed so a partially-broken log dir still permits a fresh session.
func rotate(dir string) {
	active := filepath.Join(dir, activeName)
	if _, err := os.Stat(active); err == nil {
		ts := time.Now().UTC().Format("20060102T150405Z")
		_ = os.Rename(active, filepath.Join(dir, "model-loader."+ts+".log"))
	}
	matches, err := filepath.Glob(filepath.Join(dir, rotatedGlob))
	if err != nil {
		return
	}
	// Timestamp suffix is colon-free YYYYMMDDTHHMMSSZ; lexicographic reverse =
	// chronological newest-first. Keep first maxRotatedFiles; remove the rest.
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	for i, p := range matches {
		if i >= maxRotatedFiles {
			_ = os.Remove(p)
		}
	}
}

// ResolveLevel collapses three string sources into an slog.Level with the
// documented precedence (CLI > env > config > default "info"). Unrecognized
// values silently fall back to slog.LevelInfo — flag.Parse rejects invalid
// CLI values before this is called, and env/config drift should not crash
// the binary.
func ResolveLevel(cli, env, cfg string) slog.Level {
	for _, src := range []string{cli, env, cfg} {
		if lvl, ok := parseLevel(src); ok {
			return lvl
		}
	}
	return slog.LevelInfo
}

// parseLevel interprets "debug"|"info"|"warn"|"error" (case-insensitive).
// Empty strings return (Info, false) so ResolveLevel can skip to the next source.
func parseLevel(s string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	}
	return slog.LevelInfo, false
}

// NewAttemptID returns an 8-character correlation ID for one launch attempt.
// 5 random bytes encoded as base32 (no padding) → 8 chars from [A-Z2-7].
// Collision space ~1 in 10^12 across one session — sufficient for grep.
func NewAttemptID() string {
	var b [5]byte
	// crypto/rand.Read never errors on Linux post-Go 1.19 (getrandom(2)).
	_, _ = rand.Read(b[:])
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])
}

// Nop returns a logger that discards every record. Use for tests, for the
// nil-tolerant defaults inside service constructors, and for components that
// run outside the spawn/load path (e.g. profile_editor's validator).
func Nop() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
