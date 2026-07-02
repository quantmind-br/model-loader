package httpproxy

import (
	"encoding/json"
	"testing"
)

func TestLevelToBudget(t *testing.T) {
	cases := map[string]struct {
		want int
		ok   bool
	}{
		"none": {0, true}, "auto": {-1, true}, "minimal": {512, true},
		"low": {1024, true}, "medium": {8192, true}, "high": {24576, true},
		"xhigh": {32768, true}, "max": {128000, true},
		"HIGH":  {24576, true}, // case-insensitive
		"bogus": {0, false},
	}
	for level, tc := range cases {
		got, ok := levelToBudget(level)
		if got != tc.want || ok != tc.ok {
			t.Errorf("levelToBudget(%q) = (%d,%v), want (%d,%v)", level, got, ok, tc.want, tc.ok)
		}
	}
}

func TestBudgetToLevel(t *testing.T) {
	cases := []struct {
		budget int
		want   string
	}{
		{-1, "auto"}, {0, "none"}, {1, "minimal"}, {512, "minimal"},
		{513, "low"}, {1024, "low"}, {1025, "medium"}, {8192, "medium"},
		{8193, "high"}, {24576, "high"}, {24577, "xhigh"}, {100000, "xhigh"},
	}
	for _, tc := range cases {
		if got, _ := budgetToLevel(tc.budget); got != tc.want {
			t.Errorf("budgetToLevel(%d) = %q, want %q", tc.budget, got, tc.want)
		}
	}
}

func TestExtractThinkingConfig(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantMode   thinkingMode
		wantLevel  string
		wantBudget int
	}{
		{"disabled", `{"thinking":{"type":"disabled"}}`, thinkModeNone, "", 0},
		{"adaptive+effort", `{"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"}}`, thinkModeLevel, "xhigh", 0},
		{"enabled+budget", `{"thinking":{"type":"enabled","budget_tokens":8192}}`, thinkModeBudget, "", 8192},
		{"enabled+budget zero", `{"thinking":{"type":"enabled","budget_tokens":0}}`, thinkModeNone, "", 0},
		{"enabled+budget -1", `{"thinking":{"type":"enabled","budget_tokens":-1}}`, thinkModeAuto, "", 0},
		{"enabled no budget", `{"thinking":{"type":"enabled"}}`, thinkModeAuto, "", 0},
		{"adaptive no effort", `{"thinking":{"type":"adaptive"}}`, thinkModeAuto, "", 0},
		{"effort only", `{"output_config":{"effort":"low"}}`, thinkModeLevel, "low", 0},
		{"effort none", `{"output_config":{"effort":"none"}}`, thinkModeNone, "", 0},
		{"nothing", `{}`, thinkModeUnset, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req anthropicMessagesRequest
			if err := json.Unmarshal([]byte(tc.raw), &req); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			cfg := extractThinkingConfig(&req)
			if cfg.Mode != tc.wantMode || cfg.Level != tc.wantLevel || cfg.Budget != tc.wantBudget {
				t.Errorf("got %+v, want mode=%d level=%q budget=%d", cfg, tc.wantMode, tc.wantLevel, tc.wantBudget)
			}
		})
	}
}

func TestApplyThinking(t *testing.T) {
	t.Run("none disables and sets effort none", func(t *testing.T) {
		var out oaiChatRequest
		applyThinking(&out, thinkingConfig{Mode: thinkModeNone})
		if out.ReasoningEffort != "none" {
			t.Errorf("reasoning_effort = %q, want none", out.ReasoningEffort)
		}
		if v, ok := out.ChatTemplateKwargs["enable_thinking"]; !ok || v != false {
			t.Errorf("enable_thinking = %v (ok=%v), want false", v, ok)
		}
	})
	t.Run("level passes through, no template kwargs", func(t *testing.T) {
		var out oaiChatRequest
		applyThinking(&out, thinkingConfig{Mode: thinkModeLevel, Level: "high"})
		if out.ReasoningEffort != "high" {
			t.Errorf("reasoning_effort = %q, want high", out.ReasoningEffort)
		}
		if out.ChatTemplateKwargs != nil {
			t.Errorf("enabled thinking must not force chat_template_kwargs, got %v", out.ChatTemplateKwargs)
		}
	})
	t.Run("budget maps to nearest level", func(t *testing.T) {
		var out oaiChatRequest
		applyThinking(&out, thinkingConfig{Mode: thinkModeBudget, Budget: 8192})
		if out.ReasoningEffort != "medium" {
			t.Errorf("reasoning_effort = %q, want medium", out.ReasoningEffort)
		}
	})
	t.Run("auto emits auto", func(t *testing.T) {
		var out oaiChatRequest
		applyThinking(&out, thinkingConfig{Mode: thinkModeAuto})
		if out.ReasoningEffort != "auto" {
			t.Errorf("reasoning_effort = %q, want auto", out.ReasoningEffort)
		}
	})
	t.Run("unset touches nothing", func(t *testing.T) {
		var out oaiChatRequest
		applyThinking(&out, thinkingConfig{Mode: thinkModeUnset})
		if out.ReasoningEffort != "" || out.ChatTemplateKwargs != nil {
			t.Errorf("unset must be a no-op, got effort=%q kwargs=%v", out.ReasoningEffort, out.ChatTemplateKwargs)
		}
	})
}

