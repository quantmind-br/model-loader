package httpproxy

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func intp(v int) *int { return &v }

func derefInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestDeriveContextLength(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want *int
	}{
		{"llama ctx-size", map[string]any{"ctx-size": float64(262144)}, intp(262144)},
		{"llama short c canonicalized", map[string]any{"c": float64(32768)}, intp(32768)},
		{"vllm max-model-len", map[string]any{"max-model-len": float64(98304)}, intp(98304)},
		{"tabby max-seq-len", map[string]any{"max-seq-len": float64(65536)}, intp(65536)},
		{"sglang context-length", map[string]any{"context-length": float64(131072)}, intp(131072)},
		{"dflash max-ctx", map[string]any{"max-ctx": float64(262144)}, intp(262144)},
		{"strata max-context", map[string]any{"max-context": float64(32768)}, intp(32768)},
		{"int value", map[string]any{"ctx-size": 4096}, intp(4096)},
		{"string numeric value", map[string]any{"max-model-len": "16384"}, intp(16384)},
		{"absent context flag", map[string]any{"n-gpu-layers": float64(999)}, nil},
		// Guards: these carry "ctx"/"seq" in the name but are NOT the model's
		// context window — they must never be misread as context_length.
		{"max-num-seqs is batch, not context", map[string]any{"max-num-seqs": float64(256)}, nil},
		{"spec cross-ctx is not the window", map[string]any{"spec-dflash-cross-ctx": float64(8192)}, nil},
		{"dflash wins via max-ctx when seqs also present", map[string]any{"max-num-seqs": float64(256), "max-ctx": float64(204800)}, intp(204800)},
		{"zero is ignored", map[string]any{"ctx-size": float64(0)}, nil},
		{"empty args", map[string]any{}, nil},
		{"nil args", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveContextLength(tc.args)
			if (got == nil) != (tc.want == nil) {
				t.Fatalf("got %v, want %v", derefInt(got), derefInt(tc.want))
			}
			if got != nil && *got != *tc.want {
				t.Errorf("got %d, want %d", *got, *tc.want)
			}
		})
	}
}

func TestProfileHasVision(t *testing.T) {
	if !profileHasVision(map[string]any{"mmproj": "/models/mmproj-F16.gguf"}) {
		t.Error("a non-empty mmproj path must mark the profile as vision-capable")
	}
	if profileHasVision(map[string]any{"mmproj": ""}) {
		t.Error("an empty mmproj must not mark the profile as vision-capable")
	}
	if profileHasVision(map[string]any{"ctx-size": float64(4096)}) {
		t.Error("a profile without mmproj must not be vision-capable")
	}
	if profileHasVision(nil) {
		t.Error("nil args must not be vision-capable")
	}
}

