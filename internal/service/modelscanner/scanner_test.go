package modelscanner

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func writeGGUFFile(t *testing.T, path string, paramCount uint64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	buf.WriteString("GGUF")
	binary.Write(&buf, binary.LittleEndian, uint32(3))
	binary.Write(&buf, binary.LittleEndian, uint64(0)) // tensors
	binary.Write(&buf, binary.LittleEndian, uint64(1)) // metas
	binary.Write(&buf, binary.LittleEndian, uint64(len("general.parameter_count")))
	buf.WriteString("general.parameter_count")
	binary.Write(&buf, binary.LittleEndian, uint32(10)) // uint64
	binary.Write(&buf, binary.LittleEndian, paramCount)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func collect(ch <-chan domain.ScanEvent) []domain.ScanEvent {
	var out []domain.ScanEvent
	for evt := range ch {
		out = append(out, evt)
	}
	return out
}

func TestScanner_FindsSingleGGUF(t *testing.T) {
	dir := t.TempDir()
	writeGGUFFile(t, filepath.Join(dir, "Qwen-32B-Q4_K_M.gguf"), 32_000_000_000)

	s := New()
	ch, err := s.Scan(context.Background(), []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	events := collect(ch)

	var files []*domain.ModelFile
	for _, e := range events {
		if e.Type == domain.ScanEventFile {
			files = append(files, e.File)
		}
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %#v", len(files), events)
	}
	got := files[0]
	if got.Name != "Qwen-32B-Q4_K_M.gguf" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Quant != "Q4_K_M" {
		t.Errorf("Quant = %q, want Q4_K_M", got.Quant)
	}
	if got.Params != "32B" {
		t.Errorf("Params = %q, want 32B", got.Params)
	}
	if got.SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want >0", got.SizeBytes)
	}
}

func TestScanner_RecursiveAndIgnoresNonGGUF(t *testing.T) {
	dir := t.TempDir()
	writeGGUFFile(t, filepath.Join(dir, "top.gguf"), 7_000_000_000)
	writeGGUFFile(t, filepath.Join(dir, "sub", "deep.gguf"), 13_000_000_000)
	writeGGUFFile(t, filepath.Join(dir, "sub", "sub2", "deeper.gguf"), 70_000_000_000)
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New()
	ch, err := s.Scan(context.Background(), []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	events := collect(ch)

	count := 0
	for _, e := range events {
		if e.Type == domain.ScanEventFile {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("file events = %d, want 3 (got events: %#v)", count, events)
	}
}

func TestScanner_EmitsProgressAndDone(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeGGUFFile(t, filepath.Join(dirA, "a.gguf"), 1_000_000_000)
	writeGGUFFile(t, filepath.Join(dirB, "b1.gguf"), 1_000_000_000)
	writeGGUFFile(t, filepath.Join(dirB, "b2.gguf"), 1_000_000_000)

	s := New()
	ch, err := s.Scan(context.Background(), []string{dirA, dirB})
	if err != nil {
		t.Fatal(err)
	}
	events := collect(ch)

	progressByRoot := map[string]int{}
	doneCount := 0
	for _, e := range events {
		switch e.Type {
		case domain.ScanEventProgress:
			progressByRoot[e.Root] = e.Count
		case domain.ScanEventDone:
			doneCount++
		}
	}
	if progressByRoot[dirA] != 1 {
		t.Errorf("progress[dirA] = %d, want 1", progressByRoot[dirA])
	}
	if progressByRoot[dirB] != 2 {
		t.Errorf("progress[dirB] = %d, want 2", progressByRoot[dirB])
	}
	if doneCount != 1 {
		t.Errorf("done events = %d, want 1", doneCount)
	}
}

func TestScanner_ErrorOnMissingRoot(t *testing.T) {
	s := New()
	ch, err := s.Scan(context.Background(), []string{"/definitely/does/not/exist/xyz"})
	if err != nil {
		t.Fatal(err)
	}
	events := collect(ch)

	errEvents := 0
	for _, e := range events {
		if e.Type == domain.ScanEventError {
			errEvents++
		}
	}
	if errEvents != 1 {
		t.Fatalf("error events = %d, want 1; got events: %#v", errEvents, events)
	}
}

func TestScanner_RespectsContextCancel(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 50; i++ {
		writeGGUFFile(t, filepath.Join(dir, "m"+string(rune('a'+i%26))+".gguf"), 1_000_000_000)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := New()
	ch, err := s.Scan(ctx, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	// Cancel almost immediately.
	cancel()

	// Drain the channel; we just need it to close without hanging.
	for range ch {
	}
}
