package monitor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLogFollower_TailsAppendedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	if err := os.WriteFile(path, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan string, 16)
	follower, err := newLogFollower(path, out)
	if err != nil {
		t.Fatalf("newLogFollower: %v", err)
	}
	go follower.run(ctx)

	// Drain pre-existing line.
	select {
	case line := <-out:
		if line != "first" {
			t.Fatalf("first emitted line = %q, want %q", line, "first")
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive pre-existing line")
	}

	// Append two more.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("second\nthird\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	got := []string{}
	deadline := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case line := <-out:
			got = append(got, line)
		case <-deadline:
			t.Fatalf("timed out after %d new lines", len(got))
		}
	}
	if got[0] != "second" || got[1] != "third" {
		t.Fatalf("got %v, want [second third]", got)
	}
}

func TestLogFollower_MissingFileReturnsError(t *testing.T) {
	out := make(chan string, 1)
	if _, err := newLogFollower("/nonexistent/path.log", out); err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestLogFollower_FlushesTrailingPartialLine pins the L1 behavior: a final
// write missing the trailing '\n' (e.g. a crashing backend that prints a
// traceback then os._exit) is still surfaced. emit() runs its len(line)>0
// branch before returning on io.EOF, so the partial line is not dropped. This
// deliberately favors crash-message visibility. See BUGS.md L1.
func TestLogFollower_FlushesTrailingPartialLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	if err := os.WriteFile(path, []byte("first\npartial"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan string, 16)
	follower, err := newLogFollower(path, out)
	if err != nil {
		t.Fatalf("newLogFollower: %v", err)
	}
	go follower.run(ctx)

	got := []string{}
	deadline := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case line := <-out:
			got = append(got, line)
		case <-deadline:
			t.Fatalf("timed out; got %v, want [first partial]", got)
		}
	}
	if got[0] != "first" || got[1] != "partial" {
		t.Fatalf("got %v, want [first partial]", got)
	}
}
