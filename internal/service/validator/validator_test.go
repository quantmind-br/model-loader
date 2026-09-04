package validator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
)

func existingModel(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "m.gguf")
	if err := os.WriteFile(p, []byte("g"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestValidator_EmptySchemaProducesNoTypeIssues(t *testing.T) {
	v := New(log.Nop())
	p := domain.Profile{
		ID:    "x",
		Name:  "X",
		Model: existingModel(t),
		Args:  map[string]any{},
	}
	rep := v.Validate(p, domain.FlagSchema{Flags: map[string]domain.FlagSpec{}}, domain.BackendKindLlamaServer)
	// At this stage, with no schema and no rules wired, Errors should be empty.
	if len(rep.Errors) != 0 {
		t.Errorf("Errors=%v, want empty", rep.Errors)
	}
}

func TestValidator_TypeRule(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"ctx-size":   {Long: "ctx-size", Type: domain.FlagTypeInt},
		"flash-attn": {Long: "flash-attn", Type: domain.FlagTypeEnum, EnumValues: []string{"on", "off", "auto"}},
		"mlock":      {Long: "mlock", Type: domain.FlagTypeBool},
	}}
	cases := []struct {
		name      string
		args      map[string]any
		wantErrs  int
		wantField string
	}{
		{"int ok as float64", map[string]any{"ctx-size": float64(4096)}, 0, ""},
		{"int ok as int", map[string]any{"ctx-size": 4096}, 0, ""},
		{"int rejects string", map[string]any{"ctx-size": "abc"}, 1, "ctx-size"},
		{"int rejects fractional float64", map[string]any{"ctx-size": float64(4096.5)}, 1, "ctx-size"},
		{"enum ok", map[string]any{"flash-attn": "on"}, 0, ""},
		{"enum rejects unknown", map[string]any{"flash-attn": "maybe"}, 1, "flash-attn"},
		{"bool ok", map[string]any{"mlock": true}, 0, ""},
		{"bool rejects string", map[string]any{"mlock": "yes"}, 1, "mlock"},
		{"unknown flag is error", map[string]any{"unheard-of": 1}, 1, "unheard-of"},
	}
	v := New(log.Nop())
	model := existingModel(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Model: model, Args: tc.args}
			rep := v.Validate(p, schema, domain.BackendKindLlamaServer)
			if got := len(rep.Errors); got != tc.wantErrs {
				t.Fatalf("Errors=%d (%v), want %d", got, rep.Errors, tc.wantErrs)
			}
			if tc.wantErrs > 0 && rep.Errors[0].Field != tc.wantField {
				t.Errorf("Errors[0].Field=%q, want %q", rep.Errors[0].Field, tc.wantField)
			}
		})
	}
}

func TestValidator_IntAllowedValueBeforeMin(t *testing.T) {
	min := 1
	sch := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"sleep-idle-seconds": {
			Long:        "sleep-idle-seconds",
			Type:        domain.FlagTypeInt,
			Min:         &min,
			AllowedInts: []int{-1},
		},
	}}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(`{"sleep-idle-seconds":-1}`), &decoded); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args map[string]any
		want int
	}{
		{name: "native allowed integer", args: map[string]any{"sleep-idle-seconds": -1}, want: 0},
		{name: "json decoded allowed integer", args: decoded, want: 0},
		{name: "positive integer", args: map[string]any{"sleep-idle-seconds": 1}, want: 0},
		{name: "zero below minimum", args: map[string]any{"sleep-idle-seconds": 0}, want: 1},
		{name: "other negative below minimum", args: map[string]any{"sleep-idle-seconds": -2}, want: 1},
		{name: "fractional value", args: map[string]any{"sleep-idle-seconds": -1.5}, want: 1},
	}
	v := New(log.Nop())
	model := existingModel(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := v.Validate(domain.Profile{ID: "x", Model: model, Args: tc.args}, sch, domain.BackendKindLlamaServer)
			if got := len(rep.Errors); got != tc.want {
				t.Fatalf("Errors=%d (%v), want %d", got, rep.Errors, tc.want)
			}
		})
	}
}

