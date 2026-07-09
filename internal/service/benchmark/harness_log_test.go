package benchmark

import (
	"reflect"
	"strings"
	"testing"
)

// onLine forwards each complete, non-blank output line (CR-trimmed) live, even
// when a line spans several Write calls.
func TestHarnessLog_OnLineAcrossWrites(t *testing.T) {
	var got []string
	h := newHarnessLog("", "x", func(s string) { got = append(got, s) })
	defer h.Close()

	_, _ = h.Write([]byte("hello wor"))          // partial, no newline yet
	_, _ = h.Write([]byte("ld\nsecond line\r\n")) // completes line 1 + CRLF line 2
	_, _ = h.Write([]byte("\n"))                  // blank suppressed
	_, _ = h.Write([]byte("   \n"))               // whitespace-only suppressed
	_, _ = h.Write([]byte("third"))              // partial, held back

	if want := []string{"hello world", "second line"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after partial writes got %q, want %q", got, want)
	}

	_, _ = h.Write([]byte("\n")) // completes "third"
	if want := []string{"hello world", "second line", "third"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after final newline got %q, want %q", got, want)
	}
}

// A newline-less flood keeps the partial-line buffer bounded; the eventually
// emitted line never exceeds harnessLineBufMax runes/bytes.
func TestHarnessLog_OnLineBoundedBuffer(t *testing.T) {
	var got []string
	h := newHarnessLog("", "x", func(s string) { got = append(got, s) })
	defer h.Close()

	_, _ = h.Write([]byte(strings.Repeat("A", harnessLineBufMax+5000)))
	if len(got) != 0 {
		t.Fatalf("no newline yet, got %d lines", len(got))
	}
	_, _ = h.Write([]byte("\n"))
	if len(got) != 1 {
		t.Fatalf("got %d lines, want 1", len(got))
	}
	if len(got[0]) > harnessLineBufMax {
		t.Errorf("emitted line len = %d, want <= %d", len(got[0]), harnessLineBufMax)
	}
}

// A nil onLine keeps the sink working (tail only) with no line splitting.
func TestHarnessLog_NilOnLine(t *testing.T) {
	h := newHarnessLog("", "x", nil)
	defer h.Close()
	_, _ = h.Write([]byte("a\nb\n"))
	if h.Tail() != "a\nb\n" {
		t.Errorf("tail = %q, want a\\nb\\n", h.Tail())
	}
}

// BR16: a bare-CR (\r) progress redraw with no interleaving newline must not
// reach onLine carrying embedded carriage returns — only the final overwrite
// segment is emitted, while ordinary CRLF lines are unaffected.
func TestHarnessLog_OnLineStripsInteriorCR(t *testing.T) {
	var got []string
	h := newHarnessLog("", "x", func(s string) { got = append(got, s) })
	defer h.Close()

	_, _ = h.Write([]byte("10%\r 50%\r100%\n")) // CR redraw -> only the final segment
	_, _ = h.Write([]byte("plain line\r\n"))    // ordinary CRLF stays intact
	_, _ = h.Write([]byte("\rleading-cr\n"))    // leading CR collapses to its segment

	if want := []string{"100%", "plain line", "leading-cr"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("interior-CR handling got %q, want %q", got, want)
	}
}
