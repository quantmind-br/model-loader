package validator

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
)

// policySchema declares every flag the workstation-policy tests reference so
// the generic rule set contributes no unknown-flag errors/warnings — leaving
// Report.Warnings populated exclusively by applyPerformancePolicyRules.
func policySchema() domain.FlagSchema {
	f := func(long string, t domain.FlagType) domain.FlagSpec {
		return domain.FlagSpec{Long: long, Type: t}
	}
	return domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"tensor-parallel-size":      f("tensor-parallel-size", domain.FlagTypeInt),
		"tp-size":                   f("tp-size", domain.FlagTypeInt),
		"disable-custom-all-reduce": f("disable-custom-all-reduce", domain.FlagTypeBool),
		"cache-type-k":              f("cache-type-k", domain.FlagTypeString),
		"cache-type-v":              f("cache-type-v", domain.FlagTypeString),
		"ctx-size":                  f("ctx-size", domain.FlagTypeInt),
		"n-gpu-layers":              f("n-gpu-layers", domain.FlagTypeInt),
		"split-mode":                f("split-mode", domain.FlagTypeString),
		"tensor-split":              f("tensor-split", domain.FlagTypeString),
		"main-gpu":                  f("main-gpu", domain.FlagTypeInt),
		"gpu-memory-utilization":    f("gpu-memory-utilization", domain.FlagTypeFloat),
	}}
}

func envVars(kv ...string) []domain.EnvVar {
	out := make([]domain.EnvVar, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, domain.EnvVar{Key: kv[i], Value: kv[i+1]})
	}
	return out
}

func warningFields(rep Report) []string {
	out := make([]string, 0, len(rep.Warnings))
	for _, w := range rep.Warnings {
		out = append(out, w.Field)
	}
	return out
}

func containsField(fields []string, want string) bool {
	for _, f := range fields {
		if f == want {
			return true
		}
	}
	return false
}