func TestValidator_ModelExistence(t *testing.T) {
	tmp := t.TempDir()
	existing := tmp + "/m.gguf"
	if err := os.WriteFile(existing, []byte("g"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := New(log.Nop())
	cases := []struct {
		name     string
		model    string
		wantErrs int
	}{
		{"empty path is a required error", "", 1},
		{"existing path no error", existing, 0},
		{"missing path errors", tmp + "/nope.gguf", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Model: tc.model}
			rep := v.Validate(p, domain.FlagSchema{}, domain.BackendKindLlamaServer)
			if got := len(rep.Errors); got != tc.wantErrs {
				t.Errorf("Errors=%d (%v), want %d", got, rep.Errors, tc.wantErrs)
			}
		})
	}
}

func TestValidator_HFRepoIDNoExistenceError(t *testing.T) {
	v := New(log.Nop())
	cases := []struct {
		name    string
		model   string
		wantErr bool
	}{
		{"dotted HF repo ID rejected for llama-server", "Qwen/Qwen2.5-7B-Instruct", true},
		{"dotted HF repo ID 2 rejected for llama-server", "meta-llama/Llama-3.1-8B-Instruct", true},
		{"local gguf file missing", "models/model.gguf", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Model: tc.model}
			rep := v.Validate(p, domain.FlagSchema{}, domain.BackendKindLlamaServer)
			gotErr := len(rep.Errors) > 0
			if gotErr != tc.wantErr {
				t.Errorf("Errors=%v, wantErr=%v", rep.Errors, tc.wantErr)
			}
		})
	}
}

func TestValidator_HFRepoIDAllowedForVLLM(t *testing.T) {
	v := New(log.Nop())
	cases := []struct {
		name    string
		model   string
		wantErr bool
	}{
		{"dotted HF repo ID allowed for vLLM", "Qwen/Qwen2.5-7B-Instruct", false},
		{"local gguf file missing still errors", "models/model.gguf", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Model: tc.model}
			rep := v.Validate(p, domain.FlagSchema{}, domain.BackendKindVLLM)
			gotErr := len(rep.Errors) > 0
			if gotErr != tc.wantErr {
				t.Errorf("Errors=%v, wantErr=%v", rep.Errors, tc.wantErr)
			}
		})
	}
}

// LM Studio addresses models by daemon-side key that `lms load` resolves
// server-side; neither bare keys nor HF-style IDs may trip the existence rule.
func TestValidator_LMStudioKeyNoExistenceError(t *testing.T) {
	v := New(log.Nop())
	cases := []struct {
		name  string
		model string
	}{
		{"bare daemon key", "qwen3-coder-30b"},
		{"HF-style repo ID", "lmstudio-community/Qwen3-30B-GGUF"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Model: tc.model}
			rep := v.Validate(p, domain.FlagSchema{}, domain.BackendKindLMStudio)
			if len(rep.Errors) > 0 {
				t.Errorf("Errors=%v, want none for lmstudio model key", rep.Errors)
			}
		})
	}
}

func TestValidator_HFRepoIDErrorsForLlamaServer(t *testing.T) {
	v := New(log.Nop())
	cases := []struct {
		name    string
		model   string
		wantErr bool
	}{
		{"dotted HF repo ID", "Qwen/Qwen2.5-7B-Instruct", true},
		{"dotted HF repo ID 2", "meta-llama/Llama-3.1-8B-Instruct", true},
		{"local gguf file missing", "models/model.gguf", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Model: tc.model}
			rep := v.Validate(p, domain.FlagSchema{}, domain.BackendKindLlamaServer)
			gotErr := len(rep.Errors) > 0
			if gotErr != tc.wantErr {
				t.Errorf("Errors=%v, wantErr=%v", rep.Errors, tc.wantErr)
			}
		})
	}
}

func TestValidator_HFRepoIDAllowedForUnsloth(t *testing.T) {
	v := New(log.Nop())
	cases := []struct {
		name    string
		model   string
		wantErr bool
	}{
		{"dotted HF repo ID allowed for unsloth", "Qwen/Qwen2.5-7B-Instruct", false},
		{"local gguf file missing still errors", "models/model.gguf", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Model: tc.model}
			rep := v.Validate(p, domain.FlagSchema{}, domain.BackendKindUnsloth)
			gotErr := len(rep.Errors) > 0
			if gotErr != tc.wantErr {
				t.Errorf("Errors=%v, wantErr=%v", rep.Errors, tc.wantErr)
			}
		})
	}
}

