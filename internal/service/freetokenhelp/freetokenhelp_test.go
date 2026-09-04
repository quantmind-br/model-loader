package freetokenhelp

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestEmbeddedSchema(t *testing.T) {
	fs := EmbeddedSchema()
	if fs.Version != "embedded-freetoken-v1" {
		t.Fatalf("version = %q", fs.Version)
	}

	// Spot-check the load-bearing flags exist with the right types.
	want := map[string]domain.FlagType{
		"moe-backend":          domain.FlagTypeEnum,
		"moe-cache-auto":       domain.FlagTypeBool,
		"moe-cache-rate":       domain.FlagTypeFloat,
		"memory-ratio":         domain.FlagTypeFloat,
		"attention-backend":    domain.FlagTypeString,
		"cache-type":           domain.FlagTypeEnum,
		"tensor-parallel-size": domain.FlagTypeInt,
		"cors-origins":         domain.FlagTypeString,
	}
	for long, typ := range want {
		spec, ok := fs.Flags[long]
		if !ok {
			t.Errorf("missing flag %q", long)
			continue
		}
		if spec.Type != typ {
			t.Errorf("flag %q type = %v, want %v", long, spec.Type, typ)
		}
	}

	// port must be flagged manager-owned.
	port, ok := fs.Flags["port"]
	if !ok || !port.IsPort {
		t.Fatalf("port flag missing or not IsPort: %+v", port)
	}

	// The model comes from Profile.Model and is emitted as --model by the arg
	// builder, so neither spelling of the same argparse option may be a schema
	// flag (a row would make the editor emit a second one).
	for _, long := range []string{"model", "model-path"} {
		if _, ok := fs.Flags[long]; ok {
			t.Errorf("%q must not be a schema flag", long)
		}
	}

	// --shell-mode turns `ft serve` into an interactive TUI: it pins
	// max_running_req/cuda_graph_max_bs to 1 and sets silent_output, which
	// suppresses the ready line the process manager's readiness probe waits
	// for. --dummy-weight loads random weights. --host is forced to loopback by
	// the wrapper. None may be reachable from the Backends tab.
	for _, long := range []string{"shell-mode", "dummy-weight", "host"} {
		if _, ok := fs.Flags[long]; ok {
			t.Errorf("%q must not be a schema flag", long)
		}
	}

	// The schema tracks the pinned 0.1.2 release, whose argparse tree has
	// neither option; git main added both. A row for one would let the editor
	// emit a flag `ft serve` rejects outright (argparse exits 2 on an unknown
	// option), so the launch would die before the model ever loads.
	for _, long := range []string{"gpu", "ple-backend"} {
		if _, ok := fs.Flags[long]; ok {
			t.Errorf("%q is not in freetoken 0.1.2 and must not be a schema flag", long)
		}
	}

	// Documented aliases: the validator resolves args keys through
	// FlagSchema.Lookup, so an undeclared alias makes a profile written with it
	// fail validation.
	for alias, canonical := range map[string]string{
		"tp-size":           "tensor-parallel-size",
		"attn":              "attention-backend",
		"graph":             "cuda-graph-max-bs",
		"max-extend-length": "max-prefill-length",
		"tokenizer-count":   "num-tokenizer",
	} {
		spec, ok := fs.Lookup(alias)
		if !ok || spec.Long != canonical {
			t.Errorf("alias %q resolves to %q, want %q", alias, spec.Long, canonical)
		}
	}

	// moe-backend must offer every registered MoE backend plus auto; a missing
	// member would make the validator reject a working configuration.
	mb := fs.Flags["moe-backend"]
	for _, want := range []string{"auto", "fused", "offload", "cpu", "hybrid"} {
		if !hasEnum(mb.EnumValues, want) {
			t.Errorf("moe-backend enum missing %q: %v", want, mb.EnumValues)
		}
	}

	// reasoning-parser carries the disable value as an enum member ("off"),
	// not as an omitted flag: omitting it means "auto".
	if rp := fs.Flags["reasoning-parser"]; !hasEnum(rp.EnumValues, "off") {
		t.Errorf("reasoning-parser enum missing \"off\": %v", rp.EnumValues)
	}

	// Flags whose engine default is "unset" (the engine or the API adapter
	// resolves them) must carry no schema default, so the editor never
	// materializes a wrong explicit value: an explicit --num-pages/--num-tokens
	// overrides the VRAM-derived sizing, --moe-cache-rate is mutually exclusive
	// with two siblings, and --served-model-name only falls back to the model
	// basename when the flag is ABSENT (passing it empty serves an empty id).
	for _, long := range []string{
		"num-pages", "num-tokens", "moe-cache-rate", "max-seq-len-override",
		"cuda-graph-max-bs", "max-output-tokens", "served-model-name", "moe-cpu-layers",
	} {
		spec, ok := fs.Flags[long]
		if !ok {
			t.Errorf("missing flag %q", long)
			continue
		}
		if spec.Default != nil {
			t.Errorf("flag %q default = %v, want unset", long, spec.Default)
		}
	}

	// Defaults mirroring the installed argparse tree (verified by introspecting
	// backends/freetoken/.venv `ft serve`); a drift here silently changes what
	// the Backends tab shows.
	for long, want := range map[string]any{
		"memory-ratio":         float64(0.9),
		"kv-reserve-tokens":    float64(8192),
		"moe-hybrid-max-fetch": float64(-1),
		"page-size":            float64(1),
		"max-running-requests": float64(4),
		"max-prefill-length":   float64(8192),
		"decode-log-interval":  float64(40),
		"port":                 float64(1919),
		"num-tokenizer":        float64(0),
		"moe-cache-size":       float64(0),
		"moe-cpu-threads":      float64(0),
		"tensor-parallel-size": float64(1),
		"nvfp4-backend":        "triton",
		"cache-type":           "radix",
		"sampling-defaults":    "model",
		"dtype":                "auto",
		"model-source":         "huggingface",
		"moe-cache-policy":     "lru",
		"attention-backend":    "auto",
		"tool-call-parser":     "auto",
		"reasoning-parser":     "auto",
		"cors-origins":         "tauri://localhost,http://tauri.localhost,http://localhost:1420",
	} {
		if got := fs.Flags[long].Default; got != want {
			t.Errorf("flag %q default = %v (%T), want %v", long, got, got, want)
		}
	}

	// Both --disable-* options are argparse store_false actions: their presence
	// turns the feature OFF. Modeled as plain bools defaulting to false so the
	// arg builder (which omits a false bool) emits them only when the operator
	// asked to disable.
	for _, long := range []string{"disable-pynccl", "disable-moe-prefill-overlap"} {
		spec, ok := fs.Flags[long]
		if !ok {
			t.Errorf("missing flag %q", long)
			continue
		}
		if spec.Type != domain.FlagTypeBool || spec.Default != false {
			t.Errorf("flag %q = {type:%v default:%v}, want {bool false}", long, spec.Type, spec.Default)
		}
	}
}

func hasEnum(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
