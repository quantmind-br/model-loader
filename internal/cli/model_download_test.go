package cli

import (
	"bytes"
	"encoding/json"
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
	if spec.DestFile != filepath.Join(root, "org", "model", "model.gguf") {
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
	want := filepath.Join(root, "org", "model", "file.bin")
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
	nested := filepath.Join(root, "org", "m")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "f.gguf"), []byte("x"), 0o644); err != nil {
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

func sampleStates() []downloadmgr.State {
	return []downloadmgr.State{
		{ID: "aaa111", Spec: downloadmgr.Spec{RepoID: "org/a", Filename: "a.gguf"}, Status: downloadmgr.StatusActive, Bytes: 512, Total: 1024},
		{ID: "bbb222", Spec: downloadmgr.Spec{RepoID: "org/b", Filename: "b.gguf"}, Status: downloadmgr.StatusCompleted, Bytes: 2048, Total: 2048},
	}
}

func TestListDownloads_Table(t *testing.T) {
	mgr := &fakeDLManager{snapshot: sampleStates()}
	var out bytes.Buffer
	if err := listDownloads(&out, mgr, false); err != nil {
		t.Fatalf("listDownloads: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "aaa111") || !strings.Contains(s, "active") || !strings.Contains(s, "completed") {
		t.Fatalf("table missing rows: %q", s)
	}
}

func TestListDownloads_Empty(t *testing.T) {
	var out bytes.Buffer
	if err := listDownloads(&out, &fakeDLManager{}, false); err != nil {
		t.Fatalf("listDownloads: %v", err)
	}
	if !strings.Contains(out.String(), "no downloads") {
		t.Fatalf("expected 'no downloads': %q", out.String())
	}
}

func TestResolveDownloadID_Prefix(t *testing.T) {
	mgr := &fakeDLManager{snapshot: sampleStates()}
	id, err := resolveDownloadID(mgr, "aaa")
	if err != nil || id != "aaa111" {
		t.Fatalf("prefix resolve: id=%q err=%v", id, err)
	}
	if _, err := resolveDownloadID(mgr, "zzz"); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestCancelDownload(t *testing.T) {
	mgr := &fakeDLManager{snapshot: sampleStates()}
	var out bytes.Buffer
	if err := cancelDownload(&out, mgr, "aaa111", false); err != nil {
		t.Fatalf("cancelDownload: %v", err)
	}
	if len(mgr.cancelled) != 1 || mgr.cancelled[0] != "aaa111" {
		t.Fatalf("cancel not recorded: %+v", mgr.cancelled)
	}
}

func TestResumeDownload_NotResumable(t *testing.T) {
	mgr := &fakeDLManager{snapshot: sampleStates(), resumeErr: downloadmgr.ErrNotResumable}
	var out bytes.Buffer
	err := resumeDownload(&out, mgr, "bbb222", false)
	if err == nil || !strings.Contains(err.Error(), "not resumable") {
		t.Fatalf("expected not-resumable error, got %v", err)
	}
}

func TestListDownloads_JSON(t *testing.T) {
	mgr := &fakeDLManager{snapshot: sampleStates()}
	var out bytes.Buffer
	if err := listDownloads(&out, mgr, true); err != nil {
		t.Fatalf("listDownloads json: %v", err)
	}
	var items []downloadItem
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if len(items) != 2 || items[0].ID != "aaa111" || items[0].Status != "active" {
		t.Fatalf("unexpected items: %+v", items)
	}
	var out2 bytes.Buffer
	if err := listDownloads(&out2, &fakeDLManager{}, true); err != nil {
		t.Fatalf("empty json: %v", err)
	}
	if strings.TrimSpace(out2.String()) != "[]" {
		t.Fatalf("empty json should be [], got %q", out2.String())
	}
}

func TestResolveDownloadID_Ambiguous(t *testing.T) {
	mgr := &fakeDLManager{snapshot: []downloadmgr.State{
		{ID: "aaa111"}, {ID: "aaa222"},
	}}
	_, err := resolveDownloadID(mgr, "aaa")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous error, got %v", err)
	}
	if !strings.Contains(err.Error(), "aaa111") || !strings.Contains(err.Error(), "aaa222") {
		t.Fatalf("ambiguous error should list candidate IDs, got: %v", err)
	}
	if !strings.Contains(err.Error(), "use a longer prefix") {
		t.Fatalf("ambiguous error should hint about longer prefix, got: %v", err)
	}
}

func TestCancelDownload_Error(t *testing.T) {
	mgr := &fakeDLManager{snapshot: sampleStates(), cancelErr: errors.New("boom")}
	var out bytes.Buffer
	err := cancelDownload(&out, mgr, "aaa111", false)
	if err == nil || !strings.Contains(err.Error(), "cancel:") {
		t.Fatalf("expected wrapped cancel error, got %v", err)
	}
}

func TestResumeDownload_Happy(t *testing.T) {
	mgr := &fakeDLManager{snapshot: sampleStates()}
	var out bytes.Buffer
	if err := resumeDownload(&out, mgr, "bbb222", false); err != nil {
		t.Fatalf("resumeDownload: %v", err)
	}
	if len(mgr.resumed) != 1 || mgr.resumed[0] != "bbb222" {
		t.Fatalf("resume not recorded: %+v", mgr.resumed)
	}
	if !strings.Contains(out.String(), "resuming") {
		t.Fatalf("expected resuming message: %q", out.String())
	}
}

func TestModelCommandTree(t *testing.T) {
	model := childByName(rootCmd, "model")
	if model == nil {
		t.Fatal("model command missing")
	}
	for _, name := range []string{"list", "search", "info", "download", "downloads"} {
		if childByName(model, name) == nil {
			t.Errorf("model subcommand missing: %s", name)
		}
	}
	downloads := childByName(model, "downloads")
	if downloads == nil {
		t.Fatal("downloads command missing")
	}
	for _, name := range []string{"cancel", "resume"} {
		if childByName(downloads, name) == nil {
			t.Errorf("downloads subcommand missing: %s", name)
		}
	}
}

func TestModelDownload_ArgValidation(t *testing.T) {
	// 'model download' with too few args must exit non-zero (cobra Args check),
	// without panicking or reaching the network.
	rootCmd.SetArgs([]string{"model", "download", "only-one-arg"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	if code := Execute(); code == 0 {
		t.Fatal("expected non-zero exit for missing filename arg")
	}
}