// TestTranslateAnthropicRequest_ThinkingIntegration wires the pivot into the
// full request translation.
func TestTranslateAnthropicRequest_ThinkingIntegration(t *testing.T) {
	t.Run("disabled turns reasoning off", func(t *testing.T) {
		out := mustTranslate(t, `{"model":"m","max_tokens":1,"thinking":{"type":"disabled"},"messages":[{"role":"user","content":"x"}]}`)
		if out.ReasoningEffort != "none" {
			t.Errorf("reasoning_effort = %q, want none", out.ReasoningEffort)
		}
		if v, ok := out.ChatTemplateKwargs["enable_thinking"]; !ok || v != false {
			t.Errorf("enable_thinking = %v, want false", v)
		}
	})
	t.Run("adaptive effort passes through", func(t *testing.T) {
		out := mustTranslate(t, `{"model":"m","max_tokens":1,"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"},"messages":[{"role":"user","content":"x"}]}`)
		if out.ReasoningEffort != "xhigh" {
			t.Errorf("reasoning_effort = %q, want xhigh", out.ReasoningEffort)
		}
	})
	t.Run("no reasoning fields leaves it unset", func(t *testing.T) {
		out := mustTranslate(t, `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":"x"}]}`)
		if out.ReasoningEffort != "" || out.ChatTemplateKwargs != nil {
			t.Errorf("want no reasoning, got effort=%q kwargs=%v", out.ReasoningEffort, out.ChatTemplateKwargs)
		}
	})
}

// TestTranslateAnthropicRequest_Metadata maps metadata.user_id → OpenAI user.
func TestTranslateAnthropicRequest_Metadata(t *testing.T) {
	out := mustTranslate(t, `{"model":"m","max_tokens":1,"metadata":{"user_id":"u-123"},"messages":[{"role":"user","content":"x"}]}`)
	if out.User != "u-123" {
		t.Errorf("user = %q, want u-123", out.User)
	}
}

func TestParseModelSuffix(t *testing.T) {
	cases := []struct {
		in, base, suffix string
		ok               bool
	}{
		{"gemma-4-12b-it-mtp-256k(none)", "gemma-4-12b-it-mtp-256k", "none", true},
		{"qwen(16384)", "qwen", "16384", true},
		{"plain-model", "plain-model", "", false},
		{"weird(unclosed", "weird(unclosed", "", false},
		{"a(b)(c)", "a(b)", "c", true},
		{"(x)", "(x)", "", false}, // no base
	}
	for _, tc := range cases {
		base, suffix, ok := parseModelSuffix(tc.in)
		if base != tc.base || suffix != tc.suffix || ok != tc.ok {
			t.Errorf("parseModelSuffix(%q) = (%q,%q,%v), want (%q,%q,%v)", tc.in, base, suffix, ok, tc.base, tc.suffix, tc.ok)
		}
	}
}

func TestThinkingConfigFromSuffix(t *testing.T) {
	if cfg, ok := thinkingConfigFromSuffix("none"); !ok || cfg.Mode != thinkModeNone {
		t.Errorf("none → %+v,%v", cfg, ok)
	}
	if cfg, ok := thinkingConfigFromSuffix("high"); !ok || cfg.Mode != thinkModeLevel || cfg.Level != "high" {
		t.Errorf("high → %+v,%v", cfg, ok)
	}
	if cfg, ok := thinkingConfigFromSuffix("16384"); !ok || cfg.Mode != thinkModeBudget || cfg.Budget != 16384 {
		t.Errorf("16384 → %+v,%v", cfg, ok)
	}
	if cfg, ok := thinkingConfigFromSuffix("0"); !ok || cfg.Mode != thinkModeNone {
		t.Errorf("0 → %+v,%v", cfg, ok)
	}
	if _, ok := thinkingConfigFromSuffix("garbage"); ok {
		t.Errorf("garbage must not be a valid suffix")
	}
}

// TestApplyReasoningOverride: the model suffix takes precedence over the body's
// reasoning fields.
func TestApplyReasoningOverride(t *testing.T) {
	out := &oaiChatRequest{}
	applyThinking(out, thinkingConfig{Mode: thinkModeLevel, Level: "xhigh"}) // body says xhigh
	applyReasoningOverride(out, "none")                                      // suffix forces off
	if out.ReasoningEffort != "none" {
		t.Errorf("effort = %q, want none (suffix wins)", out.ReasoningEffort)
	}
	if v, ok := out.ChatTemplateKwargs["enable_thinking"]; !ok || v != false {
		t.Errorf("enable_thinking = %v, want false", v)
	}
}