// FreeToken's --model takes a local dir, an FTW dir, or a hub repo id
// (server/args.py; --model-source picks huggingface or modelscope), so a bare
// repo id must validate while a missing local path still errors.
func TestValidator_HFRepoIDAllowedForFreeToken(t *testing.T) {
	v := New(log.Nop())
	cases := []struct {
		name    string
		model   string
		wantErr bool
	}{
		{"dotted HF repo ID allowed for freetoken", "Qwen/Qwen3.6-35B-A3B", false},
		{"nvidia NVFP4 repo ID allowed", "nvidia/GLM-5.2-NVFP4", false},
		// Absolute, so LooksLikeHFRepo rejects it up front: a real local
		// checkpoint path that does not exist still has to fail.
		{"local checkpoint dir missing still errors", "/nonexistent/models/Qwen3.6-35B-A3B", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Model: tc.model}
			rep := v.Validate(p, domain.FlagSchema{}, domain.BackendKindFreeToken)
			gotErr := len(rep.Errors) > 0
			if gotErr != tc.wantErr {
				t.Errorf("Errors=%v, wantErr=%v", rep.Errors, tc.wantErr)
			}
		})
	}
}

func TestValidator_ExistingLocalPathNotTreatedAsHFRepo(t *testing.T) {
	tmp := t.TempDir()
	existingDir := tmp + "/models"
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	v := New(log.Nop())
	// A local path like "models/llama3" that exists should be validated
	// as a local path, not treated as a HF repo ID.
	p := domain.Profile{ID: "x", Model: existingDir}
	rep := v.Validate(p, domain.FlagSchema{}, domain.BackendKindLlamaServer)
	if len(rep.Errors) != 0 {
		t.Errorf("existing local dir should not error; got %v", rep.Errors)
	}
}

func TestValidate_RequiredFlagMissing(t *testing.T) {
	sch := domain.FlagSchema{
		Flags: map[string]domain.FlagSpec{
			"port": {Long: "port", Type: domain.FlagTypeInt, Required: true},
		},
	}
	p := domain.Profile{Model: existingModel(t), Args: map[string]any{}} // port absent
	rep := New(nil).Validate(p, sch, domain.BackendKindLlamaServer)
	if !rep.HasBlockingErrors() {
		t.Fatalf("expected error for missing required flag")
	}
}

func TestValidate_RequiredFlagPresent(t *testing.T) {
	sch := domain.FlagSchema{
		Flags: map[string]domain.FlagSpec{
			"port": {Long: "port", Type: domain.FlagTypeInt, Required: true},
		},
	}
	p := domain.Profile{Model: existingModel(t), Args: map[string]any{"port": 4321}}
	rep := New(nil).Validate(p, sch, domain.BackendKindLlamaServer)
	if rep.HasBlockingErrors() {
		t.Fatalf("unexpected errors: %+v", rep.Errors)
	}
}

func TestValidate_RequiredFlagInExtraArgs(t *testing.T) {
	sch := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"port": {Long: "port", Type: domain.FlagTypeInt, Required: true},
	}}
	p := domain.Profile{Model: existingModel(t), Args: map[string]any{}, ExtraArgs: []string{"--port=8080"}}
	rep := New(nil).Validate(p, sch, domain.BackendKindLlamaServer)
	if rep.HasBlockingErrors() {
		t.Fatalf("unexpected errors: %+v", rep.Errors)
	}
}