func TestPerformancePolicy(t *testing.T) {
	cases := []struct {
		name       string
		generic    bool // true => New() (workstation policy disabled)
		kind       domain.BackendKind
		profile    domain.Profile
		wantWarns  int
		wantFields []string
	}{
		{
			name:    "disabled policy emits no workstation warnings",
			generic: true,
			kind:    domain.BackendKindVLLM,
			profile: domain.Profile{
				ID:   "gemma-tp2",
				Args: map[string]any{"tensor-parallel-size": float64(2), "disable-custom-all-reduce": true},
				Launch: domain.LaunchConfig{
					Env: envVars("NCCL_P2P_DISABLE", "1"),
				},
			},
			wantWarns: 0,
		},
		{
			name: "vLLM TP2 with NCCL_P2P_DISABLE=1 warns on launch.env",
			kind: domain.BackendKindVLLM,
			profile: domain.Profile{
				ID:   "gemma-tp2",
				Args: map[string]any{"tensor-parallel-size": float64(2)},
				Launch: domain.LaunchConfig{
					Env: envVars("CUDA_DEVICE_ORDER", "PCI_BUS_ID", "NCCL_P2P_DISABLE", "1"),
				},
			},
			wantWarns:  1,
			wantFields: []string{"launch.env"},
		},
		{
			name: "vLLM TP2 with disable-custom-all-reduce warns",
			kind: domain.BackendKindVLLM,
			profile: domain.Profile{
				ID:   "gemma-tp2",
				Args: map[string]any{"tensor-parallel-size": float64(2), "disable-custom-all-reduce": true},
			},
			wantWarns:  1,
			wantFields: []string{"args.disable-custom-all-reduce"},
		},
		{
			name: "SGLang TP2 uses the same rules",
			kind: domain.BackendKindSGLang,
			profile: domain.Profile{
				ID:   "ocr-tp2",
				Args: map[string]any{"tp-size": float64(2), "disable-custom-all-reduce": true},
				Launch: domain.LaunchConfig{
					Env: envVars("NCCL_P2P_DISABLE", "1"),
				},
			},
			wantWarns:  2,
			wantFields: []string{"launch.env", "args.disable-custom-all-reduce"},
		},
		{
			name: "TP1 does not warn",
			kind: domain.BackendKindVLLM,
			profile: domain.Profile{
				ID:   "single-vllm",
				Args: map[string]any{"tensor-parallel-size": float64(1), "disable-custom-all-reduce": true},
				Launch: domain.LaunchConfig{
					Env: envVars("CUDA_VISIBLE_DEVICES", "0", "NCCL_P2P_DISABLE", "1"),
				},
			},
			wantWarns: 0,
		},
		{
			name: "single-GPU profile with no CUDA pin warns",
			kind: domain.BackendKindLlamaServer,
			profile: domain.Profile{
				ID:   "embed-32k",
				Name: "Qwen3 Embedding f16",
				Args: map[string]any{"n-gpu-layers": float64(99), "ctx-size": float64(32768)},
			},
			wantWarns:  1,
			wantFields: []string{"launch.env"},
		},
		{
			name: "split/TP profile does not receive the single-GPU-pin warning",
			kind: domain.BackendKindLlamaServer,
			profile: domain.Profile{
				ID:   "qwythos-tensor",
				Name: "Qwythos tensor-split 2x3090",
				Tags: []string{"long-context", "dual-gpu", "tensor-split"},
				Args: map[string]any{
					"split-mode":   "tensor",
					"tensor-split": "0.5,0.5",
					"main-gpu":     float64(1),
					"cache-type-k": "q8_0",
					"cache-type-v": "q8_0",
					"n-gpu-layers": float64(99),
				},
			},
			wantWarns: 0,
		},
		{
			name: "agent llama profile with q4/q4 KV warns",
			kind: domain.BackendKindLlamaServer,
			profile: domain.Profile{
				ID:          "qwopus-coder",
				Name:        "Qwopus Coder MTP",
				Description: "agent-ready coder profile with verified tool-call behavior",
				Tags:        []string{"agentic", "tool-call", "coder"},
				Args: map[string]any{
					"cache-type-k": "q4_0",
					"cache-type-v": "q4_0",
					"n-gpu-layers": float64(99),
				},
				Launch: domain.LaunchConfig{
					Env: envVars("CUDA_DEVICE_ORDER", "PCI_BUS_ID", "CUDA_VISIBLE_DEVICES", "1"),
				},
			},
			wantWarns:  1,
			wantFields: []string{"args.cache-type-k"},
		},
		{
			name: "q4 long-context non-agent profile does not warn",
			kind: domain.BackendKindLlamaServer,
			profile: domain.Profile{
				ID:          "qwythos-1m",
				Name:        "Qwythos 1M long-context",
				Description: "long-context non-tool variant; q4 KV acceptable for narrative generation",
				Tags:        []string{"long-context", "1m", "non-tool"},
				Args: map[string]any{
					"cache-type-k": "q4_0",
					"cache-type-v": "q4_0",
					"n-gpu-layers": float64(99),
				},
				Launch: domain.LaunchConfig{
					Env: envVars("CUDA_DEVICE_ORDER", "PCI_BUS_ID", "CUDA_VISIBLE_DEVICES", "1"),
				},
			},
			wantWarns: 0,
		},
	}

	schema := policySchema()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Validator
			if tc.generic {
				v = New(log.Nop())
			} else {
				v = NewWithOptions(log.Nop(), Options{RTX3090P2P: true})
			}
			rep := v.Validate(tc.profile, schema, tc.kind)
			if got := len(rep.Warnings); got != tc.wantWarns {
				t.Fatalf("warnings=%d %v, want %d", got, rep.Warnings, tc.wantWarns)
			}
			fields := warningFields(rep)
			for _, want := range tc.wantFields {
				if !containsField(fields, want) {
					t.Errorf("missing warning on field %q; got fields %v", want, fields)
				}
			}
		})
	}
}
