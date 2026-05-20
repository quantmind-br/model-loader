package profile_editor

import (
	"reflect"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestDraft_EssentialsFieldExists(t *testing.T) {
	var d Draft
	typ := reflect.TypeOf(d)

	f, ok := typ.FieldByName("Essentials")
	if !ok {
		t.Fatalf("Draft missing Essentials field")
	}

	want := reflect.TypeOf(map[string]string{})
	if f.Type != want {
		t.Fatalf("Essentials field type = %v, want %v", f.Type, want)
	}

	d.Essentials = map[string]string{"n-gpu-layers": "33"}
	if d.Essentials["n-gpu-layers"] != "33" {
		t.Fatalf("Essentials map read/write failed")
	}
}

func llamaTestSchema() domain.FlagSchema {
	return domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"n-gpu-layers": {Long: "n-gpu-layers", Short: "ngl", Type: domain.FlagTypeInt},
		"ctx-size":     {Long: "ctx-size", Short: "c", Type: domain.FlagTypeInt},
		"batch-size":   {Long: "batch-size", Short: "b", Type: domain.FlagTypeInt},
		"ubatch-size":  {Long: "ubatch-size", Short: "ub", Type: domain.FlagTypeInt},
		"port":         {Long: "port", Type: domain.FlagTypeInt},
		"flash-attn":   {Long: "flash-attn", Short: "fa", Type: domain.FlagTypeEnum, EnumValues: []string{"on", "off", "auto"}},
		"cache-type-k": {Long: "cache-type-k", Type: domain.FlagTypeEnum, EnumValues: []string{"f16", "q8_0", "q4_0"}},
		"cache-type-v": {Long: "cache-type-v", Type: domain.FlagTypeEnum, EnumValues: []string{"f16", "q8_0", "q4_0"}},
		"seed":         {Long: "seed", Type: domain.FlagTypeInt},
	}}
}

func vllmTestSchema() domain.FlagSchema {
	return domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"tensor-parallel-size":   {Long: "tensor-parallel-size", Short: "tp", Type: domain.FlagTypeInt},
		"gpu-memory-utilization": {Long: "gpu-memory-utilization", Type: domain.FlagTypeFloat},
		"max-model-len":          {Long: "max-model-len", Type: domain.FlagTypeInt},
		"dtype":                  {Long: "dtype", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "float16", "bfloat16", "float32"}},
		"quantization":           {Long: "quantization", Type: domain.FlagTypeString},
		"port":                   {Long: "port", Type: domain.FlagTypeInt},
		"served-model-name":      {Long: "served-model-name", Type: domain.FlagTypeString},
		"seed":                   {Long: "seed", Type: domain.FlagTypeInt},
	}}
}

func sglangTestSchema() domain.FlagSchema {
	return domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"tp-size":             {Long: "tp-size", Type: domain.FlagTypeInt},
		"dp-size":             {Long: "dp-size", Type: domain.FlagTypeInt},
		"mem-fraction-static": {Long: "mem-fraction-static", Type: domain.FlagTypeFloat},
		"dtype":               {Long: "dtype", Type: domain.FlagTypeEnum, EnumValues: []string{"auto", "float16", "bfloat16", "float32"}},
		"quantization":        {Long: "quantization", Type: domain.FlagTypeString},
		"context-length":      {Long: "context-length", Type: domain.FlagTypeInt},
		"port":                {Long: "port", Type: domain.FlagTypeInt},
		"served-model-name":   {Long: "served-model-name", Type: domain.FlagTypeString},
		"seed":                {Long: "seed", Type: domain.FlagTypeInt},
	}}
}

