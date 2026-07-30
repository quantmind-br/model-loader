package backendschema

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/validator"
)

func existingModelPath(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "m.gguf")
	if err := os.WriteFile(p, []byte("g"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestCuratedLlama_ValidatesMTPProfile guards that the curated llama-server
// schema covers the flags a real MTP profile uses. These are all standard
// llama-server flags; an earlier curation gap left 7 of them out and typed
// n-gpu-layers as string, so numeric profiles failed validation with 8 errors.
func TestCuratedLlama_ValidatesMTPProfile(t *testing.T) {
	schema := CuratedLlamaSchema().ToFlagSchema()

	p := domain.Profile{
		Model: existingModelPath(t),
		Args: map[string]any{
			"batch-size":       float64(2048),
			"cache-type-k":     "q4_0",
			"cache-type-v":     "q4_0",
			"cpu-mask":         "0x00000003",
			"cpu-strict":       float64(1),
			"ctx-size":         float64(200000),
			"flash-attn":       "on",
			"host":             "127.0.0.1",
			"jinja":            true,
			"n-gpu-layers":     float64(99),
			"parallel":         float64(1),
			"port":             float64(4330),
			"reasoning-budget": float64(2048),
			"spec-draft-n-max": float64(3),
			"spec-type":        "draft-mtp",
			"threads":          float64(2),
			"threads-batch":    float64(8),
			"ubatch-size":      float64(1024),
		},
		ExtraArgs: []string{
			"--ctx-checkpoints", "8",
			"--spec-type", "draft-mtp",
			"--no-mmap",
			"--no-webui",
			"--no-perf",
		},
	}

	rep := validator.New(nil).Validate(p, schema, schema.BackendKind)
	if rep.HasBlockingErrors() {
		t.Fatalf("expected curated schema to validate MTP profile, got %d errors: %+v", len(rep.Errors), rep.Errors)
	}
}

// TestCuratedLlama_NGPULayersIsInt pins n-gpu-layers to int so numeric profile
// values validate. All real profiles store it as a number.
func TestCuratedLlama_NGPULayersIsInt(t *testing.T) {
	schema := CuratedLlamaSchema().ToFlagSchema()
	spec, ok := schema.Lookup("n-gpu-layers")
	if !ok {
		t.Fatal("n-gpu-layers missing from curated schema")
	}
	if spec.Type != domain.FlagTypeInt {
		t.Fatalf("n-gpu-layers should be FlagTypeInt, got %v", spec.Type)
	}
}

func TestMergeWithCurated_PreservesParsedSpecDraftModelAlias(t *testing.T) {
	full := domain.BackendValidationSchema{
		Flags: map[string]domain.FlagSpec{
			"model-draft": {
				Long:    "model-draft",
				Short:   "md",
				Aliases: []string{"spec-draft-model", "model-draft"},
				Type:    domain.FlagTypeString,
			},
		},
	}
	merged := mergeWithCurated(full, CuratedLlamaSchema()).ToFlagSchema()
	if _, ok := merged.Lookup("spec-draft-model"); !ok {
		t.Fatal("spec-draft-model alias missing after curated merge")
	}
}

// The v10152 golden adds four CORS flags. This asserts the merged fallback
// schema (embedded golden overlaid with the curated overlay) carries the
// source-true facts operators actually see. If a future golden refresh drops or
// mangles these (e.g. cors-methods default truncated to "GET" by the comma-stop
// in defaultRe), this fails loudly rather than silently shipping a wrong schema.
func TestMergeWithCurated_CorsFlagsSourceTrue(t *testing.T) {
	golden, err := loadGoldenSchema()
	if err != nil {
		t.Fatalf("loadGoldenSchema: %v", err)
	}
	full := domain.BackendValidationSchema{Flags: golden.Flags}
	merged := mergeWithCurated(full, CuratedLlamaSchema())

	tests := []struct {
		flag string
		def  any
	}{
		{"cors-origins", "*"},
		{"cors-methods", "GET, POST, DELETE, OPTIONS"},
		{"cors-headers", "*"},
		{"cors-credentials", true},
	}
	for _, tc := range tests {
		t.Run(tc.flag, func(t *testing.T) {
			spec, ok := merged.Flags[tc.flag]
			if !ok {
				t.Fatalf("%s missing from merged fallback schema", tc.flag)
			}
			if spec.Default != tc.def {
				t.Fatalf("%s default = %v (%T), want %v", tc.flag, spec.Default, spec.Default, tc.def)
			}
		})
	}
}

// S8: mergeWithCuratedEnrich must never inject curated-only flags — the parsed
// backend surface is authoritative on flag existence. A fork trailing upstream
// must not inherit flags its binary rejects at launch, while a parsed-only fork
// flag (kv-mean-center) survives.
func TestMergeWithCuratedEnrich_DropsAbsentCuratedFlags(t *testing.T) {
	full := domain.BackendValidationSchema{
		Flags: map[string]domain.FlagSpec{
			"ctx-size":       {Long: "ctx-size", Short: "c", Type: domain.FlagTypeInt},
			"kv-mean-center": {Long: "kv-mean-center", Type: domain.FlagTypeString, HelpText: "K-cache mean-centering bias file", Group: "common"},
		},
	}
	merged := mergeWithCuratedEnrich(full, CuratedLlamaSchema())

	for _, absent := range []string{"cors-origins", "cors-methods", "cors-headers", "cors-credentials", "reasoning-preserve", "mtmd-batch-max-tokens"} {
		if _, ok := merged.Flags[absent]; ok {
			t.Errorf("curated-only flag %q was injected; enrich must not append", absent)
		}
	}
	if _, ok := merged.Flags["kv-mean-center"]; !ok {
		t.Error("parsed-only fork flag kv-mean-center dropped")
	}
	// Enrichment still applies: ctx-size gains its curated HelpText/Group.
	if spec := merged.Flags["ctx-size"]; spec.Group == "" {
		t.Error("ctx-size missing curated Group after enrich")
	}
}

// mergeWithCurated (append-missing) must still inject curated-only flags, which
// the golden fallback and Bee/Buun fork overlays rely on.
func TestMergeWithCurated_AppendsMissingCuratedFlags(t *testing.T) {
	full := domain.BackendValidationSchema{
		Flags: map[string]domain.FlagSpec{
			"ctx-size": {Long: "ctx-size", Short: "c", Type: domain.FlagTypeInt},
		},
	}
	merged := mergeWithCurated(full, CuratedLlamaSchema())
	if _, ok := merged.Flags["cors-origins"]; !ok {
		t.Error("append-missing merge dropped curated-only flag cors-origins")
	}
}

// S10: the curated defaults for the two flags llama.cpp overrides per-example
// must match LLAMA_EXAMPLE_SERVER, not the generic common_params struct.
// common.h sets use_jinja=true and arg.cpp disables it only for
// COMPLETION/MTMD; arg.cpp overrides n_parallel=-1 (auto) for the server.
func TestCuratedLlama_ServerExampleDefaults(t *testing.T) {
	schema := CuratedLlamaSchema().ToFlagSchema()

	tests := []struct {
		flag string
		def  any
	}{
		{"jinja", true},
		{"parallel", -1},
	}
	for _, tc := range tests {
		t.Run(tc.flag, func(t *testing.T) {
			spec, ok := schema.Lookup(tc.flag)
			if !ok {
				t.Fatalf("%s missing from curated schema", tc.flag)
			}
			if spec.Default != tc.def {
				t.Fatalf("%s default = %v (%T), want %v", tc.flag, spec.Default, spec.Default, tc.def)
			}
		})
	}
}

// S9: llama.cpp's two "-ngl" flags parse "auto" as -1 and "all" as -2 but
// otherwise require an integer (common/arg.cpp:2638 and :4009), so neither a
// plain int nor a plain string models them. They are ints carrying
// FlagSpec.Keywords; both spellings must validate while junk still fails.
func TestCuratedLlama_NGPULayerFlagsAcceptKeywordsAndInts(t *testing.T) {
	schema := CuratedLlamaSchema().ToFlagSchema()

	tests := []struct {
		val  any
		want bool // true => must validate
	}{
		{"auto", true},
		{"all", true},
		{-1, true},
		{-2, true},
		{float64(99), true},
		{"banana", false}, // unlisted keyword
		{"42", false},     // numbers must stay numbers
		{-3, false},       // below the "all" floor
	}
	for _, flag := range []string{"n-gpu-layers", "spec-draft-ngl"} {
		spec, ok := schema.Lookup(flag)
		if !ok {
			t.Fatalf("%s missing from curated schema", flag)
		}
		if spec.Type != domain.FlagTypeInt {
			t.Errorf("%s should be FlagTypeInt, got %v", flag, spec.Type)
		}
		for _, tc := range tests {
			t.Run(flag+"/"+fmt.Sprint(tc.val), func(t *testing.T) {
				p := domain.Profile{Model: existingModelPath(t), Args: map[string]any{flag: tc.val}}
				rep := validator.New(nil).Validate(p, schema, schema.BackendKind)
				if got := !rep.HasBlockingErrors(); got != tc.want {
					t.Fatalf("%s=%v valid=%v, want %v (errors: %+v)", flag, tc.val, got, tc.want, rep.Errors)
				}
			})
		}
	}
}

// S9: Keywords must survive the curated overlay, otherwise the live-parse path
// (which types both -ngl flags as plain ints from --help) keeps rejecting
// "auto"/"all". The draft flag merges under the parsed canonical name
// n-gpu-layers-draft via its alias, so cover that shape specifically.
func TestMergeWithCuratedEnrich_PropagatesNGPULayerKeywords(t *testing.T) {
	full := domain.BackendValidationSchema{
		Flags: map[string]domain.FlagSpec{
			"n-gpu-layers": {Long: "n-gpu-layers", Short: "ngl", Type: domain.FlagTypeInt},
			"n-gpu-layers-draft": {
				Long:    "n-gpu-layers-draft",
				Short:   "ngld",
				Aliases: []string{"spec-draft-ngl", "gpu-layers-draft"},
				Type:    domain.FlagTypeInt,
			},
		},
	}
	merged := mergeWithCuratedEnrich(full, CuratedLlamaSchema()).ToFlagSchema()

	for _, flag := range []string{"n-gpu-layers", "spec-draft-ngl"} {
		spec, ok := merged.Lookup(flag)
		if !ok {
			t.Fatalf("%s missing after enrich merge", flag)
		}
		if len(spec.Keywords) == 0 {
			t.Fatalf("%s lost Keywords in enrich merge", flag)
		}
		p := domain.Profile{Model: existingModelPath(t), Args: map[string]any{flag: "auto"}}
		if rep := validator.New(nil).Validate(p, merged, merged.BackendKind); rep.HasBlockingErrors() {
			t.Fatalf("%s=auto rejected after enrich merge: %+v", flag, rep.Errors)
		}
	}
}

// llamaFamily are the four curated schemas descended from llama.cpp's
// common/arg.cpp, which therefore share flag facts.
func llamaFamily() map[string]domain.BackendValidationSchema {
	return map[string]domain.BackendValidationSchema{
		"llama-server":   CuratedLlamaSchema(),
		"beellama-cpp":   CuratedBeeLlamaSchema(),
		"buun-llama-cpp": CuratedBuunSchema(),
		"ik-llama-cpp":   CuratedIkLlamaSchema(),
	}
}

// S12: every llama.cpp descendant declares `int32_t n_ctx = 0` ("context the
// model was trained with") and `rope_scaling_type = UNSPECIFIED`. A curated
// ctx-size of 4096 was fit_params_min_ctx (the --fit-ctx default) bleeding in,
// and a rope-scaling default of "none" is a different thing from unset: "none"
// explicitly disables scaling, unset defers to the model.
func TestCuratedLlamaFamily_ContextAndRopeDefaults(t *testing.T) {
	for kind, schema := range llamaFamily() {
		t.Run(kind, func(t *testing.T) {
			fs := schema.ToFlagSchema()
			if spec, ok := fs.Lookup("ctx-size"); !ok {
				t.Error("ctx-size missing")
			} else if spec.Default != 0 {
				t.Errorf("ctx-size default = %v (%T), want 0", spec.Default, spec.Default)
			}
			if spec, ok := fs.Lookup("rope-scaling"); !ok {
				t.Error("rope-scaling missing")
			} else if spec.Default != nil {
				t.Errorf("rope-scaling default = %v, want nil (unset defers to the model)", spec.Default)
			}
		})
	}
}

// S11: the -ngl pair takes an exact integer, "auto" (-1) or "all" (-2) in
// upstream llama.cpp and in the BeeLlama/Buun forks, so all three curate them
// as ints carrying Keywords. ik_llama.cpp is the old-format common/common.cpp
// parser with no keyword spellings, so it must NOT gain them. Note --n-cpu-moe
// is a plain int everywhere (it throws on value < 0) and is covered here to
// stop a future sync from mistaking it for a keyword flag.
func TestCuratedLlamaFamily_NGPULayerKeywords(t *testing.T) {
	keywordKinds := map[string]bool{"llama-server": true, "beellama-cpp": true, "buun-llama-cpp": true}

	for kind, schema := range llamaFamily() {
		fs := schema.ToFlagSchema()
		for _, flag := range []string{"n-gpu-layers", "spec-draft-ngl"} {
			spec, ok := fs.Lookup(flag)
			if !ok {
				continue // ik-llama-cpp has no draft-ngl flag
			}
			t.Run(kind+"/"+flag, func(t *testing.T) {
				if !keywordKinds[kind] {
					if len(spec.Keywords) != 0 {
						t.Fatalf("%s must not accept keywords: %v", flag, spec.Keywords)
					}
					return
				}
				if spec.Type != domain.FlagTypeInt {
					t.Fatalf("%s type = %v, want int", flag, spec.Type)
				}
				p := domain.Profile{Model: existingModelPath(t), Args: map[string]any{flag: "auto"}}
				if rep := validator.New(nil).Validate(p, fs, fs.BackendKind); rep.HasBlockingErrors() {
					t.Fatalf("%s=auto rejected: %+v", flag, rep.Errors)
				}
				p = domain.Profile{Model: existingModelPath(t), Args: map[string]any{flag: float64(99)}}
				if rep := validator.New(nil).Validate(p, fs, fs.BackendKind); rep.HasBlockingErrors() {
					t.Fatalf("%s=99 rejected: %+v", flag, rep.Errors)
				}
			})
		}
		if spec, ok := fs.Lookup("n-cpu-moe"); ok && len(spec.Keywords) != 0 {
			t.Errorf("%s: n-cpu-moe is a plain int (throws on value < 0), got keywords %v", kind, spec.Keywords)
		}
	}
}

// S13: --mirostat is `params.mirostat = std::stoi(...)` in every llama.cpp
// descendant (the live --help parser already types it Int), and ik's --mla-use
// is `params.mla_attn = std::stoi(...)`. Curating them as string enums made the
// validator reject their own declared values for any profile carrying a bare
// JSON number, because enumParts only accepts strings.
func TestCuratedLlamaFamily_NumericEnumsAreInts(t *testing.T) {
	type bound struct {
		flag     string
		def      int
		max      int
		rejected float64
	}
	perFlag := map[string][]bound{
		"llama-server":   {{"mirostat", 0, 2, 3}},
		"beellama-cpp":   {{"mirostat", 0, 2, 3}},
		"buun-llama-cpp": {{"mirostat", 0, 2, 3}},
		"ik-llama-cpp":   {{"mirostat", 0, 2, 3}, {"mla-use", 3, 3, 4}},
	}

	for kind, schema := range llamaFamily() {
		fs := schema.ToFlagSchema()
		for _, b := range perFlag[kind] {
			t.Run(kind+"/"+b.flag, func(t *testing.T) {
				spec, ok := fs.Lookup(b.flag)
				if !ok {
					t.Fatalf("%s missing from curated schema", b.flag)
				}
				if spec.Type != domain.FlagTypeInt {
					t.Fatalf("%s type = %v, want int", b.flag, spec.Type)
				}
				if spec.Default != b.def {
					t.Fatalf("%s default = %v (%T), want %d", b.flag, spec.Default, spec.Default, b.def)
				}
				if spec.Min == nil || *spec.Min != 0 || spec.Max == nil || *spec.Max != b.max {
					t.Fatalf("%s bounds = %v..%v, want 0..%d", b.flag, spec.Min, spec.Max, b.max)
				}
				// The declared default must validate both as a Go int (schema
				// default surfaced into a profile) and as a float64 (what
				// encoding/json yields for a hand-edited profile).
				for _, val := range []any{b.def, float64(b.def), b.max, float64(b.max)} {
					p := domain.Profile{Model: existingModelPath(t), Args: map[string]any{b.flag: val}}
					if rep := validator.New(nil).Validate(p, fs, fs.BackendKind); rep.HasBlockingErrors() {
						t.Fatalf("%s=%v (%T) rejected: %+v", b.flag, val, val, rep.Errors)
					}
				}
				p := domain.Profile{Model: existingModelPath(t), Args: map[string]any{b.flag: b.rejected}}
				if rep := validator.New(nil).Validate(p, fs, fs.BackendKind); !rep.HasBlockingErrors() {
					t.Fatalf("%s=%v should be out of range", b.flag, b.rejected)
				}
			})
		}
	}
}

// S15: arity is a per-fork fact, so the curated catalogs must disagree where the
// binaries do. llama.cpp/beellama/buun take one `FNAME:SCALE` token for
// --lora-scaled and --control-vector-scaled; ik-llama.cpp reads two argv entries
// for both. Every fork reads --control-vector-layer-range as START END.
func TestCuratedLlamaFamily_MultiTokenFlagArity(t *testing.T) {
	tests := map[string]map[string]int{
		"llama-server":   {"control-vector-layer-range": 2, "lora-scaled": 0, "control-vector-scaled": 0},
		"buun-llama-cpp": {"control-vector-layer-range": 2, "lora-scaled": 0, "control-vector-scaled": 0},
		"beellama-cpp":   {"lora-scaled": 0, "control-vector-scaled": 0},
		"ik-llama-cpp":   {"control-vector-layer-range": 2, "lora-scaled": 2, "control-vector-scaled": 2, "spec-replace": 2},
	}
	family := llamaFamily()
	for kind, want := range tests {
		t.Run(kind, func(t *testing.T) {
			fs := family[kind].ToFlagSchema()
			for flag, wantArity := range want {
				spec, ok := fs.Lookup(flag)
				if !ok {
					t.Errorf("%s missing from the curated schema", flag)
					continue
				}
				if spec.Arity != wantArity {
					t.Errorf("%s Arity = %d, want %d", flag, spec.Arity, wantArity)
				}
			}
		})
	}
}

// S15: the live --help metavar count is authoritative for pattern-A backends, so
// overlaying a curated entry that never declared Arity must not erase it.
func TestMergeWithCurated_PreservesParsedArity(t *testing.T) {
	full := domain.BackendValidationSchema{
		Flags: map[string]domain.FlagSpec{
			"control-vector-layer-range": {Long: "control-vector-layer-range", Type: domain.FlagTypeString, Arity: 2},
		},
	}
	curated := domain.BackendValidationSchema{
		Flags: map[string]domain.FlagSpec{
			"control-vector-layer-range": {Long: "control-vector-layer-range", Type: domain.FlagTypeString},
		},
	}
	merged := mergeWithCurated(full, curated).ToFlagSchema()
	spec, ok := merged.Lookup("control-vector-layer-range")
	if !ok {
		t.Fatal("control-vector-layer-range missing after merge")
	}
	if spec.Arity != 2 {
		t.Fatalf("Arity = %d, want 2 (curated 0 must not erase the parsed count)", spec.Arity)
	}
}

// S17: mergeWithCuratedEnrich must preserve parsed EnumValues when the live
// binary declares different allowed values than the curated overlay. A prisma-ml
// fork with --spec-type=draft-dspark must not have its enum overwritten by the
// curated draft-dflash.
func TestMergeWithCuratedEnrich_PreservesParsedEnumValues(t *testing.T) {
	full := domain.BackendValidationSchema{
		Flags: map[string]domain.FlagSpec{
			"spec-type": {
				Long:       "spec-type",
				Type:       domain.FlagTypeString,
				EnumValues: []string{"none", "draft-simple", "draft-dspark"},
				Default:    "none",
			},
		},
	}
	curated := domain.BackendValidationSchema{
		Flags: map[string]domain.FlagSpec{
			"spec-type": {
				Long:       "spec-type",
				Type:       domain.FlagTypeEnum,
				List:       true,
				EnumValues: []string{"none", "draft-simple", "draft-dflash"},
				Default:    "none",
			},
		},
	}
	merged := mergeWithCuratedEnrich(full, curated)
	spec, ok := merged.Flags["spec-type"]
	if !ok {
		t.Fatal("spec-type missing after enrich merge")
	}
	// Enrich must preserve parsed enum values (prisma fork has dspark, not dflash).
	if len(spec.EnumValues) != 3 || spec.EnumValues[2] != "draft-dspark" {
		t.Fatalf("EnumValues = %v, want [none draft-simple draft-dspark] (parsed values must survive enrich)", spec.EnumValues)
	}
	// But Type and List from curated must still apply.
	if spec.Type != domain.FlagTypeEnum {
		t.Fatalf("Type = %d, want enum (4)", spec.Type)
	}
	if !spec.List {
		t.Fatal("List = false, want true (curated List must apply)")
	}
}

// S17b: mergeWithCurated (appendMissing=true, used by golden fallback) must
// still override parsed EnumValues since the golden may trail the curated schema.
func TestMergeWithCurated_OverridesParsedEnumValues(t *testing.T) {
	full := domain.BackendValidationSchema{
		Flags: map[string]domain.FlagSpec{
			"spec-type": {
				Long:       "spec-type",
				Type:       domain.FlagTypeString,
				EnumValues: []string{"none", "draft-simple"},
				Default:    "none",
			},
		},
	}
	curated := domain.BackendValidationSchema{
		Flags: map[string]domain.FlagSpec{
			"spec-type": {
				Long:       "spec-type",
				Type:       domain.FlagTypeEnum,
				List:       true,
				EnumValues: []string{"none", "draft-simple", "draft-dflash"},
				Default:    "none",
			},
		},
	}
	merged := mergeWithCurated(full, curated)
	spec, ok := merged.Flags["spec-type"]
	if !ok {
		t.Fatal("spec-type missing after append-missing merge")
	}
	// Append-missing (golden fallback) must override with curated values.
	if len(spec.EnumValues) != 3 || spec.EnumValues[2] != "draft-dflash" {
		t.Fatalf("EnumValues = %v, want [none draft-simple draft-dflash] (curated must override in golden path)", spec.EnumValues)
	}
}

// S17c: mergeWithCuratedEnrich must fill in curated EnumValues when the parser
// couldn't extract them (e.g. --prio N where the help text just says "N").
// These are curated metadata the parser structurally can't recover.
func TestMergeWithCuratedEnrich_FillsInMissingParsedEnumValues(t *testing.T) {
	full := domain.BackendValidationSchema{
		Flags: map[string]domain.FlagSpec{
			"prio": {
				Long:    "prio",
				Type:    domain.FlagTypeInt,
				Default: 0,
				// Parser couldn't extract enum values from "N" placeholder.
			},
		},
	}
	curated := domain.BackendValidationSchema{
		Flags: map[string]domain.FlagSpec{
			"prio": {
				Long:       "prio",
				Type:       domain.FlagTypeEnum,
				EnumValues: []string{"-1", "0", "1", "2", "3"},
				Default:    0,
			},
		},
	}
	merged := mergeWithCuratedEnrich(full, curated)
	spec, ok := merged.Flags["prio"]
	if !ok {
		t.Fatal("prio missing after enrich merge")
	}
	if len(spec.EnumValues) != 5 || spec.EnumValues[2] != "1" {
		t.Fatalf("EnumValues = %v, want [-1 0 1 2 3] (curated must fill in when parser didn't extract)", spec.EnumValues)
	}
}
