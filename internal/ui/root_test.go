package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/ui/pages"
)

func TestRoot_StartsOnProfilesAndQuitsOnQ(t *testing.T) {
	tm := teatest.NewTestModel(t, NewRoot(TabProfiles), teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Profiles")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if err := tm.Quit(); err != nil {
		t.Fatalf("Quit returned err: %v", err)
	}
}

func TestRoot_TabSwitchByNumber(t *testing.T) {
	tm := teatest.NewTestModel(t, NewRoot(TabProfiles), teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Server")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if err := tm.Quit(); err != nil {
		t.Fatalf("Quit returned err: %v", err)
	}
}

func TestRoot_NumberOneSwitchesToProfiles(t *testing.T) {
	tm := teatest.NewTestModel(t, NewRoot(TabProfiles), teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "1 Profiles")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if err := tm.Quit(); err != nil {
		t.Fatalf("Quit returned err: %v", err)
	}
}

func TestRoot_NumberTwoSwitchesToServer(t *testing.T) {
	tm := teatest.NewTestModel(t, NewRoot(TabProfiles), teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "2 Server")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if err := tm.Quit(); err != nil {
		t.Fatalf("Quit returned err: %v", err)
	}
}

func TestRoot_UseInNewProfileSwitchesTab(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	root := NewRoot(TabModels).
		WithProfilesPage(pages.NewProfilesPage(store, domain.FlagSchema{}))

	updated, _ := root.Update(pages.UseInNewProfileMsg{Path: "/x.gguf"})
	r := updated.(RootModel)

	if r.active != TabProfiles {
		t.Errorf("active = %d, want TabProfiles=%d", r.active, TabProfiles)
	}
}

func TestRoot_TabSwitchToProfilesShowsPage(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(domain.Profile{
		ID: "alpha", Name: "AlphaProfile", Model: "/m.gguf",
		Args: map[string]any{"port": float64(8080)},
	}); err != nil {
		t.Fatal(err)
	}

	root := NewRoot(TabProfiles).
		WithProfilesPage(pages.NewProfilesPage(store, domain.FlagSchema{}))

	tm := teatest.NewTestModel(t, root, teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "AlphaProfile")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	_ = tm.Quit()
}

func TestRoot_WithServerPageReplacesPlaceholder(t *testing.T) {
	root := NewRoot(TabServer).WithServerPage(pages.Placeholder{TabName: "MONITOR_REPLACED"})

	tm := teatest.NewTestModel(t, root, teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "MONITOR_REPLACED")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	_ = tm.Quit()
}

func TestRoot_RoutesSwitchToServerMsg(t *testing.T) {
	root := NewRoot(TabProfiles).WithServerPage(pages.Placeholder{TabName: "MONITOR"})

	updated, _ := root.Update(pages.SwitchToServerMsg{PID: 999})
	r := updated.(RootModel)

	if r.active != TabServer {
		t.Fatalf("active = %d, want TabServer=%d", r.active, TabServer)
	}
}

func TestRoot_ForwardsSwitchPIDToServer(t *testing.T) {
	rec := &recordingServer{}
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(rec).
		WithModelsPage(pages.Placeholder{TabName: "M"})
	updated, _ := r.Update(pages.SwitchToServerMsg{PID: 4321})
	rm := updated.(RootModel)
	if rm.active != TabServer {
		t.Errorf("active = %v, want TabServer", rm.active)
	}
	if rec.lastSelectPID != 4321 {
		t.Errorf("rec.lastSelectPID = %d, want 4321", rec.lastSelectPID)
	}
}

func TestRoot_BootBlockerRendersModal(t *testing.T) {
	r := NewRoot(TabProfiles).WithBootBlocker("llama-server not found", "Install with: pacman -S llama.cpp-cuda")
	view := r.View()
	if !strings.Contains(view, "llama-server not found") {
		t.Errorf("missing title in view")
	}
	if !strings.Contains(view, "pacman -S") {
		t.Errorf("missing install hint in view")
	}
}

