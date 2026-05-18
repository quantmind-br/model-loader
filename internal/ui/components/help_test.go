package components

import (
	"strings"
	"testing"
)

func TestRenderHelp_ContainsKeybindings(t *testing.T) {
	out, err := RenderHelp(80)
	if err != nil {
		t.Fatalf("RenderHelp: %v", err)
	}
	if out == "" {
		t.Fatal("RenderHelp returned empty string")
	}
	if !strings.Contains(out, "Keybindings") {
		t.Errorf("output missing 'Keybindings' header; got:\n%s", out)
	}
}

func TestRenderHelp_MentionsAllTabs(t *testing.T) {
	out, err := RenderHelp(80)
	if err != nil {
		t.Fatalf("RenderHelp: %v", err)
	}
	for _, tab := range []string{"Profiles", "Launcher", "Server", "Models", "Backends"} {
		if !strings.Contains(out, tab) {
			t.Errorf("output missing %q", tab)
		}
	}
}

func TestRenderHelp_MentionsTabRange(t *testing.T) {
	out, err := RenderHelp(80)
	if err != nil {
		t.Fatalf("RenderHelp: %v", err)
	}
	if !strings.Contains(out, "1") || !strings.Contains(out, "5") {
		t.Errorf("output missing 1–6 tab range; got:\n%s", out)
	}
}

func TestHelpMarkdown_MentionsConvention(t *testing.T) {
	if !strings.Contains(HelpMarkdown, "Convention: lowercase") {
		t.Errorf("HelpMarkdown missing letter-case convention sentence")
	}
}

func TestRenderContextualHelp_IncludesActiveContext(t *testing.T) {
	out, err := RenderContextualHelp(80, "[s] start  [x] stop")
	if err != nil {
		t.Fatalf("RenderContextualHelp: %v", err)
	}
	if !strings.Contains(out, "Current Page") {
		t.Errorf("output missing 'Current Page' heading; got:\n%s", out)
	}
	if !strings.Contains(out, "[s] start") {
		t.Errorf("output missing active context; got:\n%s", out)
	}
}

func TestRenderContextualHelp_EmptyContextOmitsHeading(t *testing.T) {
	out, err := RenderContextualHelp(80, "")
	if err != nil {
		t.Fatalf("RenderContextualHelp: %v", err)
	}
	if strings.Contains(out, "Current Page") {
		t.Errorf("output should not contain 'Current Page' when context is empty; got:\n%s", out)
	}
}
