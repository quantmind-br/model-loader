package pages

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

func TestWebEditMsgs_OpenAndResult(t *testing.T) {
	started := webEditStartedMsg{url: "http://127.0.0.1:1234"}
	if started.url == "" {
		t.Fatal("url should be set")
	}
	done := webEditDoneMsg{saved: true, profileID: "qwen"}
	if !done.saved || done.profileID != "qwen" {
		t.Fatalf("done msg wrong: %+v", done)
	}
}

// TestProfilesPage_WebEditingIsCapturingInput asserts that a page with
// webEditing=true reports IsCapturingInput()==true so the global shortcut
// gate forwards keys to the page rather than consuming them.
func TestProfilesPage_WebEditingIsCapturingInput(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})
	page.webEditing = true

	if !page.IsCapturingInput() {
		t.Error("IsCapturingInput() = false when webEditing=true, want true")
	}
}

// TestProfilesPage_WebEditDoneMsgSavedClearsState verifies that receiving a
// webEditDoneMsg with saved=true clears webEditing, clears webSession,
// flashes "saved <id>", and returns a non-nil Batch cmd (loadCmd+flashCmd).
func TestProfilesPage_WebEditDoneMsgSavedClearsState(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})
	page.webEditing = true

	updated, cmd := page.Update(webEditDoneMsg{saved: true, profileID: "my-profile"})
	page = updated.(ProfilesPage)

	if page.webEditing {
		t.Error("webEditing should be false after webEditDoneMsg")
	}
	if page.webSession != nil {
		t.Error("webSession should be nil after webEditDoneMsg")
	}
	if cmd == nil {
		t.Error("expected non-nil cmd (load+flash) after webEditDoneMsg saved=true")
	}
	if page.flash.Message() != "saved my-profile" {
		t.Errorf("flash = %q, want %q", page.flash.Message(), "saved my-profile")
	}
}

// TestProfilesPage_WebEditDoneMsgNotSavedClearsState verifies that a
// not-saved done (user cancelled from browser) clears webEditing without
// flashing success.
func TestProfilesPage_WebEditDoneMsgNotSavedClearsState(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})
	page.webEditing = true

	updated, _ := page.Update(webEditDoneMsg{saved: false, profileID: ""})
	page = updated.(ProfilesPage)

	if page.webEditing {
		t.Error("webEditing should be false after webEditDoneMsg not saved")
	}
	if page.flash.Message() != "" {
		t.Errorf("unexpected flash on cancel: %q", page.flash.Message())
	}
}

// TestProfilesPage_WebEditKeySwallowedWhileEditing verifies that printable
// keys are swallowed (nil cmd, state unchanged) while webEditing=true, so
// the global shortcut gate never sees them.
func TestProfilesPage_WebEditKeySwallowedWhileEditing(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	page := NewProfilesPage(store, domain.FlagSchema{})
	page.webEditing = true

	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'n'}},
		{Type: tea.KeyRunes, Runes: []rune{'e'}},
		{Type: tea.KeyRunes, Runes: []rune{'x'}},
	} {
		updated, cmd := page.Update(key)
		p := updated.(ProfilesPage)
		if cmd != nil {
			t.Errorf("key %q: expected nil cmd while webEditing, got non-nil", key.String())
		}
		if !p.webEditing {
			t.Errorf("key %q: webEditing cleared unexpectedly", key.String())
		}
	}
}
