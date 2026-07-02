package httpproxy

import (
	"strconv"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// This file shapes the GET /v1/models response to mirror the OpenRouter
// `/api/v1/models` model object. Fields that have no meaning for a local proxy
// are emitted empty (null / [] / "0") so the wire structure stays identical to
// OpenRouter's. On top of the OpenRouter shape we also emit the two OpenAI
// model-object keys `object` ("model") and `owned_by` (the profile's serving
// backend id), which several OpenAI-compatible clients expect.

// orModel mirrors a single OpenRouter model object, plus the OpenAI `object`
// and `owned_by` keys. `object` is always "model"; `owned_by` carries the
// profile's serving backend id (`launch.backendId`). The list envelope keeps
// the separate `object: "list"`.
type orModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
	// Anthropic model-object keys (GET /v1/models on the Anthropic API):
	// type is always "model", display_name mirrors Name, created_at is the
	// RFC3339 form of the profile's Meta.CreatedAt. Purely additive — OpenAI
	// and OpenRouter clients ignore them.
	Type                string              `json:"type"`
	DisplayName         string              `json:"display_name"`
	CreatedAt           string              `json:"created_at"`
	CanonicalSlug       string              `json:"canonical_slug"`
	HuggingFaceID       *string             `json:"hugging_face_id"`
	Name                string              `json:"name"`
	Created             int64               `json:"created"`
	Description         string              `json:"description"`
	ContextLength       *int                `json:"context_length"`
	Architecture        orArchitecture      `json:"architecture"`
	Pricing             orPricing           `json:"pricing"`
	TopProvider         orTopProvider       `json:"top_provider"`
	PerRequestLimits    any                 `json:"per_request_limits"`
	SupportedParameters []string            `json:"supported_parameters"`
	DefaultParameters   orDefaultParameters `json:"default_parameters"`
	SupportedVoices     any                 `json:"supported_voices"`
	KnowledgeCutoff     *string             `json:"knowledge_cutoff"`
	ExpirationDate      *string             `json:"expiration_date"`
	Links               any                 `json:"links"`
	Reasoning           any                 `json:"reasoning"`
}

