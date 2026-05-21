package llamahelp

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestParseSectionHeader(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"----- common params -----", "common"},
		{"----- sampling params -----", "sampling"},
		{"----- example-specific params -----", "example-specific"},
		{"   ----- weird params -----   ", "weird"},
		{"-c,    --ctx-size N", ""},
		{"", ""},
		{"-----", ""},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			got := parseSectionHeader(tc.line)
			if got != tc.want {
				t.Fatalf("parseSectionHeader(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

func TestParseFlagLine_ShortLongPlaceholder(t *testing.T) {
	cases := []struct {
		name string
		line string
		want domain.FlagSpec
	}{
		{
			name: "ctx-size with N placeholder and default",
			line: "-c,    --ctx-size N                     size of the prompt context (default: 4096, 0 = loaded from model)",
			want: domain.FlagSpec{
				Long:     "ctx-size",
				Short:    "c",
				Type:     domain.FlagTypeInt,
				Default:  4096,
				HelpText: "size of the prompt context (default: 4096, 0 = loaded from model)",
			},
		},
		{
			name: "batch-size",
			line: "-b,    --batch-size N                   logical maximum batch size (default: 2048)",
			want: domain.FlagSpec{
				Long:     "batch-size",
				Short:    "b",
				Type:     domain.FlagTypeInt,
				Default:  2048,
				HelpText: "logical maximum batch size (default: 2048)",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFlagLine(tc.line)
			if !ok {
				t.Fatalf("parseFlagLine returned !ok for %q", tc.line)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseFlagLine_LongOnlyPlaceholder(t *testing.T) {
	cases := []struct {
		name string
		line string
		want domain.FlagSpec
	}{
		{
			name: "port long-only",
			line: "--port PORT                             port to listen (default: 8080)",
			want: domain.FlagSpec{
				Long:     "port",
				Type:     domain.FlagTypeInt,
				Default:  8080,
				HelpText: "port to listen (default: 8080)",
			},
		},
		{
			name: "keep long-only",
			line: "--keep N                                number of tokens to keep from the initial prompt (default: 0, -1 = all)",
			want: domain.FlagSpec{
				Long:     "keep",
				Type:     domain.FlagTypeInt,
				Default:  0,
				HelpText: "number of tokens to keep from the initial prompt (default: 0, -1 = all)",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFlagLine(tc.line)
			if !ok {
				t.Fatalf("parseFlagLine returned !ok for %q", tc.line)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseFlagLine_BoolNoPlaceholder(t *testing.T) {
	cases := []struct {
		name string
		line string
		want domain.FlagSpec
	}{
		{
			name: "mlock",
			line: "--mlock                                 force system to keep model in RAM rather than swapping or compressing",
			want: domain.FlagSpec{
				Long:     "mlock",
				Type:     domain.FlagTypeBool,
				HelpText: "force system to keep model in RAM rather than swapping or compressing",
			},
		},
		{
			name: "swa-full bool",
			line: "--swa-full                              use full-size SWA cache (default: false)",
			want: domain.FlagSpec{
				Long:     "swa-full",
				Type:     domain.FlagTypeBool,
				Default:  false,
				HelpText: "use full-size SWA cache (default: false)",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFlagLine(tc.line)
			if !ok {
				t.Fatalf("!ok for %q", tc.line)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseFlagLine_EnumPlaceholders(t *testing.T) {
	cases := []struct {
		name string
		line string
		want domain.FlagSpec
	}{
		{
			name: "flash-attn pipe enum",
			line: "-fa,   --flash-attn [on|off|auto]       set Flash Attention use ('on', 'off', or 'auto', default: 'auto')",
			want: domain.FlagSpec{
				Long:       "flash-attn",
				Short:      "fa",
				Type:       domain.FlagTypeEnum,
				EnumValues: []string{"on", "off", "auto"},
				Default:    "auto",
				HelpText:   "set Flash Attention use ('on', 'off', or 'auto', default: 'auto')",
			},
		},
		{
			name: "split-mode brace enum",
			line: "-sm,   --split-mode {none,layer,row}    how to split the model across multiple GPUs",
			want: domain.FlagSpec{
				Long:       "split-mode",
				Short:      "sm",
				Type:       domain.FlagTypeEnum,
				EnumValues: []string{"none", "layer", "row"},
				HelpText:   "how to split the model across multiple GPUs",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFlagLine(tc.line)
			if !ok {
				t.Fatalf("!ok for %q", tc.line)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestParseFlagLine_NPlaceholderFloatByDefault covers sampling params whose
// placeholder is the ambiguous "N" but whose default value is a decimal. These
// are floats (temp, top-p, min-p, ...); inferring int makes the validator reject
// legitimate values like 0.95 with "expected int, got 0.95".
func TestParseFlagLine_NPlaceholderFloatByDefault(t *testing.T) {
	cases := []struct {
		name        string
		line        string
		wantLong    string
		wantType    domain.FlagType
		wantDefault any
	}{
		{
			name:        "temp float default",
			line:        "--temp, --temperature N                 temperature (default: 0.80)",
			wantLong:    "temperature",
			wantType:    domain.FlagTypeFloat,
			wantDefault: 0.80,
		},
		{
			name:        "top-p float default with trailing note",
			line:        "--top-p N                               top-p sampling (default: 0.95, 1.0 = disabled)",
			wantLong:    "top-p",
			wantType:    domain.FlagTypeFloat,
			wantDefault: 0.95,
		},
		{
			name:        "top-k stays int",
			line:        "--top-k N                               top-k sampling (default: 40)",
			wantLong:    "top-k",
			wantType:    domain.FlagTypeInt,
			wantDefault: 40,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFlagLine(tc.line)
			if !ok {
				t.Fatalf("!ok for %q", tc.line)
			}
			if got.Long != tc.wantLong {
				t.Errorf("long=%q, want %q", got.Long, tc.wantLong)
			}
			if got.Type != tc.wantType {
				t.Errorf("type=%v, want %v", got.Type, tc.wantType)
			}
			if got.Default != tc.wantDefault {
				t.Errorf("default=%v (%T), want %v (%T)", got.Default, got.Default, tc.wantDefault, tc.wantDefault)
			}
		})
	}
}

func TestParseFlagLine_CacheTypeHardcodedEnum(t *testing.T) {
	cases := []struct {
		name string
		line string
		want domain.FlagSpec
	}{
		{
			name: "cache-type-k",
			line: "-ctk,  --cache-type-k TYPE              KV cache data type for K",
			want: domain.FlagSpec{
				Long:       "cache-type-k",
				Short:      "ctk",
				Type:       domain.FlagTypeEnum,
				EnumValues: cacheTypeEnum,
				HelpText:   "KV cache data type for K",
			},
		},
		{
			name: "cache-type-v",
			line: "-ctv,  --cache-type-v TYPE              KV cache data type for V",
			want: domain.FlagSpec{
				Long:       "cache-type-v",
				Short:      "ctv",
				Type:       domain.FlagTypeEnum,
				EnumValues: cacheTypeEnum,
				HelpText:   "KV cache data type for V",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFlagLine(tc.line)
			if !ok {
				t.Fatalf("!ok for %q", tc.line)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseFlagLine_MultiAlias(t *testing.T) {
	line := "-ngl,  --gpu-layers, --n-gpu-layers N   max. number of layers to store in VRAM (default: -1)"
	got, ok := parseFlagLine(line)
	if !ok {
		t.Fatalf("!ok for %q", line)
	}
	want := domain.FlagSpec{
		Long:     "n-gpu-layers",
		Short:    "ngl",
		Aliases:  []string{"gpu-layers"},
		Type:     domain.FlagTypeInt,
		Default:  -1,
		HelpText: "max. number of layers to store in VRAM (default: -1)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseFlagLine_NegationPairCanonicalIsPositive(t *testing.T) {
	cases := []struct {
		line     string
		wantLong string
		wantAls  []string
	}{
		{
			line:     "--perf, --no-perf                       whether to enable internal libllama performance timings",
			wantLong: "perf",
			wantAls:  []string{"no-perf"},
		},
		{
			line:     "-e,    --escape, --no-escape            whether to process escapes sequences",
			wantLong: "escape",
			wantAls:  []string{"no-escape"},
		},
		{
			line:     "-cb,   --cont-batching, -nocb, --no-cont-batching   batching mode",
			wantLong: "cont-batching",
			wantAls:  []string{"no-cont-batching"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			got, ok := parseFlagLine(tc.line)
			if !ok {
				t.Fatalf("!ok for %q", tc.line)
			}
			if got.Long != tc.wantLong {
				t.Errorf("Long=%q, want %q", got.Long, tc.wantLong)
			}
			if !reflect.DeepEqual(got.Aliases, tc.wantAls) {
				t.Errorf("Aliases=%v, want %v", got.Aliases, tc.wantAls)
			}
		})
	}
}

func TestParseHelp_SmokeOnFixture(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/help-v7376.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	schema, err := ParseHelp(data)
	if err != nil {
		t.Fatalf("ParseHelp: %v", err)
	}
	// Sanity: well-known flags must be present.
	want := []string{"ctx-size", "batch-size", "ubatch-size", "flash-attn", "port", "mlock"}
	for _, name := range want {
		if _, ok := schema.Flags[name]; !ok {
			t.Errorf("missing flag %q in parsed schema", name)
		}
	}
	// At least 50 flags expected from a real help dump.
	if len(schema.Flags) < 50 {
		t.Errorf("expected ≥50 flags parsed, got %d", len(schema.Flags))
	}
	// Group should be set for at least one common-section flag.
	if spec, ok := schema.Flags["ctx-size"]; ok && spec.Group != "common" {
		t.Errorf("ctx-size group=%q, want %q", spec.Group, "common")
	}
}

// TestParseHelp_WrappedFlagDescription covers flag-definition lines whose
// description wraps onto the next line. Two shapes regressed: a multi-alias
// line ending in a placeholder token ("... N") and a flag whose value list is
// rendered inline ("--spec-type none,draft-simple,..."). Both previously failed
// the alias-only continuation gate and were dropped from the schema entirely.
func TestParseHelp_WrappedFlagDescription(t *testing.T) {
	in := `----- speculative params -----

--spec-draft-threads, -td, --threads-draft N
                                        number of threads to use during generation (default: same as
                                        --threads)
--spec-type none,draft-simple,draft-eagle3
                                        comma-separated list of types of speculative decoding to use (default:
                                        none)
                                        (env: LLAMA_ARG_SPEC_TYPE)
`
	schema, err := ParseHelp([]byte(in))
	if err != nil {
		t.Fatalf("ParseHelp: %v", err)
	}

	// Canonical key is the last long alias ("threads-draft"); "spec-draft-threads"
	// rides along as an alias. The flag must no longer be dropped entirely.
	td, ok := schema.Flags["threads-draft"]
	if !ok {
		t.Fatalf("wrapped multi-alias flag missing from schema (keys: %v)", flagKeys(schema))
	}
	if td.Short != "td" {
		t.Errorf("threads-draft short=%q, want %q", td.Short, "td")
	}
	if td.Type != domain.FlagTypeInt {
		t.Errorf("threads-draft type=%v, want int", td.Type)
	}
	if !strings.HasPrefix(td.HelpText, "number of threads") {
		t.Errorf("threads-draft help=%q, want prefix %q", td.HelpText, "number of threads")
	}
	if !contains(td.Aliases, "spec-draft-threads") {
		t.Errorf("threads-draft aliases=%v, want to include spec-draft-threads", td.Aliases)
	}

	st, ok := schema.Flags["spec-type"]
	if !ok {
		t.Fatalf("spec-type missing from schema")
	}
	if !strings.HasPrefix(st.HelpText, "comma-separated list") {
		t.Errorf("spec-type help=%q, want prefix %q", st.HelpText, "comma-separated list")
	}
}

func flagKeys(s domain.FlagSchema) []string {
	keys := make([]string, 0, len(s.Flags))
	for k := range s.Flags {
		keys = append(keys, k)
	}
	return keys
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestGenerateGolden runs only with -update flag; commits the parsed schema
// so future regressions show up as JSON diff.
var updateGolden = flag.Bool("update", false, "regenerate golden files")

func TestParseHelp_Golden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/help-v7376.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	schema, err := ParseHelp(data)
	if err != nil {
		t.Fatalf("ParseHelp: %v", err)
	}
	got, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	goldenPath := "../../../testdata/help-v7376.golden.json"
	if *updateGolden {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Log("golden updated")
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update first): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden mismatch.\nDiff: run `go test ./internal/service/llamahelp -update` and inspect the diff with `git diff`.")
	}
}
