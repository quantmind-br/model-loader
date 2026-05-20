package profile_editor

import (
	"reflect"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/llamahelp"
	"github.com/quantmind-br/model-loader/internal/service/sglanghelp"
	"github.com/quantmind-br/model-loader/internal/service/vllmhelp"
)

func newHydrateEditor(t *testing.T, kind domain.BackendKind, schema domain.FlagSchema, d Draft) Editor {
	t.Helper()
	e := New(schema)
	e.backendKind = kind
	e.schema = schema
	dp := d
	e.draft = &dp
	e.openSnapshot = dp
	return e
}

func TestHydrateEssentials_LlamaLegacyKeysPeeled(t *testing.T) {
	schema := llamahelp.EmbeddedSchema()
	d := Draft{
		Name: "X",
		Args: map[string]any{
			"ngl":           float64(80),
			"ctx-size":      float64(16384),
			"unrelated":     "stay",
			"flash-attn":    true,
		},
	}
	e := newHydrateEditor(t, domain.BackendKindLlamaServer, schema, d)
	e = e.hydrateEssentials()

	if got := e.draft.Essentials["n-gpu-layers"]; got != "80" {
		t.Errorf("Essentials[n-gpu-layers] = %q, want 80 (peeled from legacy ngl)", got)
	}
	if got := e.draft.Essentials["ctx-size"]; got != "16384" {
		t.Errorf("Essentials[ctx-size] = %q, want 16384", got)
	}
	if got := e.draft.Essentials["flash-attn"]; got != "on" {
		t.Errorf("Essentials[flash-attn] = %q, want on (bool coerced)", got)
	}
	if _, present := e.draft.Args["ngl"]; present {
		t.Error("Args[ngl] should be deleted after peel")
	}
	if _, present := e.draft.Args["ctx-size"]; present {
		t.Error("Args[ctx-size] should be deleted after peel")
	}
	if _, present := e.draft.Args["flash-attn"]; present {
		t.Error("Args[flash-attn] should be deleted after peel")
	}
	if got := e.draft.Args["unrelated"]; got != "stay" {
		t.Errorf("Args[unrelated] = %v, want stay (non-essential preserved)", got)
	}
}

func TestHydrateEssentials_NilSafe(t *testing.T) {
	schema := llamahelp.EmbeddedSchema()
	d := Draft{Name: "X"}
	e := newHydrateEditor(t, domain.BackendKindLlamaServer, schema, d)
	if e.draft.Essentials != nil {
		t.Fatal("precondition: Essentials should start nil")
	}
	if e.draft.Args != nil {
		t.Fatal("precondition: Args should start nil")
	}

	e = e.hydrateEssentials()

	if e.draft.Essentials == nil {
		t.Error("hydrateEssentials should initialize nil Essentials map")
	}
	if e.draft.Args == nil {
		t.Error("hydrateEssentials should initialize nil Args map")
	}
}

func TestHydrateEssentials_NilDraftDoesNotPanic(t *testing.T) {
	e := Editor{schema: llamahelp.EmbeddedSchema(), backendKind: domain.BackendKindLlamaServer}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("hydrateEssentials with nil draft panicked: %v", r)
		}
	}()
	_ = e.hydrateEssentials()
}

func TestHydrateEssentials_DefaultsSeeded(t *testing.T) {
	schema := llamahelp.EmbeddedSchema()
	d := Draft{Name: "X", Args: map[string]any{}}
	e := newHydrateEditor(t, domain.BackendKindLlamaServer, schema, d)
	e = e.hydrateEssentials()

	wantDefaults := map[string]string{
		"n-gpu-layers": "99",
		"ctx-size":     "8192",
		"batch-size":   "2048",
		"ubatch-size":  "512",
		"flash-attn":   "auto",
		"cache-type-k": "q8_0",
		"cache-type-v": "q8_0",
	}
	for k, want := range wantDefaults {
		if got := e.draft.Essentials[k]; got != want {
			t.Errorf("default Essentials[%s] = %q, want %q", k, got, want)
		}
	}
}

func TestHydrateEssentials_AbsentInSchemaSkipped(t *testing.T) {
	schema := domain.FlagSchema{
		Flags: map[string]domain.FlagSpec{
			"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt},
		},
	}
	d := Draft{
		Name: "X",
		Args: map[string]any{"ngl": float64(50), "ctx-size": float64(4096)},
	}
	e := newHydrateEditor(t, domain.BackendKindLlamaServer, schema, d)
	e = e.hydrateEssentials()

	if got := e.draft.Essentials["ctx-size"]; got != "4096" {
		t.Errorf("Essentials[ctx-size] = %q, want 4096", got)
	}
	if _, present := e.draft.Essentials["n-gpu-layers"]; present {
		t.Error("n-gpu-layers absent from schema: Essentials should not contain it")
	}
	if got := e.draft.Args["ngl"]; got != float64(50) {
		t.Errorf("Args[ngl] = %v, want 50 (untouched when flag is absent from schema)", got)
	}
}

