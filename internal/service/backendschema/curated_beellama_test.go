package backendschema

import (
	"reflect"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/validator"
)

// TestCuratedBeeLlama_SpecTypeEnum pins the spec-type enum to the BeeLlama
// v0.4.0 binary surface (build 10829), which renamed the fork's dflash to the
// upstream draft-dflash and removed the suffix/copyspec/recycle types. The
// curated enum overrides the parsed one in mergeWithCurated, so a stale list
// here rejects valid profiles (real DFlash profiles use spec-type=draft-dflash).
func TestCuratedBeeLlama_SpecTypeEnum(t *testing.T) {
	schema := CuratedBeeLlamaSchema().ToFlagSchema()
	spec, ok := schema.Lookup("spec-type")
	if !ok {
		t.Fatal("spec-type missing from curated schema")
	}
	want := []string{
		"none", "draft-simple", "draft-eagle3", "draft-mtp", "draft-dflash",
		"ngram-simple", "ngram-map-k", "ngram-map-k4v", "ngram-mod",
		"ngram-cache",
	}
	if !reflect.DeepEqual(spec.EnumValues, want) {
		t.Fatalf("spec-type enum = %v, want %v", spec.EnumValues, want)
	}
}

// TestCuratedBeeLlama_CacheTypesIncludeQ60 guards that the q6_0 KV cache type
// added in BeeLlama v0.3.0 is accepted on all four cache-type enums.
func TestCuratedBeeLlama_CacheTypesIncludeQ60(t *testing.T) {
	schema := CuratedBeeLlamaSchema().ToFlagSchema()
	for _, flag := range []string{"cache-type-k", "cache-type-v", "spec-draft-type-k", "spec-draft-type-v"} {
		spec, ok := schema.Lookup(flag)
		if !ok {
			t.Fatalf("%s missing from curated schema", flag)
		}
		if !containsStr(spec.EnumValues, "q6_0") {
			t.Fatalf("%s enum missing q6_0: %v", flag, spec.EnumValues)
		}
	}
}

// TestCuratedBeeLlama_RemovedV030SurfaceAbsent guards that flags and aliases
// removed by BeeLlama v0.3.0 no longer appear anywhere in the curated schema.
// Curated-only flags are re-injected and curated aliases override parsed ones
// in mergeWithCurated, so stale entries resolve to arguments the binary now
// rejects at startup.
func TestCuratedBeeLlama_RemovedV030SurfaceAbsent(t *testing.T) {
	schema := CuratedBeeLlamaSchema()

	removedFlags := map[string]bool{
		"spec-dflash-default":       true,
		"checkpoint-every-n-tokens": true,
	}
	removedAliases := map[string]bool{
		"draft": true, "draft-max": true, "draft-n": true,
		"draft-min": true, "draft-n-min": true, "draft-topk": true,
		"tree-budget": true, "spec-replace": true,
	}

	for key, spec := range schema.Flags {
		if removedFlags[key] || removedFlags[spec.Long] {
			t.Errorf("curated schema still defines removed flag %q", spec.Long)
		}
		for _, a := range spec.Aliases {
			if removedAliases[a] || removedFlags[a] {
				t.Errorf("flag %q still carries removed alias %q", spec.Long, a)
			}
		}
	}

	for _, rule := range schema.Rules {
		if removedFlags[rule.When.Flag] || removedFlags[rule.Then.Flag] {
			t.Errorf("cross-field rule %q references removed flag", rule.ID)
		}
	}

	for _, group := range schema.Presentation.Groups {
		for _, f := range group.Flags {
			if removedFlags[f] {
				t.Errorf("presentation group %q still lists removed flag %q", group.Name, f)
			}
		}
	}
}