// TestValidator_ListEnum pins BUGS.md S1 fix #1: a FlagSpec with List=true
// models a comma-separated list of enum values (llama-server --spec-type). The
// validator splits on "," and validates each element; a single value and a JSON
// array are accepted too. A scalar (List=false) enum must still reject a
// comma-list as a whole.
func TestValidator_ListEnum(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"spec-type":  {Long: "spec-type", Type: domain.FlagTypeEnum, List: true, EnumValues: []string{"none", "draft-mtp", "draft-dflash", "ngram-mod", "ngram-cache"}},
		"flash-attn": {Long: "flash-attn", Type: domain.FlagTypeEnum, EnumValues: []string{"on", "off", "auto"}},
	}}
	cases := []struct {
		name      string
		args      map[string]any
		wantErrs  int
		wantField string
	}{
		{"list enum single value ok", map[string]any{"spec-type": "draft-mtp"}, 0, ""},
		{"list enum comma pair ok", map[string]any{"spec-type": "draft-mtp,ngram-mod"}, 0, ""},
		{"list enum spaced comma pair ok", map[string]any{"spec-type": "draft-mtp, ngram-mod"}, 0, ""},
		{"list enum trailing space ok", map[string]any{"spec-type": "draft-mtp "}, 0, ""},
		{"list enum double comma ok", map[string]any{"spec-type": "draft-mtp,,ngram-mod"}, 0, ""},
		{"list enum new value draft-dflash ok", map[string]any{"spec-type": "draft-dflash"}, 0, ""},
		{"list enum chained ok", map[string]any{"spec-type": "draft-mtp,ngram-mod,ngram-cache"}, 0, ""},
		{"list enum array any form ok", map[string]any{"spec-type": []any{"draft-mtp", "ngram-mod"}}, 0, ""},
		{"list enum array string form ok", map[string]any{"spec-type": []string{"draft-mtp", "ngram-mod"}}, 0, ""},
		{"list enum rejects unknown element", map[string]any{"spec-type": "draft-mtp,bogus"}, 1, "spec-type"},
		{"list enum rejects empty string", map[string]any{"spec-type": ""}, 1, "spec-type"},
		{"list enum rejects only comma", map[string]any{"spec-type": ","}, 1, "spec-type"},
		{"scalar enum still rejects comma list", map[string]any{"flash-attn": "on,off"}, 1, "flash-attn"},
		{"scalar enum still rejects unknown", map[string]any{"flash-attn": "maybe"}, 1, "flash-attn"},
	}
	v := New(log.Nop())
	model := existingModel(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Model: model, Args: tc.args}
			rep := v.Validate(p, schema, domain.BackendKindLlamaServer)
			if got := len(rep.Errors); got != tc.wantErrs {
				t.Fatalf("Errors=%d (%v), want %d", got, rep.Errors, tc.wantErrs)
			}
			if tc.wantErrs > 0 && rep.Errors[0].Field != tc.wantField {
				t.Errorf("Errors[0].Field=%q, want %q", rep.Errors[0].Field, tc.wantField)
			}
		})
	}
}

// TestValidate_ExtraArgsKnownFlagPassthrough pins BUGS.md S1 fix #2: extraArgs is
// a raw passthrough, so a KNOWN flag whose curated enum is stale (e.g. a newer
// spec-type value or a comma-list) must NOT block validation. Only unknown flags
// warn; a bare token (no --) still errors.
func TestValidate_ExtraArgsKnownFlagPassthrough(t *testing.T) {
	// Stale schema mirroring the S1 condition: spec-type lacks draft-dflash and
	// is scalar (no List), yet the binary accepts the value.
	sch := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"spec-type": {Long: "spec-type", Type: domain.FlagTypeEnum, EnumValues: []string{"none", "draft-mtp", "ngram-mod"}},
		"mlock":     {Long: "mlock", Type: domain.FlagTypeBool},
	}}
	cases := []struct {
		name      string
		extra     []string
		wantErrs  int
		wantWarns int
	}{
		{"known stale enum value passes", []string{"--spec-type", "draft-dflash"}, 0, 0},
		{"known comma-list value passes", []string{"--spec-type=draft-mtp,ngram-mod"}, 0, 0},
		{"known spaced comma-list passes", []string{"--spec-type", "draft-mtp, ngram-mod"}, 0, 0},
		{"known flag value via equals passes", []string{"--spec-type=draft-dflash"}, 0, 0},
		{"known bool flag passes", []string{"--mlock"}, 0, 0},
		{"known bool flag with equals value passes", []string{"--mlock=true"}, 0, 0},
		{"known bool flag with space value passes", []string{"--mlock", "true"}, 0, 0},
		{"known non-bool trailing value-less passes", []string{"--spec-type"}, 0, 0},
		{"value token consumed not treated as bare", []string{"--spec-type", "draft-dflash", "--mlock"}, 0, 0},
		{"unknown flag warns only", []string{"--totally-fake-flag", "1"}, 0, 1},
		{"bare value still errors", []string{"bare-token"}, 1, 0},
	}
	v := New(log.Nop())
	model := existingModel(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Model: model, Args: map[string]any{}, ExtraArgs: tc.extra}
			rep := v.Validate(p, sch, domain.BackendKindLlamaServer)
			if got := len(rep.Errors); got != tc.wantErrs {
				t.Fatalf("Errors=%d (%v), want %d", got, rep.Errors, tc.wantErrs)
			}
			if got := len(rep.Warnings); got != tc.wantWarns {
				t.Errorf("Warnings=%d (%v), want %d", got, rep.Warnings, tc.wantWarns)
			}
		})
	}
}

