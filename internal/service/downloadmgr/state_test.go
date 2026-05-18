package downloadmgr

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStatus_RoundTripJSON(t *testing.T) {
	cases := []Status{
		StatusQueued, StatusActive, StatusCompleted,
		StatusFailed, StatusCancelled, StatusAbandoned,
	}
	for _, want := range cases {
		got := ParseStatus(want.String())
		if got != want {
			t.Errorf("round-trip %v: got %v", want, got)
		}
	}
	if ParseStatus("bogus") != StatusFailed {
		t.Error("unknown status should fall back to StatusFailed")
	}
}

func TestStatus_IsTerminal(t *testing.T) {
	if StatusQueued.IsTerminal() || StatusActive.IsTerminal() {
		t.Error("queued/active must not be terminal")
	}
	for _, s := range []Status{StatusCompleted, StatusFailed, StatusCancelled, StatusAbandoned} {
		if !s.IsTerminal() {
			t.Errorf("%v expected terminal", s)
		}
	}
}

func TestSaveLoadRecord_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	id := NewID()
	r := DownloadRecord{
		ID:        id,
		PID:       1234,
		RepoID:    "user/repo",
		Filename:  "model.gguf",
		URL:       "https://example.com/model.gguf",
		DestFile:  "/tmp/model.gguf",
		Status:    StatusActive,
		Bytes:     512,
		Total:     2048,
		StartedAt: time.Unix(1700_000_000, 0).UTC(),
	}
	if err := SaveRecord(dir, r); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	got, err := LoadRecord(StatePath(dir, id))
	if err != nil {
		t.Fatalf("LoadRecord: %v", err)
	}
	if got.ID != r.ID || got.PID != r.PID || got.Bytes != r.Bytes ||
		got.Total != r.Total || got.Status != r.Status || got.URL != r.URL {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Error("SaveRecord must stamp UpdatedAt")
	}
}

func TestListRecords_MissingDirReturnsNil(t *testing.T) {
	out, err := ListRecords(filepath.Join(t.TempDir(), "nope"))
	if err != nil || out != nil {
		t.Errorf("missing dir: out=%v err=%v", out, err)
	}
}

func TestListRecords_FiltersAndIgnoresMalformed(t *testing.T) {
	dir := t.TempDir()
	if err := SaveRecord(dir, DownloadRecord{ID: "abc", URL: "u", DestFile: "/x", Status: StatusActive}); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	// noise files that must be skipped
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dl-bad.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ListRecords(dir)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(got) != 1 || got[0].ID != "abc" {
		t.Errorf("expected only the valid record; got %+v", got)
	}
}

func TestDeleteRecord_MissingIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := DeleteRecord(dir, "nope"); err != nil {
		t.Errorf("missing record should not error; got %v", err)
	}
}

func TestDeleteRecord_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	id := ID("xyz")
	if err := SaveRecord(dir, DownloadRecord{ID: id, URL: "u", DestFile: "/x"}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteRecord(dir, id); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
	if _, err := os.Stat(StatePath(dir, id)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("file still present after delete: err=%v", err)
	}
}

func TestRecordFromSpec_DefaultsToQueued(t *testing.T) {
	r := RecordFromSpec("z", Spec{URL: "u", DestFile: "/x"})
	if r.Status != StatusQueued {
		t.Errorf("Status=%v, want StatusQueued", r.Status)
	}
	if r.StartedAt.IsZero() || r.UpdatedAt.IsZero() {
		t.Error("StartedAt/UpdatedAt must be stamped")
	}
}