func TestRoot_BootBlockerSwallowsKeysExceptQuit(t *testing.T) {
	r := NewRoot(TabProfiles).WithBootBlocker("err", "fix")
	// Pressing 1 (tab switch) should NOT change active tab.
	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	rm := updated.(RootModel)
	if rm.active != TabProfiles {
		t.Errorf("active changed despite blocker; got %v", rm.active)
	}
	// Pressing q must still quit (tea.Quit cmd).
	_, cmd := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Errorf("q must still produce tea.Quit when blocker is open")
	}
}

func TestRoot_HelpToggle(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})
	// Help closed by default.
	if rendered := r.View(); strings.Contains(rendered, "Keybindings") {
		t.Error("help is open before any keypress")
	}
	// Press ?
	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	rm := updated.(RootModel)
	if rendered := rm.View(); !strings.Contains(rendered, "Keybindings") {
		t.Errorf("help did not open after ?; view:\n%s", rendered)
	}
	// Press Esc
	updated, _ = rm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	rm = updated.(RootModel)
	if rendered := rm.View(); strings.Contains(rendered, "Keybindings") {
		t.Errorf("help did not close on Esc; view:\n%s", rendered)
	}
}

func TestRoot_HelpModalShowsPageContext(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(hintingPage{name: "P"}).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})
	r.width = 120
	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	rm := updated.(RootModel)
	view := rm.View()
	if !strings.Contains(view, "[x] do-x") {
		t.Errorf("help modal missing page context; view:\n%s", view)
	}
}

func TestRoot_HelpModalSwitchesContextOnTabChange(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(hintingPage{name: "P"}).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})
	r.width = 120
	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	rm := updated.(RootModel)
	if !strings.Contains(rm.View(), "[x] do-x") {
		t.Fatal("help missing profiles context")
	}
	updated, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	rm = updated.(RootModel)
	view := rm.View()
	if !strings.Contains(view, "[x] do-x") {
		t.Errorf("help missing launcher context after tab switch; view:\n%s", view)
	}
}

func TestRoot_HelpSwallowsTabSwitch(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})
	// Open help.
	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	rm := updated.(RootModel)
	// Press 2 — should NOT switch tab while help is open.
	updated, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	rm = updated.(RootModel)
	if rm.active != TabProfiles {
		t.Errorf("active = %v; want still TabProfiles", rm.active)
	}
}

// capturingPage is a test double that announces it owns Tab/Shift+Tab.
type capturingPage struct {
	captured bool
	keys     []string
}

func (c *capturingPage) Init() tea.Cmd { return nil }
func (c *capturingPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		c.keys = append(c.keys, k.String())
	}
	return c, nil
}
func (c *capturingPage) View() string           { return "captured" }
func (c *capturingPage) IsCapturingInput() bool { return c.captured }

func TestRoot_TabPassesThroughWhenPageCapturesInput(t *testing.T) {
	cap := &capturingPage{captured: true}
	r := NewRoot(TabProfiles).
		WithProfilesPage(cap).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyTab})
	rm := updated.(RootModel)

	if rm.active != TabProfiles {
		t.Errorf("active = %v, want still TabProfiles", rm.active)
	}
	if len(cap.keys) != 1 || cap.keys[0] != "tab" {
		t.Errorf("page did not receive Tab; keys=%v", cap.keys)
	}
}

func TestRoot_QSwallowedWhilePageCapturesInput(t *testing.T) {
	cap := &capturingPage{captured: true}
	r := NewRoot(TabProfiles).
		WithProfilesPage(cap).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})

	_, cmd := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd != nil {
		t.Errorf("q produced cmd while page captures input; want nil (key forwarded)")
	}
	if len(cap.keys) != 1 || cap.keys[0] != "q" {
		t.Errorf("page did not receive q; keys=%v", cap.keys)
	}
}