func TestHydrateEssentials_ConflictingKeysPreferCanonical(t *testing.T) {
	schema := llamahelp.EmbeddedSchema()
	d := Draft{
		Name: "X",
		Args: map[string]any{
			"n-gpu-layers": float64(50),
			"ngl":          float64(99),
			"gpu-layers":   float64(77),
		},
	}
	e := newHydrateEditor(t, domain.BackendKindLlamaServer, schema, d)
	e = e.hydrateEssentials()

	if got := e.draft.Essentials["n-gpu-layers"]; got != "50" {
		t.Errorf("Essentials[n-gpu-layers] = %q, want 50 (canonical wins over legacy)", got)
	}
	for _, k := range []string{"n-gpu-layers", "ngl", "gpu-layers"} {
		if _, present := e.draft.Args[k]; present {
			t.Errorf("Args[%s] should be deleted after peel (all candidates removed)", k)
		}
	}
}

func TestHydrateEssentials_VLLMDefaults(t *testing.T) {
	schema := vllmhelp.EmbeddedSchema()
	d := Draft{Name: "X"}
	e := newHydrateEditor(t, domain.BackendKindVLLM, schema, d)
	e = e.hydrateEssentials()

	wantDefaults := map[string]string{
		"tensor-parallel-size":   "1",
		"gpu-memory-utilization": "0.9",
		"dtype":                  "auto",
		"port":                   "8000",
	}
	for k, want := range wantDefaults {
		if got := e.draft.Essentials[k]; got != want {
			t.Errorf("vLLM default Essentials[%s] = %q, want %q", k, got, want)
		}
	}
	if _, ok := e.draft.Essentials["max-model-len"]; ok {
		t.Error("max-model-len has empty Default + AllowEmpty: should NOT be seeded")
	}
	if _, ok := e.draft.Essentials["served-model-name"]; ok {
		t.Error("served-model-name has no Default: should NOT be seeded")
	}
}

func TestHydrateEssentials_SGLangDefaults(t *testing.T) {
	schema := sglanghelp.EmbeddedSchema()
	d := Draft{Name: "X"}
	e := newHydrateEditor(t, domain.BackendKindSGLang, schema, d)
	e = e.hydrateEssentials()

	wantDefaults := map[string]string{
		"tp-size":             "1",
		"dp-size":             "1",
		"mem-fraction-static": "0.9",
		"dtype":               "auto",
		"port":                "30000",
	}
	for k, want := range wantDefaults {
		if got := e.draft.Essentials[k]; got != want {
			t.Errorf("SGLang default Essentials[%s] = %q, want %q", k, got, want)
		}
	}
	if _, ok := e.draft.Essentials["context-length"]; ok {
		t.Error("context-length has empty Default + AllowEmpty: should NOT be seeded")
	}
	if _, ok := e.draft.Essentials["quantization"]; ok {
		t.Error("quantization has no Default: should NOT be seeded")
	}
}

func TestHydrateEssentials_SwitchDoesNotResnapshot(t *testing.T) {
	schema := llamahelp.EmbeddedSchema()
	mkDraft := func() Draft {
		return Draft{Name: "X", Args: map[string]any{"ngl": float64(42)}}
	}
	e := newHydrateEditor(t, domain.BackendKindLlamaServer, schema, mkDraft())
	preSnapshot := e.openSnapshot

	e = e.hydrateEssentialsForSwitch()

	if e.draft.Essentials["n-gpu-layers"] != "42" {
		t.Errorf("hydrateEssentialsForSwitch should still peel: got %q, want 42", e.draft.Essentials["n-gpu-layers"])
	}
	if !reflect.DeepEqual(e.openSnapshot, preSnapshot) {
		t.Errorf("hydrateEssentialsForSwitch must NOT re-snapshot; openSnapshot diverged from pre state")
	}
	if e.openSnapshot.Essentials != nil {
		t.Errorf("openSnapshot.Essentials = %v, want nil (snapshot must remain pre-hydration)", e.openSnapshot.Essentials)
	}

	eRe := newHydrateEditor(t, domain.BackendKindLlamaServer, schema, mkDraft())
	eRe = eRe.hydrateEssentials()
	if eRe.openSnapshot.Essentials == nil {
		t.Error("hydrateEssentials SHOULD re-snapshot; openSnapshot.Essentials still nil")
	}
	if got := eRe.openSnapshot.Essentials["n-gpu-layers"]; got != "42" {
		t.Errorf("re-snapshotted openSnapshot.Essentials[n-gpu-layers] = %q, want 42", got)
	}
}
