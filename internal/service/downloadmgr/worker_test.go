package downloadmgr

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunWorker_SuccessfulDownload(t *testing.T) {
	body := strings.Repeat("x", 64*1024) // 64KB so we cross multiple Read calls
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			t.Errorf("first request must not send Range; got %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "model.gguf")
	id := ID("test-success")
	statePath := StatePath(dir, id)
	rec := DownloadRecord{ID: id, URL: srv.URL, DestFile: dest, Status: StatusQueued}
	if err := SaveRecord(dir, rec); err != nil {
		t.Fatal(err)
	}

	code := RunWorker(WorkerConfig{StatePath: statePath, Client: srv.Client(), CheckpointBytes: 4 * 1024})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	got, err := LoadRecord(statePath)
	if err != nil {
		t.Fatalf("LoadRecord: %v", err)
	}
	if got.Status != StatusCompleted {
		t.Errorf("Status = %v, want completed", got.Status)
	}
	if got.Bytes != int64(len(body)) {
		t.Errorf("Bytes = %d, want %d", got.Bytes, len(body))
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != body {
		t.Error("file content mismatch")
	}
	if _, err := os.Stat(dest + ".partial"); !os.IsNotExist(err) {
		t.Errorf(".partial must be renamed away after success; err=%v", err)
	}
}

func TestRunWorker_ResumeFromPartial(t *testing.T) {
	full := strings.Repeat("y", 8*1024)
	preExisting := full[:1024] // first 1 KiB already on disk

	var gotRange string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.Header.Get("Range")
		if gotRange == "" {
			t.Errorf("expected Range header on resume")
		}
		// Reply 206 with the remaining bytes.
		w.Header().Set("Content-Range", "bytes 1024-"+strconv.Itoa(len(full)-1)+"/"+strconv.Itoa(len(full)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte(full[1024:]))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "resume.bin")
	id := ID("test-resume")
	statePath := StatePath(dir, id)

	// Seed the partial file as if a prior worker had written 1 KiB.
	if err := os.WriteFile(dest+".partial", []byte(preExisting), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := DownloadRecord{ID: id, URL: srv.URL, DestFile: dest, Status: StatusAbandoned, Bytes: 1024}
	if err := SaveRecord(dir, rec); err != nil {
		t.Fatal(err)
	}

	code := RunWorker(WorkerConfig{StatePath: statePath, Client: srv.Client()})
	if code != 0 {
		t.Errorf("exit code = %d", code)
	}
	if !strings.HasPrefix(gotRange, "bytes=1024-") {
		t.Errorf("Range header = %q, want bytes=1024-...", gotRange)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != full {
		t.Errorf("final file should contain prior + appended bytes")
	}
}

func TestRunWorker_ResumeButServer200_RestartsFromZero(t *testing.T) {
	body := strings.Repeat("z", 4*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ignore Range; reply 200 with full body.
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "restart.bin")
	id := ID("test-restart")
	statePath := StatePath(dir, id)
	// Pretend we had 100 wrong bytes from a previous run.
	if err := os.WriteFile(dest+".partial", []byte(strings.Repeat("?", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveRecord(dir, DownloadRecord{ID: id, URL: srv.URL, DestFile: dest, Bytes: 100, Status: StatusAbandoned}); err != nil {
		t.Fatal(err)
	}

	code := RunWorker(WorkerConfig{StatePath: statePath, Client: srv.Client()})
	if code != 0 {
		t.Errorf("exit code = %d", code)
	}
	got, _ := LoadRecord(statePath)
	if got.Bytes != int64(len(body)) {
		t.Errorf("Bytes = %d, want %d (full body)", got.Bytes, len(body))
	}
	data, _ := os.ReadFile(dest)
	if string(data) != body {
		t.Errorf("file should be overwritten with full body, got %d bytes", len(data))
	}
}

func TestRunWorker_HTTPErrorRecordsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "boom.bin")
	id := ID("test-fail")
	statePath := StatePath(dir, id)
	if err := SaveRecord(dir, DownloadRecord{ID: id, URL: srv.URL, DestFile: dest, Status: StatusQueued}); err != nil {
		t.Fatal(err)
	}

	code := RunWorker(WorkerConfig{StatePath: statePath, Client: srv.Client()})
	if code != 0 {
		t.Errorf("exit code = %d (should still be 0 — status is in record)", code)
	}
	got, _ := LoadRecord(statePath)
	if got.Status != StatusFailed {
		t.Errorf("Status = %v, want StatusFailed", got.Status)
	}
	if !strings.Contains(got.Err, "500") {
		t.Errorf("Err = %q, want substring '500'", got.Err)
	}
}

func TestRunWorker_BadStateFile(t *testing.T) {
	code := RunWorker(WorkerConfig{StatePath: "/does/not/exist"})
	if code == 0 {
		t.Error("missing state file should exit non-zero")
	}
}

func TestTotalFromContentRange(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"bytes 0-99/100", 100},
		{"bytes 1024-2047/4096", 4096},
		{"bytes 0-99/*", 0},
		{"", 0},
		{"junk", 0},
		{"bytes 0-99", 0},
	}
	for _, c := range cases {
		if got := totalFromContentRange(c.in); got != c.want {
			t.Errorf("%q: got %d, want %d", c.in, got, c.want)
		}
	}
}
