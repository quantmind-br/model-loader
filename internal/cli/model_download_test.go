package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
)

// fakeDLManager implements downloadManager, recording calls and serving canned
// snapshots/events.
type fakeDLManager struct {
	started   []downloadmgr.Spec
	cancelled []downloadmgr.ID
	resumed   []downloadmgr.ID
	snapshot  []downloadmgr.State
	startErr  error
	cancelErr error
	resumeErr error
	events    chan downloadmgr.Event
	nextID    downloadmgr.ID
}

func (f *fakeDLManager) Start(spec downloadmgr.Spec) (downloadmgr.ID, error) {
	if f.startErr != nil {
		return "", f.startErr
	}
	f.started = append(f.started, spec)
	id := f.nextID
	if id == "" {
		id = "id-1"
	}
	return id, nil
}
func (f *fakeDLManager) Cancel(id downloadmgr.ID) error {
	f.cancelled = append(f.cancelled, id)
	return f.cancelErr
}
func (f *fakeDLManager) Resume(id downloadmgr.ID) error {
	f.resumed = append(f.resumed, id)
	return f.resumeErr
}
func (f *fakeDLManager) Snapshot() []downloadmgr.State { return f.snapshot }
func (f *fakeDLManager) Subscribe() <-chan downloadmgr.Event {
	if f.events == nil {
		f.events = make(chan downloadmgr.Event, 4)
	}
	return f.events
}
func (f *fakeDLManager) StartPolling() {}

// tempSearchPath returns an existing temp dir usable as a download root.
func tempSearchPath(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestStartDownload_SingleFile(t *testing.T) {
	root := tempSearchPath(t)
	mgr := &fakeDLManager{}
	var out bytes.Buffer
	err := startDownload(&out, mgr, &fakeHub{}, root, "org/model", "model.gguf", false, false)
	if err != nil {
		t.Fatalf("startDownload: %v", err)
	}
	if len(mgr.started) != 1 {
		t.Fatalf("expected 1 Start call, got %d", len(mgr.started))
	}
	spec := mgr.started[0]
	if spec.RepoID != "org/model" || spec.Filename != "model.gguf" {
		t.Fatalf("bad spec: %+v", spec)
	}
	if spec.DestFile != filepath.Join(root, "model.gguf") {
		t.Fatalf("bad destFile: %q", spec.DestFile)
	}
	if spec.URL != "https://hf/org/model/model.gguf" {
		t.Fatalf("bad url: %q", spec.URL)
	}
}

func TestStartDownload_Snapshot(t *testing.T) {
	root := tempSearchPath(t)
	mgr := &fakeDLManager{}
	var out bytes.Buffer
	if err := startDownload(&out, mgr, &fakeHub{}, root, "org/model", "file.bin", true, false); err != nil {
		t.Fatalf("startDownload: %v", err)
	}
	want := filepath.Join(root, "org__model", "file.bin")
	if mgr.started[0].DestFile != want {
		t.Fatalf("snapshot destFile = %q, want %q", mgr.started[0].DestFile, want)
	}
}

func TestStartDownload_NoSearchPath(t *testing.T) {
	var out bytes.Buffer
	if err := startDownload(&out, &fakeDLManager{}, &fakeHub{}, "", "org/m", "f.gguf", false, false); err == nil {
		t.Fatal("expected error when search path is empty")
	}
}

func TestStartDownload_AlreadyExists(t *testing.T) {
	root := tempSearchPath(t)
	if err := os.WriteFile(filepath.Join(root, "f.gguf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := startDownload(&out, &fakeDLManager{}, &fakeHub{}, root, "org/m", "f.gguf", false, false); err == nil {
		t.Fatal("expected error when destination already exists")
	}
}

func TestStartDownload_WaitCompletes(t *testing.T) {
	root := tempSearchPath(t)
	mgr := &fakeDLManager{nextID: "dl-7", events: make(chan downloadmgr.Event, 4)}
	mgr.events <- downloadmgr.Event{ID: "dl-7", State: downloadmgr.State{Status: downloadmgr.StatusCompleted}}
	var out bytes.Buffer
	if err := startDownload(&out, mgr, &fakeHub{}, root, "org/m", "f.gguf", false, true); err != nil {
		t.Fatalf("startDownload wait: %v", err)
	}
	if !strings.Contains(out.String(), "completed") {
		t.Fatalf("expected completed message: %q", out.String())
	}
}

func TestStartDownload_WaitFails(t *testing.T) {
	root := tempSearchPath(t)
	mgr := &fakeDLManager{nextID: "dl-8", events: make(chan downloadmgr.Event, 4)}
	mgr.events <- downloadmgr.Event{ID: "dl-8", State: downloadmgr.State{Status: downloadmgr.StatusFailed, Err: errors.New("404 Not Found")}}
	var out bytes.Buffer
	err := startDownload(&out, mgr, &fakeHub{}, root, "org/m", "f.gguf", false, true)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected failure error including reason, got: %v", err)
	}
}
