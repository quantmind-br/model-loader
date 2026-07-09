package processmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestReadStderrTail_LargeFileReturnsLastLines — audit C5: a >64 KiB log is
// read via a bounded tail, still returning the exact last maxLines lines.
func TestReadStderrTail_LargeFileReturnsLastLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.log")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 5000 { // ~50 bytes/line → ~250 KiB, well past 64 KiB
		fmt.Fprintf(f, "line %d padding padding padding padding padding\n", i)
	}
	_ = f.Close()

	got := readStderrTail(p, 50)
	if len(got) != 50 {
		t.Fatalf("got %d lines; want 50", len(got))
	}
	if want := "line 4999 padding padding padding padding padding"; got[len(got)-1] != want {
		t.Fatalf("last line = %q; want %q", got[len(got)-1], want)
	}
	if want := "line 4950 padding padding padding padding padding"; got[0] != want {
		t.Fatalf("first tail line = %q; want %q", got[0], want)
	}
}

// TestReadStderrTail_SmallFileUnchanged — a sub-64 KiB log returns every line.
func TestReadStderrTail_SmallFileUnchanged(t *testing.T) {
	p := filepath.Join(t.TempDir(), "small.log")
	if err := os.WriteFile(p, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readStderrTail(p, 50)
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("got %v; want [a b c]", got)
	}
}