func TestRoot_NumberKeySwallowedWhilePageCapturesInput(t *testing.T) {
	cap := &capturingPage{captured: true}
	r := NewRoot(TabProfiles).
		WithProfilesPage(cap).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	rm := updated.(RootModel)
	if rm.active != TabProfiles {
		t.Errorf("active = %v, want still TabProfiles (page captures input)", rm.active)
	}
	if len(cap.keys) != 1 || cap.keys[0] != "2" {
		t.Errorf("page did not receive '2'; keys=%v", cap.keys)
	}
}

func TestRoot_QuestionMarkSwallowedWhilePageCapturesInput(t *testing.T) {
	cap := &capturingPage{captured: true}
	r := NewRoot(TabProfiles).
		WithProfilesPage(cap).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	rm := updated.(RootModel)
	if rm.helpOpen {
		t.Errorf("help opened despite page capturing input")
	}
	if len(cap.keys) != 1 || cap.keys[0] != "?" {
		t.Errorf("page did not receive '?'; keys=%v", cap.keys)
	}
}

func TestRoot_CtrlCAlwaysQuitsEvenWhenCapturingInput(t *testing.T) {
	cap := &capturingPage{captured: true}
	r := NewRoot(TabProfiles).
		WithProfilesPage(cap).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})

	_, cmd := r.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Errorf("ctrl+c must always quit; got nil cmd")
	}
}

func TestRoot_TabSwitchesWhenPageDoesNotCapture(t *testing.T) {
	cap := &capturingPage{captured: false}
	r := NewRoot(TabProfiles).
		WithProfilesPage(cap).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyTab})
	rm := updated.(RootModel)

	if rm.active != TabServer {
		t.Errorf("active = %v, want TabServer", rm.active)
	}
}

// TestRoot_EscSwallowedWhenPageDoesNotCaptureInput verifies the Esc gate added
// in T7: when the active page does NOT capture input, Esc must be swallowed
// at the root (no tea.Quit, no tab change) instead of bubbling to a page that
// might quit or take a destructive action. ctrl+c remains the only escape.
func TestRoot_EscSwallowedWhenPageDoesNotCaptureInput(t *testing.T) {
	cap := &capturingPage{captured: false}
	r := NewRoot(TabProfiles).
		WithProfilesPage(cap).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})

	_, cmd := r.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Errorf("Esc produced cmd while page does not capture input; want nil (swallowed)")
	}
	if len(cap.keys) != 0 {
		t.Errorf("page received Esc while it does not capture input; keys=%v", cap.keys)
	}
}

// TestRoot_EscFromBackendsTabDoesNotQuit ensures Esc on the Backends tab
// (when nothing is capturing input) does not produce tea.Quit. Bug F-01:
// the validator observed Esc terminating the app from Profiles/Backends.
func TestRoot_EscFromBackendsTabDoesNotQuit(t *testing.T) {
	r := NewRoot(TabBackends).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"}).
		WithBackendsPage(pages.Placeholder{TabName: "B"})

	_, cmd := r.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Errorf("Esc on Backends tab produced cmd; want nil (no quit)")
	}
}

// F-12 regression: with a real terminal size (height > 0), the help
// modal title must announce the scroll keys so users know the content
// is scrollable.
func TestRoot_HelpModalAnnouncesScrollKeysWhenSized(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})
	r.width = 120
	r.height = 30
	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	rm := updated.(RootModel)
	view := rm.View()
	for _, want := range []string{"PgUp", "PgDn", "scroll"} {
		if !strings.Contains(view, want) {
			t.Errorf("help title missing %q; view:\n%s", want, view)
		}
	}
}

// F-12 regression: pressing Down/PgDown after `?` must move the
// viewport's scroll offset so long help content becomes reachable.
func TestRoot_HelpModalScrollsOnDownKey(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})
	r.width = 120
	r.height = 30
	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	rm := updated.(RootModel)
	_ = rm.View()

	if got := rm.helpViewport.YOffset; got != 0 {
		t.Fatalf("initial YOffset=%d; want 0", got)
	}
	updated, _ = rm.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	rm = updated.(RootModel)
	if got := rm.helpViewport.YOffset; got == 0 {
		t.Errorf("YOffset still 0 after PgDown; viewport did not scroll")
	}
}