type orArchitecture struct {
	Modality         string   `json:"modality"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	Tokenizer        *string  `json:"tokenizer"`
	InstructType     *string  `json:"instruct_type"`
}

// orPricing mirrors OpenRouter's pricing object. Values are strings there; a
// local proxy is free, so every rate is "0".
type orPricing struct {
	Prompt            string `json:"prompt"`
	Completion        string `json:"completion"`
	Request           string `json:"request"`
	Image             string `json:"image"`
	Audio             string `json:"audio"`
	WebSearch         string `json:"web_search"`
	InternalReasoning string `json:"internal_reasoning"`
	InputCacheRead    string `json:"input_cache_read"`
	InputCacheWrite   string `json:"input_cache_write"`
}

type orTopProvider struct {
	ContextLength       *int `json:"context_length"`
	MaxCompletionTokens *int `json:"max_completion_tokens"`
	IsModerated         bool `json:"is_moderated"`
}

type orDefaultParameters struct {
	Temperature       *float64 `json:"temperature"`
	TopP              *float64 `json:"top_p"`
	TopK              *float64 `json:"top_k"`
	FrequencyPenalty  *float64 `json:"frequency_penalty"`
	PresencePenalty   *float64 `json:"presence_penalty"`
	RepetitionPenalty *float64 `json:"repetition_penalty"`
}

func zeroPricing() orPricing {
	return orPricing{
		Prompt:            "0",
		Completion:        "0",
		Request:           "0",
		Image:             "0",
		Audio:             "0",
		WebSearch:         "0",
		InternalReasoning: "0",
		InputCacheRead:    "0",
		InputCacheWrite:   "0",
	}
}

// contextLengthFlags lists the canonical args keys that carry the model's total
// context window, in priority order, across the supported backend kinds.
var contextLengthFlags = []string{
	"ctx-size",       // llama-server family (also "c" via CanonicalFlag)
	"max-model-len",  // vLLM
	"max-seq-len",    // tabby / exllama
	"context-length", // sglang
	"max-ctx",        // dflash (lucebox native server) — context window / KV size
	"n-ctx",          // alternate spelling
}

// deriveContextLength inspects a profile's args (normalizing short-form keys via
// domain.CanonicalFlag) and returns the configured context window, or nil if no
// recognized context flag holds a positive value.
func deriveContextLength(args map[string]any) *int {
	if len(args) == 0 {
		return nil
	}
	canon := make(map[string]any, len(args))
	for k, v := range args {
		canon[domain.CanonicalFlag(k)] = v
	}
	for _, key := range contextLengthFlags {
		if v, ok := canon[key]; ok {
			if n, ok := toPositiveInt(v); ok {
				return &n
			}
		}
	}
	return nil
}

// profileHasVision reports whether the profile wires a multimodal projector,
// which is the flag-derivable signal of image input across the llama.cpp
// family. It does NOT cover natively-multimodal backends (vLLM/SGLang VL and
// caption models serve images without a projector flag) — those are detected by
// name via modelLooksMultimodal.
func profileHasVision(args map[string]any) bool {
	for k, v := range args {
		key := domain.CanonicalFlag(k)
		if (key == "mmproj" || key == "mmproj-url" || k == "mm") && nonEmptyArg(v) {
			return true
		}
	}
	return false
}

// visionModelSubstrings are case-insensitive markers in a model name/path that
// identify a vision-language (natively multimodal) model. They are the fallback
// signal for backends that serve multimodal models without a projector flag
// (vLLM/SGLang VL and caption models). This mirrors OpenRouter semantics, where
// `architecture.modality` describes the model's inherent capability rather than
// one deployment's wiring.
var visionModelSubstrings = []string{
	"vision",
	"caption", // also matches "joycaption"
	"llava",
	"internvl",
	"minicpm-v",
	"moondream",
	"pixtral",
	"smolvlm",
	"molmo",
	"idefics",
	"fuyu",
}

// visionModelTokens are short markers matched only as whole tokens (split on
// -_./\\ and spaces) so "vl" inside "vllm" or other words never false-positives.
var visionModelTokens = map[string]bool{
	"vl":  true,
	"vlm": true,
	"vqa": true,
}

func isModelSeparator(r rune) bool {
	switch r {
	case '-', '_', '.', '/', '\\', ' ':
		return true
	}
	return false
}

// modelLooksMultimodal reports whether a model name/path carries a well-known
// vision-language marker. It is the only signal available for backends whose VL
// models are multimodal without a projector flag (e.g. vLLM Qwen3-VL / caption
// models), and complements the flag-based profileHasVision.
func modelLooksMultimodal(model string) bool {
	s := strings.ToLower(strings.TrimSpace(model))
	if s == "" {
		return false
	}
	for _, sub := range visionModelSubstrings {
		if strings.Contains(s, sub) {
			return true
		}
	}
	for _, tok := range strings.FieldsFunc(s, isModelSeparator) {
		if visionModelTokens[tok] {
			return true
		}
	}
	return false
}

// buildORModel maps a profile onto the OpenRouter model shape. Derivable facts
// (id, name, description, created, context length, image modality, free
// pricing) are filled; everything else is left empty.
func buildORModel(p domain.Profile) orModel {
	ctx := deriveContextLength(p.Args)

	inputModalities := []string{"text"}
	modality := "text->text"
	if profileHasVision(p.Args) || modelLooksMultimodal(p.Model) {
		inputModalities = []string{"text", "image"}
		modality = "text+image->text"
	}

	return orModel{
		ID:            p.ID,
		Object:        "model",
		OwnedBy:       p.Launch.BackendID,
		Type:          "model",
		DisplayName:   p.Name,
		CreatedAt:     p.Meta.CreatedAt.UTC().Format(time.RFC3339),
		CanonicalSlug: p.ID,
		HuggingFaceID: nil,
		Name:          p.Name,
		Created:       p.Meta.CreatedAt.Unix(),
		Description:   p.Description,
		ContextLength: ctx,
		Architecture: orArchitecture{
			Modality:         modality,
			InputModalities:  inputModalities,
			OutputModalities: []string{"text"},
			Tokenizer:        nil,
			InstructType:     nil,
		},
		Pricing: zeroPricing(),
		TopProvider: orTopProvider{
			ContextLength:       ctx,
			MaxCompletionTokens: nil,
			IsModerated:         false,
		},
		PerRequestLimits:    nil,
		SupportedParameters: []string{},
		DefaultParameters:   orDefaultParameters{},
		SupportedVoices:     nil,
		KnowledgeCutoff:     nil,
		ExpirationDate:      nil,
		Links:               nil,
		Reasoning:           nil,
	}
}

func toPositiveInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n > 0 {
			return int(n), true
		}
	case float32:
		if n > 0 {
			return int(n), true
		}
	case int:
		if n > 0 {
			return n, true
		}
	case int64:
		if n > 0 {
			return int(n), true
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil && i > 0 {
			return i, true
		}
	}
	return 0, false
}

func nonEmptyArg(v any) bool {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x) != ""
	case bool:
		return x
	case nil:
		return false
	default:
		return true
	}
}
