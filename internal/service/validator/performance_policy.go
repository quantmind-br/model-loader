package validator

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
)

// Options configures optional, installation-specific validator behavior. The
// zero value is the generic installation: no workstation policy is applied, so
// validator.New keeps producing the standard, portable report.
type Options struct {
	// RTX3090P2P enables the dual-RTX-3090 patched-P2P workstation performance
	// policy. When set, the validator layers extra advisory checks tuned for a
	// two-card, no-NVLink, PCIe-P2P rig on top of the standard rules. These
	// checks are warning-only and never block a profile.
	RTX3090P2P bool
}

// NewWithOptions returns a Validator with the standard rule set plus any
// optional installation policy enabled in opts. logger may be nil; nil ->
// log.Nop(). Production wires this from config so a workstation can opt into
// its policy; New stays the generic constructor for every other caller.
func NewWithOptions(logger *slog.Logger, opts Options) Validator {
	if logger == nil {
		logger = log.Nop()
	}
	return defaultValidator{logger: logger, opts: opts}
}

// applyPerformancePolicyRules layers the optional workstation policy on top of
// the generic report. It is a no-op unless opts.RTX3090P2P is set, and only
// ever appends SeverityWarning issues — never blocking errors.
func applyPerformancePolicyRules(p domain.Profile, kind domain.BackendKind, opts Options, rep Report) Report {
	if !opts.RTX3090P2P {
		return rep
	}
	rep = checkTP2Communication(p, kind, rep)
	rep = checkSingleGPUPin(p, kind, rep)
	rep = checkAgentKVCache(p, rep)
	return rep
}

// checkTP2Communication warns when a tensor-parallel (TP>=2) vLLM/SGLang
// profile deviates from the communication config proven on this SM86 rig:
//
//   - NCCL_P2P_DISABLE=1 pins NCCL to the SHM fallback instead of the validated
//     PCIe P2P transport (measured +13.5% concurrent throughput on vLLM TP2).
//     Warned for both backends.
//   - custom all-reduce crashes at startup on SM86 vLLM TP>=2
//     (custom_all_reduce.cuh:455 'invalid argument'), so
//     disable-custom-all-reduce=true is mandatory there. Warned only when a vLLM
//     profile leaves custom all-reduce enabled (flag absent or false). SGLang
//     silently self-disables custom all-reduce and is not warned either way.
func checkTP2Communication(p domain.Profile, kind domain.BackendKind, rep Report) Report {
	tp, ok := tensorParallelSize(p, kind)
	if !ok || tp < 2 {
		return rep
	}
	if hasEnv(p, "NCCL_P2P_DISABLE", "1") {
		rep = appendIssue(rep, FieldIssue{
			Field:    "launch.env",
			Message:  "NCCL_P2P_DISABLE=1 forces the stock-driver SHM fallback instead of the validated PCIe P2P transport for this TP>=2 profile (measured +13.5% concurrent throughput on vLLM TP2); remove it to keep the P2P path (rtx3090_p2p policy)",
			Severity: SeverityWarning,
		})
	}
	if kind == domain.BackendKindVLLM && !argBool(p, "disable-custom-all-reduce") {
		rep = appendIssue(rep, FieldIssue{
			Field:    "args.disable-custom-all-reduce",
			Message:  "custom all-reduce crashes at startup on this SM86 rig for TP>=2 vLLM (custom_all_reduce.cuh:455 'invalid argument'); set disable-custom-all-reduce=true (mandatory here) to fall back to NCCL (rtx3090_p2p policy)",
			Severity: SeverityWarning,
		})
	}
	return rep
}

// checkSingleGPUPin warns when a profile that does not deliberately span both
// cards is left unpinned, so it may land on the wrong GPU or fan out across
// both. Split/TP profiles are exempt because they are intentionally multi-GPU.
func checkSingleGPUPin(p domain.Profile, kind domain.BackendKind, rep Report) Report {
	if isMultiGPU(p, kind) || hasSingleGPUPin(p) {
		return rep
	}
	return appendIssue(rep, FieldIssue{
		Field:    "launch.env",
		Message:  "single-GPU profile is not pinned to a specific card; set CUDA_DEVICE_ORDER=PCI_BUS_ID and CUDA_VISIBLE_DEVICES=<n> so it lands on the intended GPU (rtx3090_p2p policy)",
		Severity: SeverityWarning,
	})
}

// checkAgentKVCache warns when an agent/tool-calling llama.cpp profile quantizes
// its KV cache to q4, which corrupts tool-call generation. Long-context /
// non-tool profiles may keep q4 KV and are exempt.
func checkAgentKVCache(p domain.Profile, rep Report) Report {
	ctk := strings.ToLower(strings.TrimSpace(argStringRaw(p, "cache-type-k")))
	ctv := strings.ToLower(strings.TrimSpace(argStringRaw(p, "cache-type-v")))
	if ctk == "" && ctv == "" {
		return rep // not a llama.cpp KV profile
	}
	if !isAgentProfile(p) {
		return rep
	}
	field := ""
	switch {
	case isQ4KV(ctk):
		field = "args.cache-type-k"
	case isQ4KV(ctv):
		field = "args.cache-type-v"
	default:
		return rep
	}
	return appendIssue(rep, FieldIssue{
		Field:    field,
		Message:  "agent/tool-calling profile uses q4 KV cache, which corrupts tool-call output; use q8_0/q8_0 unless this profile is explicitly long-context/non-tool (rtx3090_p2p policy)",
		Severity: SeverityWarning,
	})
}

