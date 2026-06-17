package metricsstore

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendAndRead(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	rec := Record{TS: now.Unix(), TokensPerSec: 42.0}
	if err := Append(dir, "p1", rec); err != nil {
		t.Fatal(err)
	}
	recs, err := Read(dir, "p1", now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0].TokensPerSec != 42.0 {
		t.Fatalf("expected TokensPerSec 42.0, got %f", recs[0].TokensPerSec)
	}
}

func TestReadSinceFilters(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)
	new := time.Now()
	Append(dir, "p1", Record{TS: old.Unix()})
	Append(dir, "p1", Record{TS: new.Unix()})
	recs, err := Read(dir, "p1", new.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	if recs[0].TS != new.Unix() {
		t.Fatalf("expected TS %d, got %d", new.Unix(), recs[0].TS)
	}
}

func TestCompactDropsOld(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-8 * 24 * time.Hour)
	new := time.Now()
	Append(dir, "p1", Record{TS: old.Unix(), TokensPerSec: 1.0})
	Append(dir, "p1", Record{TS: new.Unix(), TokensPerSec: 2.0})
	if err := Compact(dir, "p1", 7*24*time.Hour, 5<<20); err != nil {
		t.Fatal(err)
	}
	recs, err := Read(dir, "p1", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record after compact, got %d", len(recs))
	}
	if recs[0].TokensPerSec != 2.0 {
		t.Fatalf("expected TokensPerSec 2.0, got %f", recs[0].TokensPerSec)
	}
}

func TestCompactTrimsSize(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i := 0; i < 1000; i++ {
		Append(dir, "p1", Record{TS: now.Add(-time.Duration(i) * time.Minute).Unix(), TokensPerSec: float64(i)})
	}
	if err := Compact(dir, "p1", 30*24*time.Hour, 1024); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "p1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 1024 {
		t.Fatalf("expected file size <= 1024, got %d", info.Size())
	}
}

// failWriter fails every Write after an optional number of successful bytes.
type failWriter struct{ err error }

func (f failWriter) Write(p []byte) (int, error) { return 0, f.err }
func (f failWriter) Close() error                { return nil }

func TestWriteRecordsPropagatesWriteError(t *testing.T) {
	want := errors.New("disk full")
	err := writeRecords(failWriter{err: want}, []Record{{TS: 1}})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped %v, got %v", want, err)
	}
}

func TestCompactWriteFailureLeavesOriginal(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	// Two records, the old one outside retention so a rewrite is triggered.
	Append(dir, "p1", Record{TS: now.Add(-8 * 24 * time.Hour).Unix(), TokensPerSec: 1.0})
	Append(dir, "p1", Record{TS: now.Unix(), TokensPerSec: 2.0})

	p := filepath.Join(dir, "p1.jsonl")
	before, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}

	// Inject a writer that fails on every Write.
	orig := createWriter
	createWriter = func(string) (io.WriteCloser, error) { return failWriter{err: errors.New("boom")}, nil }
	defer func() { createWriter = orig }()

	if err := Compact(dir, "p1", 7*24*time.Hour, 5<<20); err == nil {
		t.Fatal("expected Compact to return the write error, got nil")
	}

	after, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("original file should be untouched: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("original file was modified:\nbefore=%q\nafter=%q", before, after)
	}
	if _, err := os.Stat(p + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file should be removed, stat err=%v", err)
	}
}
