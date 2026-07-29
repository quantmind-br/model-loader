package backendschema

import (
	"reflect"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestCuratedBuun_RuntimeFacts(t *testing.T) {
	fs := CuratedBuunSchema().ToFlagSchema()
	specType, ok := fs.Lookup("spec-type")
	if !ok {
		t.Fatal("spec-type missing from curated schema")
	}
	if !containsStr(specType.EnumValues, "draft-dflash") || !containsStr(specType.EnumValues, "dflash") {
		t.Fatalf("spec-type enum = %v, want both draft-dflash and dflash", specType.EnumValues)
	}

	tests := []struct {
		name        string
		def         any
		aliases     []string
		short       string
		wantType    domain.FlagType
		wantMin     *int
		wantAllowed []int
	}{
		{name: "cache-idle-slots", def: true, aliases: []string{"no-cache-idle-slots"}, wantType: domain.FlagTypeBool},
		{name: "sleep-idle-seconds", def: -1, wantType: domain.FlagTypeInt, wantMin: ptrutilInt(1), wantAllowed: []int{-1}},
		{name: "parallel", def: -1, wantType: domain.FlagTypeInt},
		{name: "spec-draft-n-max", def: 3, aliases: []string{"draft", "draft-n", "draft-max"}, wantType: domain.FlagTypeInt},
		{name: "spec-draft-n-min", def: 0, aliases: []string{"draft-min", "draft-n-min"}, wantType: domain.FlagTypeInt},
		{name: "spec-draft-p-split", def: 0.1, aliases: []string{"draft-p-split"}, wantType: domain.FlagTypeFloat},
		{name: "spec-draft-p-min", def: 0.0, aliases: []string{"draft-p-min"}, wantType: domain.FlagTypeFloat},
		{name: "dflash-max-slots", def: 1, wantType: domain.FlagTypeInt},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec, ok := fs.Lookup(tc.name)
			if !ok {
				t.Fatalf("%s missing from curated schema", tc.name)
			}
			if spec.Type != tc.wantType {
				t.Fatalf("%s type = %v, want %v", tc.name, spec.Type, tc.wantType)
			}
			if spec.Default != tc.def {
				t.Fatalf("%s default = %v (%T), want %v (%T)", tc.name, spec.Default, spec.Default, tc.def, tc.def)
			}
			for _, alias := range tc.aliases {
				if _, ok := fs.Lookup(alias); !ok {
					t.Errorf("%s alias %q does not resolve", tc.name, alias)
				}
			}
			if tc.wantMin != nil && (spec.Min == nil || *spec.Min != *tc.wantMin) {
				t.Fatalf("%s min = %v, want %d", tc.name, spec.Min, *tc.wantMin)
			}
			if tc.wantAllowed != nil && !reflect.DeepEqual(spec.AllowedInts, tc.wantAllowed) {
				t.Fatalf("%s allowed ints = %v, want %v", tc.name, spec.AllowedInts, tc.wantAllowed)
			}
		})
	}
}

func TestCuratedBuun_DraftAliasesAndCacheTypes(t *testing.T) {
	fs := CuratedBuunSchema().ToFlagSchema()

	for _, tc := range []struct {
		name    string
		short   string
		aliases []string
	}{
		{name: "spec-draft-model", short: "md", aliases: []string{"model-draft", "draft-model"}},
		{name: "spec-draft-hf", short: "hfd", aliases: []string{"hf-repo-draft"}},
		{name: "spec-draft-device", short: "devd", aliases: []string{"device-draft"}},
		{name: "spec-draft-ngl", short: "ngld", aliases: []string{"gpu-layers-draft", "n-gpu-layers-draft"}},
		{name: "spec-draft-threads", short: "td", aliases: []string{"threads-draft"}},
		{name: "cache-type-k-draft", short: "ctkd", aliases: []string{"spec-draft-type-k"}},
		{name: "cache-type-v-draft", short: "ctvd", aliases: []string{"spec-draft-type-v"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec, ok := fs.Lookup(tc.name)
			if !ok {
				t.Fatalf("%s missing from curated schema", tc.name)
			}
			if spec.Short != tc.short {
				t.Errorf("%s short = %q, want %q", tc.name, spec.Short, tc.short)
			}
			for _, alias := range tc.aliases {
				if _, ok := fs.Lookup(alias); !ok {
					t.Errorf("%s alias %q does not resolve", tc.name, alias)
				}
			}
		})
	}

	wantBase := []string{"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1", "turbo2", "turbo3", "turbo4", "turbo8", "turbo3_tcq", "turbo2_tcq", "turbo1_tcq"}
	main, _ := fs.Lookup("cache-type-k")
	if !reflect.DeepEqual(main.EnumValues, append(append([]string{}, wantBase...), "vbr")) {
		t.Fatalf("main K cache types = %v", main.EnumValues)
	}
	draft, _ := fs.Lookup("spec-draft-type-k")
	if !reflect.DeepEqual(draft.EnumValues, wantBase) {
		t.Fatalf("draft K cache types = %v", draft.EnumValues)
	}
	for _, name := range []string{"cache-type-v", "spec-draft-type-v"} {
		spec, ok := fs.Lookup(name)
		if !ok {
			t.Fatalf("%s missing from curated schema", name)
		}
		want := wantBase
		if name == "cache-type-v" {
			want = append(append([]string{}, wantBase...), "vbr")
		}
		if !reflect.DeepEqual(spec.EnumValues, want) {
			t.Errorf("%s cache types = %v, want %v", name, spec.EnumValues, want)
		}
	}
}