// F-12 regression: `j` (vim-style) also drives the help viewport.
func TestRoot_HelpModalScrollsOnJKey(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})
	r.width = 120
	r.height = 30
	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	rm := updated.(RootModel)
	_ = rm.View()

	for i := 0; i < 30; i++ {
		updated, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		rm = updated.(RootModel)
	}
	if rm.helpViewport.YOffset == 0 {
		t.Errorf("YOffset still 0 after 30 j presses; viewport did not scroll")
	}
}

// TestRoot_EscForwardedWhenPageCapturesInput ensures Esc reaches the page
// when it owns input (e.g. confirm dialog, picker overlay). The Esc gate
// must only swallow when activePageCapturesInput() returns false.
func TestRoot_EscForwardedWhenPageCapturesInput(t *testing.T) {
	cap := &capturingPage{captured: true}
	r := NewRoot(TabProfiles).
		WithProfilesPage(cap).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})

	_, _ = r.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if len(cap.keys) != 1 || cap.keys[0] != "esc" {
		t.Errorf("page did not receive esc while capturing input; keys=%v", cap.keys)
	}
}

func TestRoot_ModelsFilterDoesNotLeakQ(t *testing.T) {
	cap := &capturingPage{captured: true}
	r := NewRoot(TabModels).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(cap)

	_, cmd := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd != nil {
		t.Errorf("q produced cmd while models filter captures input; want nil (key forwarded)")
	}
	if len(cap.keys) != 1 || cap.keys[0] != "q" {
		t.Errorf("page did not receive q; keys=%v", cap.keys)
	}
}

func TestRoot_ModelsFilterDoesNotLeakQuestionMark(t *testing.T) {
	cap := &capturingPage{captured: true}
	r := NewRoot(TabModels).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(cap)

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	rm := updated.(RootModel)
	if rm.helpOpen {
		t.Errorf("help opened despite models filter capturing input")
	}
	if len(cap.keys) != 1 || cap.keys[0] != "?" {
		t.Errorf("page did not receive ?; keys=%v", cap.keys)
	}
}

func TestRoot_ModelsFilterDoesNotLeakNumberKeys(t *testing.T) {
	cap := &capturingPage{captured: true}
	r := NewRoot(TabModels).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(cap)

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	rm := updated.(RootModel)
	if rm.active != TabModels {
		t.Errorf("active = %v, want still TabModels (filter captures input)", rm.active)
	}
	if len(cap.keys) != 1 || cap.keys[0] != "1" {
		t.Errorf("page did not receive '1'; keys=%v", cap.keys)
	}
}

func TestRoot_RoutesLaunchProfileMsg(t *testing.T) {
	dir := t.TempDir()
	store, _ := profilestore.NewFSStore(dir)
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.NewProfilesPage(store, domain.FlagSchema{}))

	updated, _ := r.Update(pages.LaunchProfileMsg{ID: "any"})
	rm := updated.(RootModel)
	if rm.active != TabProfiles {
		t.Errorf("active = %v, want TabProfiles", rm.active)
	}
}

func TestRoot_TabStripContainsSeparator(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})
	r.width = 120
	if !strings.Contains(r.View(), "│") {
		t.Errorf("tab strip missing │ separator; view:\n%s", r.View())
	}
}

func TestRoot_StatusBarMentionsHelp(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})
	r.width = 120
	view := r.View()
	if !strings.Contains(view, "[?] help") {
		t.Errorf("status bar missing [?] help; view:\n%s", view)
	}
}

// hintingPage is a tea.Model that publishes a known Hints() string.
type hintingPage struct{ name string }

func (h hintingPage) Init() tea.Cmd                       { return nil }
func (h hintingPage) Update(tea.Msg) (tea.Model, tea.Cmd) { return h, nil }
func (h hintingPage) View() string                        { return h.name }
func (h hintingPage) Hints() string                       { return "[x] do-x  [y] do-y" }

type captureHintingPage struct{ hintingPage }

func (captureHintingPage) IsCapturingInput() bool { return true }

