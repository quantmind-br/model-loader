package components

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeFileLister struct {
	info *RepoInfo
	err  error
}

func (f *fakeFileLister) RepoInfo(ctx context.Context, repoID string) (*RepoInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.info, nil
}

func TestHFFilePicker_InitReturnsCmd(t *testing.T) {
	lister := &fakeFileLister{info: &RepoInfo{ID: "user/repo"}}
	p := NewHFFilePicker(lister, "user/repo", false, 80, 24)
	cmd := p.Init()
	if cmd == nil {
		t.Fatal("Init() must return a Cmd that fetches the file list")
	}
	if !p.IsActive() {
		t.Error("picker should be active after construction")
	}
	if !p.loading {
		t.Error("picker should start in loading state")
	}
}

func TestHFFilePicker_InitCmdEmitsFileList(t *testing.T) {
	lister := &fakeFileLister{
		info: &RepoInfo{
			ID: "user/repo",
			Siblings: []Sibling{
				{RFilename: "model.gguf", Size: 1024},
				{RFilename: "config.json", Size: 256},
			},
		},
	}
	p := NewHFFilePicker(lister, "user/repo", false, 80, 24)
	cmd := p.Init()
	msg := cmd()
	listMsg, ok := msg.(HFFileListMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want HFFileListMsg", msg)
	}
	if listMsg.Err != nil {
		t.Fatalf("unexpected error: %v", listMsg.Err)
	}
	if len(listMsg.Files) != 2 {
		t.Errorf("Files count = %d, want 2", len(listMsg.Files))
	}
}

func TestHFFilePicker_InitCmdSurfacesError(t *testing.T) {
	lister := &fakeFileLister{err: errors.New("network down")}
	p := NewHFFilePicker(lister, "user/repo", false, 80, 24)
	msg := p.Init()()
	listMsg, ok := msg.(HFFileListMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want HFFileListMsg", msg)
	}
	if listMsg.Err == nil {
		t.Error("expected error to be surfaced via HFFileListMsg.Err")
	}
}

func TestHFFilePicker_EscClosesPicker(t *testing.T) {
	p := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "user/repo", false, 80, 24)
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if p.IsActive() {
		t.Error("picker should not be active after esc")
	}
}

func TestHFFilePicker_SpaceTogglesSelection(t *testing.T) {
	p := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "user/repo", false, 80, 24)
	p.files = []FileItem{
		{RFilename: "a.gguf", Size: 10, Selected: false},
		{RFilename: "b.gguf", Size: 20, Selected: false},
	}
	p.cursor = 0

	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if !p.files[0].Selected {
		t.Error("first file should be selected after space")
	}
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if p.files[0].Selected {
		t.Error("first file should be unselected after second space")
	}
}

func TestHFFilePicker_SpaceIsNoOpInSnapshotMode(t *testing.T) {
	p := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "user/repo", true, 80, 24)
	p.files = []FileItem{{RFilename: "a.gguf", Size: 10, Selected: false}}
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if p.files[0].Selected {
		t.Error("snapshot mode must not toggle individual selection on space")
	}
}

func TestHFFilePicker_EnterClosesPicker(t *testing.T) {
	p := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "user/repo", false, 80, 24)
	p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if p.IsActive() {
		t.Error("picker should not be active after enter")
	}
}

func TestHFFilePicker_UpDownMovesCursor(t *testing.T) {
	p := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "user/repo", false, 80, 24)
	p.files = []FileItem{
		{RFilename: "a.gguf"}, {RFilename: "b.gguf"}, {RFilename: "c.gguf"},
	}

	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if p.cursor != 1 {
		t.Errorf("cursor after Down = %d, want 1", p.cursor)
	}
	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if p.cursor != 2 {
		t.Errorf("cursor should clamp at end; got %d, want 2", p.cursor)
	}
	p.Update(tea.KeyMsg{Type: tea.KeyUp})
	if p.cursor != 1 {
		t.Errorf("cursor after Up = %d, want 1", p.cursor)
	}
}

func TestHFFilePicker_HFFileListMsgFiltersToGGUFInPerFile(t *testing.T) {
	p := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "user/repo", false, 80, 24)
	p.Update(HFFileListMsg{
		Files: []FileItem{
			{RFilename: "model.gguf", Size: 1024},
			{RFilename: "tokenizer.json", Size: 128},
			{RFilename: "model.q4.gguf", Size: 512},
		},
	})
	if len(p.files) != 2 {
		t.Fatalf("expected 2 .gguf files retained, got %d", len(p.files))
	}
	for _, f := range p.files {
		if !strings.HasSuffix(f.RFilename, ".gguf") {
			t.Errorf("non-gguf file leaked through filter: %s", f.RFilename)
		}
	}
}