// tensorParallelSize returns the configured tensor-parallel degree. vLLM uses
// tensor-parallel-size; SGLang's canonical flag is tp-size but it also accepts
// the tensor-parallel-size alias, so fall back to it when tp-size is absent.
func tensorParallelSize(p domain.Profile, kind domain.BackendKind) (int, bool) {
	switch kind {
	case domain.BackendKindVLLM:
		return argInt(p, "tensor-parallel-size")
	case domain.BackendKindSGLang:
		if tp, ok := argInt(p, "tp-size"); ok {
			return tp, true
		}
		return argInt(p, "tensor-parallel-size")
	}
	return 0, false
}

// isMultiGPU reports whether a profile deliberately distributes across both
// cards: vLLM/SGLang tensor parallelism >= 2, or a llama.cpp tensor/row split
// (split-mode, or an explicit multi-device tensor-split).
func isMultiGPU(p domain.Profile, kind domain.BackendKind) bool {
	if tp, ok := tensorParallelSize(p, kind); ok && tp >= 2 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(argStringRaw(p, "split-mode"))) {
	case "tensor", "row":
		return true
	}
	if ts := strings.TrimSpace(argStringRaw(p, "tensor-split")); strings.Contains(ts, ",") {
		return true
	}
	return false
}

// hasSingleGPUPin reports whether launch env pins the process to exactly one
// visible CUDA device via CUDA_VISIBLE_DEVICES.
func hasSingleGPUPin(p domain.Profile) bool {
	for _, e := range p.Launch.Env {
		if e.Key != "CUDA_VISIBLE_DEVICES" {
			continue
		}
		v := strings.TrimSpace(e.Value)
		if v == "" {
			return false
		}
		return !strings.Contains(v, ",")
	}
	return false
}

// hasEnv reports whether launch env carries the exact key=value pair.
func hasEnv(p domain.Profile, key, value string) bool {
	for _, e := range p.Launch.Env {
		if e.Key == key && e.Value == value {
			return true
		}
	}
	return false
}

// agentKeywords mark a profile as agent/tool-calling. Matched as whitespace-
// delimited tokens against the normalized tags + name + description.
var agentKeywords = []string{"agent", "agentic", "agents", "tool call", "tool calls", "tool calling", "coder", "coding"}

// nonAgentKeywords explicitly exclude a profile from the agent class even when
// an agent keyword also appears (e.g. a long-context "non-tool" variant).
var nonAgentKeywords = []string{"non tool", "no tool", "not tool", "non agent", "no agent"}

// isAgentProfile infers whether a profile serves agentic/tool-calling work from
// its tags, name, and description via normalized keyword matching. An explicit
// non-tool marker wins over any agent keyword.
func isAgentProfile(p domain.Profile) bool {
	hay := normalizeText(strings.Join(p.Tags, " ") + " " + p.Name + " " + p.Description)
	for _, neg := range nonAgentKeywords {
		if strings.Contains(hay, " "+neg+" ") {
			return false
		}
	}
	for _, kw := range agentKeywords {
		if strings.Contains(hay, " "+kw+" ") {
			return true
		}
	}
	return false
}

// normalizeText lowercases s and collapses every run of non-alphanumeric runes
// into a single space, then pads with surrounding spaces so keyword matches
// respect token boundaries.
func normalizeText(s string) string {
	var b strings.Builder
	b.WriteByte(' ')
	prevSpace := true
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	if !prevSpace {
		b.WriteByte(' ')
	}
	return b.String()
}

// isQ4KV reports whether a KV cache-type string is a q4 quantization.
func isQ4KV(t string) bool { return strings.HasPrefix(t, "q4") }

// argStringRaw returns a string-typed arg verbatim; non-string or missing -> "".
func argStringRaw(p domain.Profile, flag string) string {
	if s, ok := p.Args[flag].(string); ok {
		return s
	}
	return ""
}

// argInt coerces an arg to int, tolerating the JSON float64 round-trip and
// string-encoded numerics. Missing or non-numeric -> (0, false).
func argInt(p domain.Profile, flag string) (int, bool) {
	switch x := p.Args[flag].(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// argBool coerces an arg to bool, tolerating string-encoded booleans. Missing
// or non-boolean -> false.
func argBool(p domain.Profile, flag string) bool {
	switch x := p.Args[flag].(type) {
	case bool:
		return x
	case string:
		b, _ := strconv.ParseBool(strings.TrimSpace(x))
		return b
	}
	return false
}
