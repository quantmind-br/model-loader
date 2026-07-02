package httpproxy

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// Canonical reasoning/thinking pivot, modeled on CLIProxyAPI's internal/thinking.
// The Anthropic request expresses reasoning three ways (thinking.type,
// thinking.budget_tokens, output_config.effort); we normalize them into one
// thinkingConfig, then emit what local backends understand (reasoning_effort +
// chat_template_kwargs.enable_thinking). Backends ignore whichever they don't use.

type thinkingMode int

const (
	thinkModeUnset  thinkingMode = iota // no reasoning control in the request
	thinkModeNone                       // reasoning explicitly off
	thinkModeAuto                       // let the model decide
	thinkModeLevel                      // discrete level (low/medium/high/…)
	thinkModeBudget                     // numeric token budget
)

type thinkingConfig struct {
	Mode   thinkingMode
	Level  string // canonical level when Mode == thinkModeLevel
	Budget int    // token budget when Mode == thinkModeBudget
}

// levelBudgets is the canonical level→budget table (from CLIProxyAPI).
var levelBudgets = map[string]int{
	"none": 0, "auto": -1, "minimal": 512, "low": 1024,
	"medium": 8192, "high": 24576, "xhigh": 32768, "max": 128000,
}

// levelToBudget converts a canonical level to its budget (case-insensitive).
func levelToBudget(level string) (int, bool) {
	b, ok := levelBudgets[strings.ToLower(strings.TrimSpace(level))]
	return b, ok
}

// budgetToLevel maps a numeric budget to the nearest canonical level by
// threshold. -1→auto, 0→none; <-1 is invalid.
func budgetToLevel(budget int) (string, bool) {
	switch {
	case budget < -1:
		return "", false
	case budget == -1:
		return "auto", true
	case budget == 0:
		return "none", true
	case budget <= 512:
		return "minimal", true
	case budget <= 1024:
		return "low", true
	case budget <= 8192:
		return "medium", true
	case budget <= 24576:
		return "high", true
	default:
		return "xhigh", true
	}
}

// extractThinkingConfig normalizes the Anthropic reasoning fields into the
// canonical pivot. Precedence: thinking.type drives the mode; a numeric
// budget_tokens wins for type "enabled"; output_config.effort supplies the
// level for adaptive/auto and when only output_config is present.
func extractThinkingConfig(req *anthropicMessagesRequest) thinkingConfig {
	effort := ""
	if len(bytes.TrimSpace(req.OutputConfig)) > 0 {
		var oc struct {
			Effort string `json:"effort"`
		}
		if json.Unmarshal(req.OutputConfig, &oc) == nil {
			effort = strings.ToLower(strings.TrimSpace(oc.Effort))
		}
	}

	if len(bytes.TrimSpace(req.Thinking)) > 0 {
		var th struct {
			Type   string `json:"type"`
			Budget *int   `json:"budget_tokens"`
		}
		if json.Unmarshal(req.Thinking, &th) == nil {
			switch strings.ToLower(strings.TrimSpace(th.Type)) {
			case "disabled":
				return thinkingConfig{Mode: thinkModeNone}
			case "enabled":
				if th.Budget != nil {
					return budgetConfig(*th.Budget)
				}
				return thinkingConfig{Mode: thinkModeAuto}
			case "adaptive", "auto":
				if effort != "" {
					return effortConfig(effort)
				}
				return thinkingConfig{Mode: thinkModeAuto}
			}
			// Unknown/absent type: a budget still expresses intent.
			if th.Budget != nil {
				return budgetConfig(*th.Budget)
			}
		}
	}

	if effort != "" {
		return effortConfig(effort)
	}
	return thinkingConfig{Mode: thinkModeUnset}
}

// budgetConfig maps a numeric budget to a config: 0→none, negative→auto.
func budgetConfig(budget int) thinkingConfig {
	switch {
	case budget == 0:
		return thinkingConfig{Mode: thinkModeNone}
	case budget < 0:
		return thinkingConfig{Mode: thinkModeAuto}
	default:
		return thinkingConfig{Mode: thinkModeBudget, Budget: budget}
	}
}

// effortConfig maps an effort/level string to a config.
func effortConfig(effort string) thinkingConfig {
	switch effort {
	case "none":
		return thinkingConfig{Mode: thinkModeNone}
	case "auto":
		return thinkingConfig{Mode: thinkModeAuto}
	default:
		return thinkingConfig{Mode: thinkModeLevel, Level: effort}
	}
}

// applyThinking injects the canonical config into the OpenAI request. It emits
// reasoning_effort (understood by gpt-oss/vLLM reasoning parsers) and, when
// reasoning is turned OFF, chat_template_kwargs.enable_thinking=false (the
// switch the Qwen3/llama.cpp family uses). Backends ignore the irrelevant one.
// thinkModeUnset is a no-op so requests without reasoning fields are unchanged.
func applyThinking(out *oaiChatRequest, cfg thinkingConfig) {
	switch cfg.Mode {
	case thinkModeNone:
		out.ReasoningEffort = "none"
		if out.ChatTemplateKwargs == nil {
			out.ChatTemplateKwargs = map[string]any{}
		}
		out.ChatTemplateKwargs["enable_thinking"] = false
	case thinkModeAuto:
		out.ReasoningEffort = "auto"
	case thinkModeLevel:
		out.ReasoningEffort = cfg.Level
	case thinkModeBudget:
		if lvl, ok := budgetToLevel(cfg.Budget); ok {
			out.ReasoningEffort = lvl
		}
	case thinkModeUnset:
		// no reasoning control present — leave the request untouched
	}
}

// parseModelSuffix splits a model id of the form "base(suffix)" into its parts
// (only a trailing "(...)" with a non-empty base counts). Enables forcing the
// reasoning level via the model name, e.g. "qwen3.6-27b(none)".
func parseModelSuffix(model string) (base, suffix string, ok bool) {
	if !strings.HasSuffix(model, ")") {
		return model, "", false
	}
	open := strings.LastIndex(model, "(")
	if open <= 0 { // no "(" or nothing before it
		return model, "", false
	}
	return model[:open], model[open+1 : len(model)-1], true
}

// thinkingConfigFromSuffix parses a reasoning suffix: a canonical level
// (none/low/…/max/auto) or a numeric token budget.
func thinkingConfigFromSuffix(suffix string) (thinkingConfig, bool) {
	s := strings.ToLower(strings.TrimSpace(suffix))
	if s == "" {
		return thinkingConfig{}, false
	}
	if n, err := strconv.Atoi(s); err == nil {
		return budgetConfig(n), true
	}
	if _, ok := levelToBudget(s); ok {
		return effortConfig(s), true
	}
	return thinkingConfig{}, false
}

// applyReasoningOverride replaces any body-derived reasoning on out with the
// model-suffix override — the suffix takes precedence (following CLIProxyAPI).
func applyReasoningOverride(out *oaiChatRequest, suffix string) {
	cfg, ok := thinkingConfigFromSuffix(suffix)
	if !ok {
		return
	}
	out.ReasoningEffort = ""
	if out.ChatTemplateKwargs != nil {
		delete(out.ChatTemplateKwargs, "enable_thinking")
		if len(out.ChatTemplateKwargs) == 0 {
			out.ChatTemplateKwargs = nil
		}
	}
	applyThinking(out, cfg)
}