func TestRoot_StatusBarIncludesActivePageHints(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(hintingPage{name: "P"})
	r.width = 120
	r.recomputeHints()

	view := r.View()
	if !strings.Contains(view, "[x] do-x") {
		t.Errorf("active page hints not in status bar; view:\n%s", view)
	}
	if !strings.Contains(view, "[?] help") {
		t.Errorf("global help token missing; view:\n%s", view)
	}

	// Switch tabs — placeholder Server does not implement HintProvider, so
	// only globalHints should remain.
	updated, _ := r.activate(TabServer)
	rl := updated.(RootModel)
	rl.width = 120
	view = rl.View()
	if strings.Contains(view, "[x] do-x") {
		t.Errorf("stale page hints still in status bar after tab switch; view:\n%s", view)
	}
	if !strings.Contains(view, "[?] help") {
		t.Errorf("global help token missing after tab switch; view:\n%s", view)
	}
}

func TestRoot_StatusBarSuppressesGlobalHintsDuringCapture(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(captureHintingPage{hintingPage{name: "P"}})
	r.width = 120
	r.recomputeHints()

	view := r.View()
	for _, want := range []string{"[ctrl+c] quit", "[x] do-x"} {
		if !strings.Contains(view, want) {
			t.Errorf("capture status bar missing %q; view:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"[1-5] tabs", "[?] help"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("capture status bar advertises inert %q; view:\n%s", unwanted, view)
		}
	}
}

func TestRoot_NumberFourSwitchesToModels(t *testing.T) {
	tm := teatest.NewTestModel(t, NewRoot(TabProfiles), teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Models")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if err := tm.Quit(); err != nil {
		t.Fatalf("Quit returned err: %v", err)
	}
}

func TestRoot_BackendsTabSwitchByNumber(t *testing.T) {
	tm := teatest.NewTestModel(t, NewRoot(TabProfiles), teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Backends")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if err := tm.Quit(); err != nil {
		t.Fatalf("Quit returned err: %v", err)
	}
}

func TestRoot_TabCyclesForwardThroughAllTabs(t *testing.T) {
	r := NewRoot(TabModels)
	want := []Tab{TabBackends, TabBenchmark, TabProfiles, TabServer, TabModels}
	var m tea.Model = r
	for i, expected := range want {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = updated
		if rm := m.(RootModel); rm.active != expected {
			t.Fatalf("after %d Tab presses active = %v, want %v", i+1, rm.active, expected)
		}
	}
}

func TestRoot_ShiftTabFromProfilesWrapsToBenchmark(t *testing.T) {
	r := NewRoot(TabProfiles)
	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if rm := updated.(RootModel); rm.active != TabBenchmark {
		t.Fatalf("active = %v, want TabBenchmark (wrap-around)", rm.active)
	}
}

func TestRoot_WithBackendsPageReplacesPlaceholder(t *testing.T) {
	root := NewRoot(TabBackends).WithBackendsPage(pages.Placeholder{TabName: "BACKENDS_REPLACED"})

	tm := teatest.NewTestModel(t, root, teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "BACKENDS_REPLACED")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	_ = tm.Quit()
}

func TestRoot_NumberFiveSwallowedWhilePageCapturesInput(t *testing.T) {
	cap := &capturingPage{captured: true}
	r := NewRoot(TabBackends).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"}).
		WithBackendsPage(cap)

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	rm := updated.(RootModel)
	if rm.active != TabBackends {
		t.Errorf("active = %v, want still TabBackends (page captures input)", rm.active)
	}
	if len(cap.keys) != 1 || cap.keys[0] != "5" {
		t.Errorf("page did not receive '5'; keys=%v", cap.keys)
	}
}

func TestRoot_NumberThreeSwallowedWhilePageCapturesInput(t *testing.T) {
	cap := &capturingPage{captured: true}
	r := NewRoot(TabServer).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(cap).
		WithModelsPage(pages.Placeholder{TabName: "Md"}).
		WithBackendsPage(pages.Placeholder{TabName: "B"})

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	rm := updated.(RootModel)
	if rm.active != TabServer {
		t.Errorf("active = %v, want still TabServer (page captures input)", rm.active)
	}
	if len(cap.keys) != 1 || cap.keys[0] != "3" {
		t.Errorf("page did not receive '3'; keys=%v", cap.keys)
	}
}

type recordingServer struct {
	lastSelectPID int
}

func (r *recordingServer) Init() tea.Cmd { return nil }
func (r *recordingServer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(pages.ServerSelectPIDMsg); ok {
		r.lastSelectPID = m.PID
	}
	return r, nil
}
func (r *recordingServer) View() string { return "" }

func TestRoot_NumberFiveSwitchesToBenchmark(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(pages.Placeholder{TabName: "S"}).
		WithModelsPage(pages.Placeholder{TabName: "M"}).
		WithBackendsPage(pages.Placeholder{TabName: "B"}).
		WithBenchmarkPage(pages.Placeholder{TabName: "BENCHMARK_PAGE"})

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	rm := updated.(RootModel)
	if rm.active != TabBenchmark {
		t.Fatalf("active = %v, want TabBenchmark", rm.active)
	}
}

// UIUX-021: a TabAttentionMsg for a background tab badges it in the tab
// strip; visiting the tab clears the badge.
func TestRoot_TabAttentionBadgesInactiveTabUntilVisited(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(pages.Placeholder{TabName: "S"}).
		WithModelsPage(pages.Placeholder{TabName: "M"})
	r.width = 120

	updated, cmd := r.Update(pages.TabAttentionMsg{Page: pages.AttentionModels})
	rm := updated.(RootModel)
	if cmd != nil {
		t.Errorf("TabAttentionMsg produced cmd; root should consume it")
	}
	if !rm.badges[TabModels] {
		t.Fatal("Models badge not set after TabAttentionMsg")
	}
	if !strings.Contains(rm.View(), "3 Models ●") {
		t.Errorf("tab strip missing badge next to Models; view:\n%s", rm.View())
	}

	// Visiting the Models tab (key "3") clears the badge.
	updated, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	rm = updated.(RootModel)
	if rm.active != TabModels {
		t.Fatalf("active = %v, want TabModels", rm.active)
	}
	if rm.badges[TabModels] {
		t.Error("Models badge still set after visiting the tab")
	}
	if strings.Contains(rm.View(), "●") {
		t.Errorf("badge glyph still rendered after visit; view:\n%s", rm.View())
	}
}

// Attention for the tab the user is already looking at is noise — no badge.
func TestRoot_TabAttentionIgnoredForActiveTab(t *testing.T) {
	r := NewRoot(TabServer).
		WithServerPage(pages.Placeholder{TabName: "S"})
	updated, _ := r.Update(pages.TabAttentionMsg{Page: pages.AttentionServer})
	rm := updated.(RootModel)
	if rm.badges[TabServer] {
		t.Error("badge set for the active tab")
	}
}

// Cross-tab navigation paths (not just digit keys) must also clear badges.
func TestRoot_SwitchToServerMsgClearsServerBadge(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithServerPage(pages.Placeholder{TabName: "S"})
	r.badges[TabServer] = true

	updated, _ := r.Update(pages.SwitchToServerMsg{PID: 1})
	rm := updated.(RootModel)
	if rm.badges[TabServer] {
		t.Error("Server badge still set after SwitchToServerMsg navigation")
	}
}

// TUI-RESP: root renders a notice when the terminal is below MinTermWidth/Height.
func TestRoot_TerminalTooSmallNotice(t *testing.T) {
	r := NewRoot(TabProfiles)
	updated, _ := r.Update(tea.WindowSizeMsg{Width: 18, Height: 5})
	rm := updated.(RootModel)
	if !strings.Contains(rm.View(), "Terminal too small") {
		t.Fatalf("View() = %q, want Terminal too small", rm.View())
	}
	updated, _ = rm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	rm = updated.(RootModel)
	if strings.Contains(rm.View(), "Terminal too small") {
		t.Fatalf("View() at 80x24 should not show too-small notice")
	}
}