func TestModelLooksMultimodal(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		// vLLM VL / caption models (no projector flag) — must be detected.
		{"Qwen3-VL-8B-Instruct-AWQ-4bit", true},
		{"Huihui-Qwen3-VL-8B-Instruct-abliterated-AWQ-int4", true},
		{"Qwen3-VL-30B-A3B-Instruct-AWQ-4bit", true},
		{"Qwen3-VL-8B-NSFW-Caption-V4.5", true},
		{"llama-joycaption-beta-one-hf-llava-GPTQ-4bit", true},
		{"/models/MiniCPM-V-2_6", true},
		{"InternVL3-8B", true},
		{"some-vision-model", true},
		// Path forms.
		{"/home/me/models/qwen3-vl/model.safetensors", true},
		// Non-multimodal — must NOT be detected. Critically, "vllm" must not
		// trip the "vl" token match.
		{"/opt/vllm/models/Qwen3.6-27B-Q4_K_M.gguf", false},
		{"Nex-N2-mini-UD-Q4_K_XL.gguf", false},
		{"gemma-4-12B-it-qat-UD-Q4_K_XL.gguf", false},
		{"MiniCPM3-4B", false},
		{"Qwen3-Embedding-0.6B-f16.gguf", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			if got := modelLooksMultimodal(tc.model); got != tc.want {
				t.Errorf("modelLooksMultimodal(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}

// TestBuildORModel_VLLMVisionByModelName covers the case the mmproj flag cannot:
// a natively-multimodal vLLM model (no projector flag) is reported as
// text+image->text purely from its model name.
func TestBuildORModel_VLLMVisionByModelName(t *testing.T) {
	p := domain.Profile{
		ID:     "qwen3-vl-8b-awq-vllm-8k",
		Name:   "Qwen3 VL 8B",
		Model:  "Qwen3-VL-8B-Instruct-AWQ-4bit",
		Args:   map[string]any{"max-model-len": float64(8192)},
		Launch: domain.LaunchConfig{BackendID: "vllm-default"},
	}
	m := buildORModel(p)

	if m.Architecture.Modality != "text+image->text" {
		t.Errorf("modality = %q, want text+image->text", m.Architecture.Modality)
	}
	if len(m.Architecture.InputModalities) != 2 || m.Architecture.InputModalities[1] != "image" {
		t.Errorf("input_modalities = %v, want [text image]", m.Architecture.InputModalities)
	}
	if m.OwnedBy != "vllm-default" {
		t.Errorf("owned_by = %q, want vllm-default", m.OwnedBy)
	}
}

// TestBuildORModel_DflashMaxCtx covers the lucebox-dflash native server, whose
// context window lives under the backend-specific --max-ctx flag (not ctx-size).
func TestBuildORModel_DflashMaxCtx(t *testing.T) {
	p := domain.Profile{
		ID:    "qwen3.6-27b-lucebox-dflash-256k-maxperf",
		Name:  "Qwen3.6 27B DFlash",
		Model: "/models/Qwen3.6-27B-Q4_K_M.gguf",
		Args: map[string]any{
			"max-ctx": float64(262144),
			"draft":   "/models/draft.gguf",
		},
		Launch: domain.LaunchConfig{BackendID: "lucebox-dflash"},
	}
	m := buildORModel(p)

	if m.ContextLength == nil || *m.ContextLength != 262144 {
		t.Errorf("context_length = %v, want 262144", derefInt(m.ContextLength))
	}
	if m.TopProvider.ContextLength == nil || *m.TopProvider.ContextLength != 262144 {
		t.Errorf("top_provider.context_length = %v, want 262144", derefInt(m.TopProvider.ContextLength))
	}
	if m.OwnedBy != "lucebox-dflash" {
		t.Errorf("owned_by = %q, want lucebox-dflash", m.OwnedBy)
	}
	if m.Architecture.Modality != "text->text" {
		t.Errorf("modality = %q, want text->text (dense dflash, no vision)", m.Architecture.Modality)
	}
}

func TestBuildORModel_FilledFields(t *testing.T) {
	created := time.Unix(1735603200, 0)
	p := domain.Profile{
		ID:          "qwen3-vl-32b-gguf-q4km-vision-36k",
		Name:        "Qwen3 VL 32B",
		Description: "vision model",
		Tags:        []string{"vision"},
		Model:       "/models/Qwen3-VL-32B.gguf",
		Args: map[string]any{
			"ctx-size": float64(36864),
			"mmproj":   "/models/mmproj.gguf",
		},
		Launch: domain.LaunchConfig{BackendID: "llama-cpp-default"},
		Meta:   domain.ProfileMeta{CreatedAt: created},
	}
	m := buildORModel(p)

	if m.ID != p.ID {
		t.Errorf("id = %q, want %q", m.ID, p.ID)
	}
	if m.Object != "model" {
		t.Errorf("object = %q, want model", m.Object)
	}
	if m.OwnedBy != "llama-cpp-default" {
		t.Errorf("owned_by = %q, want llama-cpp-default", m.OwnedBy)
	}
	if m.CanonicalSlug != p.ID {
		t.Errorf("canonical_slug = %q, want %q", m.CanonicalSlug, p.ID)
	}
	if m.Name != "Qwen3 VL 32B" {
		t.Errorf("name = %q", m.Name)
	}
	if m.Created != 1735603200 {
		t.Errorf("created = %d, want 1735603200", m.Created)
	}
	if m.Description != "vision model" {
		t.Errorf("description = %q", m.Description)
	}
	if m.ContextLength == nil || *m.ContextLength != 36864 {
		t.Errorf("context_length = %v, want 36864", derefInt(m.ContextLength))
	}
	if m.TopProvider.ContextLength == nil || *m.TopProvider.ContextLength != 36864 {
		t.Errorf("top_provider.context_length = %v, want 36864", derefInt(m.TopProvider.ContextLength))
	}
	if m.Architecture.Modality != "text+image->text" {
		t.Errorf("modality = %q, want text+image->text", m.Architecture.Modality)
	}
	if len(m.Architecture.InputModalities) != 2 || m.Architecture.InputModalities[1] != "image" {
		t.Errorf("input_modalities = %v, want [text image]", m.Architecture.InputModalities)
	}
	if len(m.Architecture.OutputModalities) != 1 || m.Architecture.OutputModalities[0] != "text" {
		t.Errorf("output_modalities = %v, want [text]", m.Architecture.OutputModalities)
	}
	if m.Pricing.Prompt != "0" || m.Pricing.Completion != "0" {
		t.Errorf("pricing not zeroed: %+v", m.Pricing)
	}
	if m.TopProvider.IsModerated {
		t.Error("is_moderated must default to false")
	}
}

func TestBuildORModel_EmptyForNonApplicable(t *testing.T) {
	p := domain.Profile{
		ID:   "text-only",
		Name: "Text",
		Args: map[string]any{"max-model-len": float64(8192)},
	}
	m := buildORModel(p)

	if m.Architecture.Modality != "text->text" {
		t.Errorf("modality = %q, want text->text", m.Architecture.Modality)
	}
	if len(m.Architecture.InputModalities) != 1 || m.Architecture.InputModalities[0] != "text" {
		t.Errorf("input_modalities = %v, want [text]", m.Architecture.InputModalities)
	}
	if m.HuggingFaceID != nil {
		t.Error("hugging_face_id must be nil")
	}
	if m.Architecture.Tokenizer != nil {
		t.Error("tokenizer must be nil")
	}
	if m.Architecture.InstructType != nil {
		t.Error("instruct_type must be nil")
	}
	if m.PerRequestLimits != nil {
		t.Error("per_request_limits must be nil")
	}
	if m.SupportedParameters == nil || len(m.SupportedParameters) != 0 {
		t.Errorf("supported_parameters = %v, want empty (non-nil) slice", m.SupportedParameters)
	}
	if m.SupportedVoices != nil {
		t.Error("supported_voices must be nil")
	}
	if m.KnowledgeCutoff != nil {
		t.Error("knowledge_cutoff must be nil")
	}
	if m.ExpirationDate != nil {
		t.Error("expiration_date must be nil")
	}
	if m.Links != nil {
		t.Error("links must be nil")
	}
	if m.Reasoning != nil {
		t.Error("reasoning must be nil")
	}
	if m.DefaultParameters.Temperature != nil || m.DefaultParameters.TopP != nil {
		t.Error("default_parameters must be all-nil")
	}
}

// TestBuildORModel_JSONShapeMatchesOpenRouter pins the exact wire shape: every
// OpenRouter top-level key present, non-applicable scalars/objects emitted as
// null, supported_parameters as [], plus the two OpenAI model-object keys
// object ("model") and owned_by (the profile's serving backend id).
func TestBuildORModel_JSONShapeMatchesOpenRouter(t *testing.T) {
	p := domain.Profile{
		ID:     "m",
		Name:   "M",
		Args:   map[string]any{"ctx-size": float64(4096)},
		Launch: domain.LaunchConfig{BackendID: "llama-cpp-default"},
	}
	raw, err := json.Marshal(buildORModel(p))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	wantKeys := []string{
		"id", "object", "owned_by", "canonical_slug", "hugging_face_id", "name", "created", "description",
		"context_length", "architecture", "pricing", "top_provider", "per_request_limits",
		"supported_parameters", "default_parameters", "supported_voices", "knowledge_cutoff",
		"expiration_date", "links", "reasoning",
		// Anthropic model-object keys riding alongside the hybrid shape.
		"type", "display_name", "created_at",
	}
	for _, k := range wantKeys {
		if _, ok := top[k]; !ok {
			t.Errorf("missing top-level key %q; body=%s", k, raw)
		}
	}
	if string(top["object"]) != `"model"` {
		t.Errorf("object = %s, want \"model\"", top["object"])
	}
	if string(top["owned_by"]) != `"llama-cpp-default"` {
		t.Errorf("owned_by = %s, want \"llama-cpp-default\"", top["owned_by"])
	}
	for _, k := range []string{"hugging_face_id", "per_request_limits", "supported_voices", "knowledge_cutoff", "expiration_date", "links", "reasoning"} {
		if string(top[k]) != "null" {
			t.Errorf("%q = %s, want null", k, top[k])
		}
	}
	if string(top["supported_parameters"]) != "[]" {
		t.Errorf("supported_parameters = %s, want []", top["supported_parameters"])
	}

	var arch map[string]json.RawMessage
	if err := json.Unmarshal(top["architecture"], &arch); err != nil {
		t.Fatalf("unmarshal architecture: %v", err)
	}
	for _, k := range []string{"modality", "input_modalities", "output_modalities", "tokenizer", "instruct_type"} {
		if _, ok := arch[k]; !ok {
			t.Errorf("architecture missing %q", k)
		}
	}

	var pricing map[string]json.RawMessage
	if err := json.Unmarshal(top["pricing"], &pricing); err != nil {
		t.Fatalf("unmarshal pricing: %v", err)
	}
	for _, k := range []string{"prompt", "completion", "request", "image", "audio", "web_search", "internal_reasoning", "input_cache_read", "input_cache_write"} {
		if string(pricing[k]) != `"0"` {
			t.Errorf("pricing[%q] = %s, want \"0\"", k, pricing[k])
		}
	}

	var tp map[string]json.RawMessage
	if err := json.Unmarshal(top["top_provider"], &tp); err != nil {
		t.Fatalf("unmarshal top_provider: %v", err)
	}
	for _, k := range []string{"context_length", "max_completion_tokens", "is_moderated"} {
		if _, ok := tp[k]; !ok {
			t.Errorf("top_provider missing %q", k)
		}
	}
	if string(tp["max_completion_tokens"]) != "null" {
		t.Errorf("top_provider.max_completion_tokens = %s, want null", tp["max_completion_tokens"])
	}

	var dp map[string]json.RawMessage
	if err := json.Unmarshal(top["default_parameters"], &dp); err != nil {
		t.Fatalf("unmarshal default_parameters: %v", err)
	}
	for _, k := range []string{"temperature", "top_p", "top_k", "frequency_penalty", "presence_penalty", "repetition_penalty"} {
		if string(dp[k]) != "null" {
			t.Errorf("default_parameters[%q] = %s, want null", k, dp[k])
		}
	}
}

// TestBuildORModel_AnthropicFields pins the additive Anthropic model-object
// keys: type ("model"), display_name (the profile name), and created_at
// (RFC3339 of Meta.CreatedAt).
func TestBuildORModel_AnthropicFields(t *testing.T) {
	created := time.Date(2026, 6, 1, 12, 30, 0, 0, time.UTC)
	p := domain.Profile{
		ID:   "m",
		Name: "My Model",
		Meta: domain.ProfileMeta{CreatedAt: created},
	}
	raw, err := json.Marshal(buildORModel(p))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if top["type"] != "model" {
		t.Errorf("type = %v, want \"model\"", top["type"])
	}
	if top["display_name"] != "My Model" {
		t.Errorf("display_name = %v, want profile name", top["display_name"])
	}
	if top["created_at"] != "2026-06-01T12:30:00Z" {
		t.Errorf("created_at = %v, want RFC3339 of Meta.CreatedAt", top["created_at"])
	}
}