// TestCuratedBeeLlama_ServerInvisibleDraftFlagsAbsent guards that the curated
// schema omits draft flags the llama-server binary does not register. Some
// draft CPU-affinity flags in common/arg.cpp are restricted via
// set_examples({LLAMA_EXAMPLE_SPECULATIVE}) (no LLAMA_EXAMPLE_SERVER), so
// llama-server rejects them at launch with "error: invalid argument". Because
// curated-only flags are re-injected by mergeWithCurated, keeping one here
// would make the web editor offer an option that fails when the profile starts.
func TestCuratedBeeLlama_ServerInvisibleDraftFlagsAbsent(t *testing.T) {
	schema := CuratedBeeLlamaSchema()

	// name -> why llama-server does not expose it
	invisible := map[string]string{
		"spec-draft-cpu-range-batch": "set_examples is LLAMA_EXAMPLE_SPECULATIVE only; llama-server rejects it",
	}

	for key, spec := range schema.Flags {
		if reason, ok := invisible[spec.Long]; ok {
			t.Errorf("curated schema defines server-invisible flag %q (%s)", spec.Long, reason)
		}
		if reason, ok := invisible[key]; ok {
			t.Errorf("curated schema key %q is a server-invisible flag (%s)", key, reason)
		}
	}
	for _, group := range schema.Presentation.Groups {
		for _, f := range group.Flags {
			if reason, ok := invisible[f]; ok {
				t.Errorf("presentation group %q lists server-invisible flag %q (%s)", group.Name, f, reason)
			}
		}
	}
}

// TestCuratedBeeLlama_CheckpointMinStep guards the v0.3.0 replacement of
// --checkpoint-every-n-tokens with --checkpoint-min-step (-cms). The default is
// 8192 (common.h::checkpoint_min_step); curated Default overrides the parsed one
// in mergeWithCurated, so a stale value here misreports the binary to operators.
func TestCuratedBeeLlama_CheckpointMinStep(t *testing.T) {
	curated := CuratedBeeLlamaSchema()
	spec, ok := curated.ToFlagSchema().Lookup("checkpoint-min-step")
	if !ok {
		t.Fatal("checkpoint-min-step missing from curated schema")
	}
	if spec.Short != "cms" {
		t.Errorf("checkpoint-min-step short = %q, want cms", spec.Short)
	}
	if spec.Default != 8192 {
		t.Errorf("checkpoint-min-step default = %v, want 8192", spec.Default)
	}

	listed := false
	for _, group := range curated.Presentation.Groups {
		for _, f := range group.Flags {
			if f == "checkpoint-min-step" {
				listed = true
			}
		}
	}
	if !listed {
		t.Error("checkpoint-min-step not listed in any presentation group")
	}
}

// TestCuratedBeeLlama_SpecDraftNMaxDefault pins spec-draft-n-max to the
// upstream default 3. Since v0.3.0 DFlash raises the effective omitted draft
// max to 16 by itself; the curated default overrides the parsed one in
// mergeWithCurated, so forcing 16 here would misreport the binary's behavior.
func TestCuratedBeeLlama_SpecDraftNMaxDefault(t *testing.T) {
	schema := CuratedBeeLlamaSchema().ToFlagSchema()
	spec, ok := schema.Lookup("spec-draft-n-max")
	if !ok {
		t.Fatal("spec-draft-n-max missing from curated schema")
	}
	if spec.Default != 3 {
		t.Fatalf("spec-draft-n-max default = %v, want 3", spec.Default)
	}
}

// TestCuratedBeeLlama_ValidatesMTPProfile guards that the curated fallback
// schema validates a realistic BeeLlama MTP profile, including the draft-mtp
// spec-type, the checkpoint-min-step flag, and the q6_0 cache type.
func TestCuratedBeeLlama_ValidatesMTPProfile(t *testing.T) {
	schema := CuratedBeeLlamaSchema().ToFlagSchema()

	p := domain.Profile{
		Model: existingModelPath(t),
		Args: map[string]any{
			"batch-size":          float64(2048),
			"ubatch-size":         float64(1024),
			"cache-type-k":        "q8_0",
			"cache-type-v":        "q6_0",
			"ctx-size":            float64(200000),
			"flash-attn":          "on",
			"host":                "127.0.0.1",
			"parallel":            float64(1),
			"threads":             float64(2),
			"threads-batch":       float64(8),
			"reasoning-budget":    float64(2048),
			"checkpoint-min-step": float64(256),
			"spec-type":           "draft-mtp",
			"spec-draft-n-max":    float64(3),
		},
	}

	rep := validator.New(nil).Validate(p, schema, schema.BackendKind)
	if rep.HasBlockingErrors() {
		t.Fatalf("expected curated schema to validate MTP profile, got %d errors: %+v", len(rep.Errors), rep.Errors)
	}
}