func TestCuratedBuun_MergePreservesParsedDraftLongs(t *testing.T) {
	parsed := domain.BackendValidationSchema{Flags: map[string]domain.FlagSpec{
		"draft-model": {
			Long:    "draft-model",
			Aliases: []string{"spec-draft-model", "model-draft"},
			Type:    domain.FlagTypeString,
		},
		"hf-repo-draft": {
			Long:    "hf-repo-draft",
			Aliases: []string{"spec-draft-hf"},
			Type:    domain.FlagTypeString,
		},
		"cache-type-k-draft": {
			Long:    "cache-type-k-draft",
			Aliases: []string{"spec-draft-type-k"},
			Type:    domain.FlagTypeEnum,
		},
	}}

	merged := mergeWithCurated(parsed, CuratedBuunSchema()).ToFlagSchema()
	for _, tc := range []struct {
		name string
		long string
	}{
		{name: "spec-draft-model", long: "draft-model"},
		{name: "spec-draft-hf", long: "hf-repo-draft"},
		{name: "spec-draft-type-k", long: "cache-type-k-draft"},
	} {
		spec, ok := merged.Lookup(tc.name)
		if !ok {
			t.Fatalf("%s missing after curated merge", tc.name)
		}
		if spec.Long != tc.long {
			t.Errorf("%s resolved Long = %q, want parsed Long %q", tc.name, spec.Long, tc.long)
		}
	}
}

func TestCuratedBuun_VBRControls(t *testing.T) {
	fs := CuratedBuunSchema().ToFlagSchema()

	tests := []struct {
		name    string
		def     any
		aliases []string
		kind    domain.FlagType
	}{
		{name: "vbr-budget", def: "dynamic", aliases: []string{"vbr-bits"}, kind: domain.FlagTypeString},
		{name: "vbr-min-bits", def: "t1", aliases: []string{"vbr-floor"}, kind: domain.FlagTypeString},
		{name: "vbr-vram-budget", def: "auto", aliases: []string{"vbr-vram"}, kind: domain.FlagTypeString},
		{name: "vbr-reclaim-floor", def: 8.125, kind: domain.FlagTypeFloat},
		{name: "vbr-reset-keep-frac", def: 0.25, kind: domain.FlagTypeFloat},
		{name: "vbr-policy", def: nil, kind: domain.FlagTypeString},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec, ok := fs.Lookup(tc.name)
			if !ok {
				t.Fatalf("%s missing from curated schema", tc.name)
			}
			if spec.Group != buunGroupKV {
				t.Errorf("%s group = %q, want %q", tc.name, spec.Group, buunGroupKV)
			}
			if spec.Type != tc.kind {
				t.Errorf("%s type = %v, want %v", tc.name, spec.Type, tc.kind)
			}
			if spec.Default != tc.def {
				t.Errorf("%s default = %v (%T), want %v (%T)", tc.name, spec.Default, spec.Default, tc.def, tc.def)
			}
			for _, alias := range tc.aliases {
				if _, ok := fs.Lookup(alias); !ok {
					t.Errorf("%s alias %q does not resolve", tc.name, alias)
				}
			}
		})
	}
}

func TestCuratedBuun_EssentialsAndSource(t *testing.T) {
	schema := CuratedBuunSchema()
	if schema.Source.GeneratedFrom != "buun-llama-cpp/common/arg.cpp" {
		t.Errorf("GeneratedFrom = %q", schema.Source.GeneratedFrom)
	}
	if schema.Source.SourceVersion != "10696 (0eb1e82b6)" {
		t.Errorf("SourceVersion = %q", schema.Source.SourceVersion)
	}

	wantEssentials := []string{"hf-repo", "hf-token", "ctx-size", "host", "port", "n-gpu-layers", "device", "parallel", "threads", "batch-size", "ubatch-size", "cache-type-k", "cache-type-v", "cache-ram", "kv-unified", "cont-batching", "api-key", "alias"}
	for _, group := range schema.Presentation.Groups {
		if group.Name == buunGroupEssentials {
			if !group.Highlighted {
				t.Fatal("Essentials group is no longer highlighted")
			}
			if !reflect.DeepEqual(group.Flags, wantEssentials) {
				t.Fatalf("Essentials flags changed: got %v, want %v", group.Flags, wantEssentials)
			}
			return
		}
	}
	t.Fatal("Essentials group missing")
}

func TestCuratedBuun_EnglishDescriptions(t *testing.T) {
	fs := CuratedBuunSchema().ToFlagSchema()
	want := map[string]string{
		"ssl-key-file":  "SSL private key.",
		"timeout":       "Read/write timeout in seconds.",
		"cont-batching": "Enables or disables continuous batching.",
		"models-preset": "Model preset loaded by the multi-model router.",
	}
	for name, text := range want {
		spec, ok := fs.Lookup(name)
		if !ok {
			t.Fatalf("%s missing from curated schema", name)
		}
		if spec.HelpText != text {
			t.Errorf("%s description = %q, want %q", name, spec.HelpText, text)
		}
	}
}

func ptrutilInt(value int) *int {
	return &value
}