func TestHFFilePicker_HFFileListMsgKeepsAllInSnapshotAndPreselects(t *testing.T) {
	p := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "user/repo", true, 80, 24)
	p.Update(HFFileListMsg{
		Files: []FileItem{
			{RFilename: "model.gguf"},
			{RFilename: "config.json"},
		},
	})
	if len(p.files) != 2 {
		t.Fatalf("snapshot mode should retain all files; got %d", len(p.files))
	}
	for _, f := range p.files {
		if !f.Selected {
			t.Errorf("snapshot mode should pre-select all files; %s not selected", f.RFilename)
		}
	}
}

func TestHFFilePicker_SelectedFilesPerFileMode(t *testing.T) {
	p := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "user/repo", false, 80, 24)
	p.files = []FileItem{
		{RFilename: "a.gguf", Selected: true},
		{RFilename: "b.gguf", Selected: false},
		{RFilename: "c.gguf", Selected: true},
	}
	got := p.SelectedFiles()
	if len(got) != 2 {
		t.Fatalf("SelectedFiles = %d items, want 2", len(got))
	}
	if got[0] != "a.gguf" || got[1] != "c.gguf" {
		t.Errorf("SelectedFiles = %v, want [a.gguf c.gguf]", got)
	}
}

func TestHFFilePicker_SelectedFilesSnapshotMode(t *testing.T) {
	p := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "user/repo", true, 80, 24)
	p.files = []FileItem{
		{RFilename: "model.gguf"},
		{RFilename: "config.json"},
	}
	got := p.SelectedFiles()
	if len(got) != 2 {
		t.Fatalf("snapshot SelectedFiles = %d items, want 2", len(got))
	}
}

func TestHFFilePicker_ViewLoading(t *testing.T) {
	p := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "user/repo", false, 80, 24)
	if !strings.Contains(p.View(), "Loading") {
		t.Errorf("loading view missing 'Loading'; got %q", p.View())
	}
}

func TestHFFilePicker_ViewError(t *testing.T) {
	p := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "user/repo", false, 80, 24)
	p.Update(HFFileListMsg{Err: errors.New("boom")})
	if !strings.Contains(p.View(), "Error") || !strings.Contains(p.View(), "boom") {
		t.Errorf("error view missing 'Error: boom'; got %q", p.View())
	}
}

func TestHFFilePicker_ViewRendersFilesAndHelp(t *testing.T) {
	p := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "user/repo", false, 80, 24)
	p.loading = false
	p.files = []FileItem{{RFilename: "model.gguf", Size: 1024, Selected: true}}
	out := p.View()
	if !strings.Contains(out, "model.gguf") {
		t.Errorf("view missing file name; got:\n%s", out)
	}
	if !strings.Contains(out, "[x]") {
		t.Errorf("view missing checked marker; got:\n%s", out)
	}
	if !strings.Contains(out, "space") {
		t.Errorf("per-file mode view should mention 'space'; got:\n%s", out)
	}
}

func TestHFFilePicker_ViewSnapshotHidesSpaceHint(t *testing.T) {
	p := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "user/repo", true, 80, 24)
	p.loading = false
	p.files = []FileItem{{RFilename: "model.gguf", Selected: true}}
	out := p.View()
	if !strings.Contains(out, "Snapshot") {
		t.Errorf("snapshot view should mention 'Snapshot'; got:\n%s", out)
	}
	if strings.Contains(out, "space: toggle") {
		t.Errorf("snapshot view should not mention space-toggle help; got:\n%s", out)
	}
}

func TestHFFilePicker_IsSnapshotReflectsMode(t *testing.T) {
	per := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "x", false, 80, 24)
	if per.IsSnapshot() {
		t.Error("per-file picker should report IsSnapshot()=false")
	}
	snap := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "x", true, 80, 24)
	if !snap.IsSnapshot() {
		t.Error("snapshot picker should report IsSnapshot()=true")
	}
}

func TestHFFilePicker_SetSizeUpdatesDimensions(t *testing.T) {
	p := NewHFFilePicker(&fakeFileLister{info: &RepoInfo{}}, "x", false, 80, 24)
	p.SetSize(120, 40)
	if p.width != 120 || p.height != 40 {
		t.Errorf("size = (%d,%d), want (120,40)", p.width, p.height)
	}
}