// UIUX-031: an empty model must block. Whitespace-only counts as empty; it also
// trips the existence rule (applyExistenceRules only early-returns on ""), so
// assert on the presence of the required issue, not on the error count.
func TestValidate_ModelRequired(t *testing.T) {
	v := New(log.Nop())
	cases := []struct {
		name  string
		model string
		want  bool
	}{
		{"empty", "", true},
		{"spaces only", "   ", true},
		{"tab and newline", "\t\n", true},
		{"set", existingModel(t), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := v.Validate(domain.Profile{ID: "x", Model: tc.model},
				domain.FlagSchema{}, domain.BackendKindLlamaServer)
			var got bool
			for _, e := range rep.Errors {
				if e.Field == "model" && e.Message == "required" {
					got = true
				}
			}
			if got != tc.want {
				t.Fatalf("model=%q required-error=%v, want %v (errors: %+v)", tc.model, got, tc.want, rep.Errors)
			}
		})
	}
}

// S15: a flag whose value spans several argv tokens cannot be emitted from
// Args (one token per key), so it must be refused there and accepted — with
// all its bare values — in the verbatim extraArgs passthrough.
func TestValidate_MultiTokenFlagArity(t *testing.T) {
	sch := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"control-vector-layer-range": {Long: "control-vector-layer-range", Type: domain.FlagTypeString, Arity: 2},
		"ctx-size":                   {Long: "ctx-size", Type: domain.FlagTypeInt},
	}}
	v := New(log.Nop())
	model := existingModel(t)
	tests := []struct {
		name      string
		args      map[string]any
		extra     []string
		wantErrs  int
		wantWarns int
	}{
		{
			name:     "args rejects a multi-token flag",
			args:     map[string]any{"control-vector-layer-range": "0 31"},
			wantErrs: 1,
		},
		{
			name:  "extra args accepts both values",
			extra: []string{"--control-vector-layer-range", "0", "31"},
		},
		{
			name:  "following flag after both values still parses",
			extra: []string{"--control-vector-layer-range", "0", "31", "--mlock"},
			// --mlock is absent from this schema, so it warns but must not error.
			wantWarns: 1,
		},
		{
			name:     "a third bare value is still a stray token",
			extra:    []string{"--control-vector-layer-range", "0", "31", "99"},
			wantErrs: 1,
		},
		{
			name: "single-token flags keep consuming exactly one value",
			args: map[string]any{"ctx-size": 4096},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.args
			if args == nil {
				args = map[string]any{}
			}
			p := domain.Profile{ID: "x", Model: model, Args: args, ExtraArgs: tt.extra}
			rep := v.Validate(p, sch, domain.BackendKindLlamaServer)
			if got := len(rep.Errors); got != tt.wantErrs {
				t.Fatalf("Errors=%d (%v), want %d", got, rep.Errors, tt.wantErrs)
			}
			if got := len(rep.Warnings); got != tt.wantWarns {
				t.Errorf("Warnings=%d (%v), want %d", got, rep.Warnings, tt.wantWarns)
			}
		})
	}
}
