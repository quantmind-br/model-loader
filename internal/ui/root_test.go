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
		return strings.Contains(string(out), "Monitor")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if err := tm.Quit(); err != nil {
		t.Fatalf("Quit returned err: %v", err)
	}
}

func TestRoot_NumberOneSwitchesToLauncher(t *testing.T) {
	tm := teatest.NewTestModel(t, NewRoot(TabProfiles), teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "1 Launcher")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	if err := tm.Quit(); err != nil {
		t.Fatalf("Quit returned err: %v", err)
	}
}

func TestRoot_NumberTwoSwitchesToProfiles(t *testing.T) {
	tm := teatest.NewTestModel(t, NewRoot(TabLauncher), teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "2 Profiles")
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

func TestRoot_TabSwitchToLauncherShowsPage(t *testing.T) {
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
		WithLauncherPage(pages.NewLauncherPage(store, nil, nil))

	tm := teatest.NewTestModel(t, root, teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "AlphaProfile")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	_ = tm.Quit()
}

func TestRoot_WithMonitorPageReplacesPlaceholder(t *testing.T) {
	root := NewRoot(TabMonitor).WithMonitorPage(pages.Placeholder{TabName: "MONITOR_REPLACED"})

	tm := teatest.NewTestModel(t, root, teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "MONITOR_REPLACED")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	_ = tm.Quit()
}

func TestRoot_RoutesSwitchToMonitorMsg(t *testing.T) {
	root := NewRoot(TabProfiles).WithMonitorPage(pages.Placeholder{TabName: "MONITOR"})

	updated, _ := root.Update(pages.SwitchToMonitorMsg{PID: 999})
	r := updated.(RootModel)

	if r.active != TabMonitor {
		t.Fatalf("active = %d, want TabMonitor=%d", r.active, TabMonitor)
	}
}

func TestRoot_ForwardsSwitchPIDToMonitor(t *testing.T) {
	rec := &recordingMonitor{}
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(rec).
		WithModelsPage(pages.Placeholder{TabName: "M"})
	updated, _ := r.Update(pages.SwitchToMonitorMsg{PID: 4321})
	rm := updated.(RootModel)
	if rm.active != TabMonitor {
		t.Errorf("active = %v, want TabMonitor", rm.active)
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
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
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
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
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
		WithLauncherPage(hintingPage{name: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
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
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
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
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
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
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
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
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
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
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
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
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})

	_, cmd := r.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Errorf("ctrl+c must always quit; got nil cmd")
	}
}

func TestRoot_TabSwitchesWhenPageDoesNotCapture(t *testing.T) {
	cap := &capturingPage{captured: false}
	r := NewRoot(TabLauncher).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithLauncherPage(cap).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyTab})
	rm := updated.(RootModel)

	if rm.active != TabProfiles {
		t.Errorf("active = %v, want TabProfiles", rm.active)
	}
}

func TestRoot_ModelsFilterDoesNotLeakQ(t *testing.T) {
	cap := &capturingPage{captured: true}
	r := NewRoot(TabModels).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
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
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
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
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
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
		WithLauncherPage(pages.NewLauncherPage(store, nil, nil))

	updated, _ := r.Update(pages.LaunchProfileMsg{ID: "any"})
	rm := updated.(RootModel)
	if rm.active != TabLauncher {
		t.Errorf("active = %v, want TabLauncher", rm.active)
	}
}

func TestRoot_TabStripContainsSeparator(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"})
	r.width = 120
	if !strings.Contains(r.View(), "│") {
		t.Errorf("tab strip missing │ separator; view:\n%s", r.View())
	}
}

func TestRoot_StatusBarMentionsHelp(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
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

func TestRoot_StatusBarIncludesActivePageHints(t *testing.T) {
	r := NewRoot(TabProfiles).
		WithProfilesPage(hintingPage{name: "P"}).
		WithLauncherPage(pages.Placeholder{TabName: "L"})
	r.width = 120
	r.recomputeHints()

	view := r.View()
	if !strings.Contains(view, "[x] do-x") {
		t.Errorf("active page hints not in status bar; view:\n%s", view)
	}
	if !strings.Contains(view, "[?] help") {
		t.Errorf("global help token missing; view:\n%s", view)
	}

	// Switch tabs — placeholder Launcher does not implement HintProvider, so
	// only globalHints should remain.
	updated, _ := r.activate(TabLauncher)
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

func TestRoot_TabCyclesThroughBackends(t *testing.T) {
	tm := teatest.NewTestModel(t, NewRoot(TabModels), teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Backends")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Server")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if err := tm.Quit(); err != nil {
		t.Fatalf("Quit returned err: %v", err)
	}
}

func TestRoot_ShiftTabFromLauncherGoesToServer(t *testing.T) {
	tm := teatest.NewTestModel(t, NewRoot(TabLauncher), teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	tm.Send(tea.KeyMsg{Type: tea.KeyShiftTab})
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Server")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if err := tm.Quit(); err != nil {
		t.Fatalf("Quit returned err: %v", err)
	}
}

func TestRoot_ServerTabSwitchByNumber(t *testing.T) {
	tm := teatest.NewTestModel(t, NewRoot(TabLauncher), teatest.WithInitialTermSize(120, 30))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 30})

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'6'}})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return strings.Contains(string(out), "Server")
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if err := tm.Quit(); err != nil {
		t.Fatalf("Quit returned err: %v", err)
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
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
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

func TestRoot_NumberSixSwallowedWhilePageCapturesInput(t *testing.T) {
	cap := &capturingPage{captured: true}
	r := NewRoot(TabServer).
		WithProfilesPage(pages.Placeholder{TabName: "P"}).
		WithLauncherPage(pages.Placeholder{TabName: "L"}).
		WithMonitorPage(pages.Placeholder{TabName: "Mo"}).
		WithModelsPage(pages.Placeholder{TabName: "Md"}).
		WithBackendsPage(pages.Placeholder{TabName: "B"}).
		WithServerPage(cap)

	updated, _ := r.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'6'}})
	rm := updated.(RootModel)
	if rm.active != TabServer {
		t.Errorf("active = %v, want still TabServer (page captures input)", rm.active)
	}
	if len(cap.keys) != 1 || cap.keys[0] != "6" {
		t.Errorf("page did not receive '6'; keys=%v", cap.keys)
	}
}

type recordingMonitor struct {
	lastSelectPID int
}

func (r *recordingMonitor) Init() tea.Cmd { return nil }
func (r *recordingMonitor) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(pages.MonitorSelectPIDMsg); ok {
		r.lastSelectPID = m.PID
	}
	return r, nil
}
func (r *recordingMonitor) View() string { return "" }