func TestApplyToWithSchema_LlamaEssentialsSerialize(t *testing.T) {
	d := Draft{Essentials: map[string]string{
		"n-gpu-layers": "33",
		"ctx-size":     "8192",
		"batch-size":   "2048",
		"ubatch-size":  "512",
		"port":         "4321",
		"flash-attn":   "on",
		"cache-type-k": "q8_0",
		"cache-type-v": "q8_0",
	}}
	pr := d.ApplyToWithSchema(domain.Profile{}, llamaTestSchema())

	cases := map[string]any{
		"n-gpu-layers": float64(33),
		"ctx-size":     float64(8192),
		"batch-size":   float64(2048),
		"ubatch-size":  float64(512),
		"port":         float64(4321),
		"flash-attn":   "on",
		"cache-type-k": "q8_0",
		"cache-type-v": "q8_0",
	}
	for k, want := range cases {
		if got := pr.Args[k]; got != want {
			t.Errorf("Args[%q] = %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}
	if len(pr.Args) != len(cases) {
		t.Errorf("Args length = %d, want %d (%v)", len(pr.Args), len(cases), pr.Args)
	}
}

func TestApplyToWithSchema_DedupeLegacyShortKey(t *testing.T) {
	schema := llamaTestSchema()
	base := domain.Profile{Args: map[string]any{
		"ngl":          float64(7), // legacy short, resolves to n-gpu-layers
		"n-gpu-layers": float64(11),
	}}
	d := Draft{Essentials: map[string]string{"n-gpu-layers": "33"}}
	pr := d.ApplyToWithSchema(base, schema)

	if _, ok := pr.Args["ngl"]; ok {
		t.Errorf("legacy short key %q should be deduped; got Args=%v", "ngl", pr.Args)
	}
	if got := pr.Args["n-gpu-layers"]; got != float64(33) {
		t.Errorf("n-gpu-layers = %v, want 33 (Essentials wins)", got)
	}
}

func TestApplyToWithSchema_PrecedenceBaseLowestEssentialsHighest(t *testing.T) {
	schema := llamaTestSchema()
	base := domain.Profile{Args: map[string]any{
		"ctx-size": float64(1000),
	}}
	d := Draft{
		Args:       map[string]any{"ctx-size": float64(2000)},
		Essentials: map[string]string{"ctx-size": "4096"},
	}
	pr := d.ApplyToWithSchema(base, schema)
	if got := pr.Args["ctx-size"]; got != float64(4096) {
		t.Errorf("ctx-size = %v, want 4096 (Essentials > Draft.Args > base.Args)", got)
	}
}

func TestApplyToWithSchema_EmptyEssentialOmitted(t *testing.T) {
	schema := llamaTestSchema()
	d := Draft{Essentials: map[string]string{
		"n-gpu-layers": "33",
		"port":         "", // explicitly empty → omit, use backend default
	}}
	pr := d.ApplyToWithSchema(domain.Profile{}, schema)
	if _, ok := pr.Args["port"]; ok {
		t.Errorf("empty Essential should be omitted; got Args[port]=%v", pr.Args["port"])
	}
	if got := pr.Args["n-gpu-layers"]; got != float64(33) {
		t.Errorf("n-gpu-layers = %v, want 33", got)
	}
}

func TestApplyToWithSchema_VLLMEssentials(t *testing.T) {
	d := Draft{Essentials: map[string]string{
		"tensor-parallel-size":   "2",
		"gpu-memory-utilization": "0.9",
		"max-model-len":          "8192",
		"dtype":                  "bfloat16",
		"quantization":           "awq",
		"port":                   "8000",
		"served-model-name":      "my-model",
	}}
	pr := d.ApplyToWithSchema(domain.Profile{}, vllmTestSchema())

	cases := map[string]any{
		"tensor-parallel-size":   float64(2),
		"gpu-memory-utilization": 0.9,
		"max-model-len":          float64(8192),
		"dtype":                  "bfloat16",
		"quantization":           "awq",
		"port":                   float64(8000),
		"served-model-name":      "my-model",
	}
	for k, want := range cases {
		if got := pr.Args[k]; got != want {
			t.Errorf("Args[%q] = %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}
}

func TestApplyToWithSchema_SGLangEssentials(t *testing.T) {
	d := Draft{Essentials: map[string]string{
		"tp-size":             "4",
		"dp-size":             "1",
		"mem-fraction-static": "0.85",
		"dtype":               "float16",
		"context-length":      "16384",
		"port":                "30000",
	}}
	pr := d.ApplyToWithSchema(domain.Profile{}, sglangTestSchema())

	cases := map[string]any{
		"tp-size":             float64(4),
		"dp-size":             float64(1),
		"mem-fraction-static": 0.85,
		"dtype":               "float16",
		"context-length":      float64(16384),
		"port":                float64(30000),
	}
	for k, want := range cases {
		if got := pr.Args[k]; got != want {
			t.Errorf("Args[%q] = %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}
}

func TestApplyToWithSchema_NoSchemaPassthrough(t *testing.T) {
	d := Draft{Essentials: map[string]string{
		"n-gpu-layers": "33",
		"port":         "4321",
		"flash-attn":   "on",
	}}
	pr := d.ApplyToWithSchema(domain.Profile{}, domain.FlagSchema{})

	cases := map[string]string{
		"n-gpu-layers": "33",
		"port":         "4321",
		"flash-attn":   "on",
	}
	for k, want := range cases {
		if got := pr.Args[k]; got != want {
			t.Errorf("Args[%q] = %v (%T), want %q (string pass-through)", k, got, got, want)
		}
	}
}

func TestApplyToWithSchema_EmptyEssentialsNoArgs(t *testing.T) {
	schema := llamaTestSchema()
	d := Draft{Essentials: map[string]string{}}
	pr := d.ApplyToWithSchema(domain.Profile{}, schema)
	if len(pr.Args) != 0 {
		t.Errorf("empty Essentials must not write args; got Args=%v", pr.Args)
	}
}

func TestModelLabel_Llama(t *testing.T) {
	if got := modelLabel(domain.BackendKindLlamaServer); got != "Model path (.gguf)" {
		t.Errorf("modelLabel(llama-server) = %q, want %q", got, "Model path (.gguf)")
	}
}

func TestModelLabel_VLLM(t *testing.T) {
	if got := modelLabel(domain.BackendKindVLLM); got != "Model (HF repo id or local path)" {
		t.Errorf("modelLabel(vllm) = %q, want %q", got, "Model (HF repo id or local path)")
	}
}

func TestModelLabel_SGLang(t *testing.T) {
	if got := modelLabel(domain.BackendKindSGLang); got != "Model (HF repo id or local path)" {
		t.Errorf("modelLabel(sglang) = %q, want %q", got, "Model (HF repo id or local path)")
	}
}

func TestModelLabel_EmptyKind(t *testing.T) {
	if got := modelLabel(""); got != "Model path (.gguf)" {
		t.Errorf("modelLabel(\"\") = %q, want %q", got, "Model path (.gguf)")
	}
}

func TestModelDesc_Llama(t *testing.T) {
	want := "Absolute path to a .gguf file (Ctrl+P to browse)"
	if got := modelDesc(domain.BackendKindLlamaServer); got != want {
		t.Errorf("modelDesc(llama-server) = %q, want %q", got, want)
	}
}

func TestModelDesc_VLLM(t *testing.T) {
	want := "HuggingFace repo id (e.g., meta-llama/Llama-3-8B) or local path (Ctrl+P to browse)"
	if got := modelDesc(domain.BackendKindVLLM); got != want {
		t.Errorf("modelDesc(vllm) = %q, want %q", got, want)
	}
}

func TestModelDesc_SGLang(t *testing.T) {
	want := "HuggingFace repo id (e.g., meta-llama/Llama-3-8B) or local path (Ctrl+P to browse)"
	if got := modelDesc(domain.BackendKindSGLang); got != want {
		t.Errorf("modelDesc(sglang) = %q, want %q", got, want)
	}
}

func TestModelDesc_EmptyKind(t *testing.T) {
	want := "Absolute path to a .gguf file (Ctrl+P to browse)"
	if got := modelDesc(""); got != want {
		t.Errorf("modelDesc(\"\") = %q, want %q", got, want)
	}
}

func TestSchemaRows_HidesLlamaEssentials(t *testing.T) {
	schema := llamaTestSchema()
	rows := schemaRows(schema, nil, domain.BackendKindLlamaServer)
	for _, r := range rows {
		flag := r[0]
		if flag == "n-gpu-layers" || flag == "ctx-size" || flag == "batch-size" || flag == "ubatch-size" || flag == "port" || flag == "flash-attn" || flag == "cache-type-k" || flag == "cache-type-v" {
			t.Errorf("essential flag %q should be hidden from Advanced rows", flag)
		}
	}
}

func TestSchemaRows_HidesVLLMEssentials(t *testing.T) {
	schema := vllmTestSchema()
	rows := schemaRows(schema, nil, domain.BackendKindVLLM)
	for _, r := range rows {
		flag := r[0]
		if flag == "tensor-parallel-size" || flag == "gpu-memory-utilization" || flag == "max-model-len" || flag == "dtype" || flag == "quantization" || flag == "port" || flag == "served-model-name" {
			t.Errorf("essential flag %q should be hidden from Advanced rows", flag)
		}
	}
}

func TestSchemaRows_HidesSGLangEssentials(t *testing.T) {
	schema := sglangTestSchema()
	rows := schemaRows(schema, nil, domain.BackendKindSGLang)
	for _, r := range rows {
		flag := r[0]
		if flag == "tp-size" || flag == "dp-size" || flag == "mem-fraction-static" || flag == "dtype" || flag == "quantization" || flag == "context-length" || flag == "port" || flag == "served-model-name" {
			t.Errorf("essential flag %q should be hidden from Advanced rows", flag)
		}
	}
}

func TestSchemaRows_NonEssentialsStillVisible(t *testing.T) {
	schema := llamaTestSchema()
	rows := schemaRows(schema, nil, domain.BackendKindLlamaServer)
	found := false
	for _, r := range rows {
		if r[0] == "seed" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("non-essential flag 'seed' should still appear in Advanced rows")
	}
}
