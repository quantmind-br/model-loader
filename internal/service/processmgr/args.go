package processmgr

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// BuildArgs converts a Profile into the CLI args slice used to spawn
// llama-server. The argument order is deterministic: --model first, then
// flags from p.Args sorted by key, then p.ExtraArgs verbatim.
//
// This is a convenience wrapper for llama-server; new backends should
// use BuildArgsForBackend.
func BuildArgs(p domain.Profile) []string {
	return buildLlamaArgs(p)
}

// BuildArgsForBackend converts a Profile into CLI args for the given backend
// kind and resolved executable. It dispatches to the appropriate builder.
// An empty kind defaults to llama-server for backward compatibility.
func BuildArgsForBackend(p domain.Profile, kind domain.BackendKind, executable string) ([]string, error) {
	switch kind {
	case domain.BackendKindLlamaServer, "":
		return buildLlamaArgs(p), nil
	case domain.BackendKindSGLang:
		return buildSGLangArgs(p), nil
	case domain.BackendKindVLLM:
		return buildVLLMArgs(p, executable), nil
	case domain.BackendKindDFlash:
		return buildDFlashArgs(p), nil
	case domain.BackendKindBuunLlamaCpp:
		return buildLlamaArgs(p), nil
	case domain.BackendKindBeeLlamaCpp:
		return buildLlamaArgs(p), nil
	case domain.BackendKindUnsloth:
		return buildUnslothArgs(p), nil
	default:
		return nil, fmt.Errorf("unsupported backend kind for arg building: %s", kind)
	}
}

// argBuildOpts parameterizes buildArgs for a specific backend's CLI shape.
type argBuildOpts struct {
	// skipKeys are p.Args keys never emitted as flags (the model is already
	// emitted from p.Model, so its alias keys must be skipped to avoid dups).
	skipKeys []string
	// modelFlag is the flag the model is emitted under (e.g. "--model"). An
	// empty modelFlag emits the model as a bare positional argument (vllm serve).
	modelFlag string
	// canonical maps user-friendly short keys to long-form flags via
	// domain.CanonicalFlag (llama-server only; other backends pass keys verbatim).
	canonical bool
}

// buildArgs converts a Profile into the CLI args slice for a backend described
// by opts: model first (flag or positional), then p.Args sorted by key (minus
// skipKeys) formatted by value type, then p.ExtraArgs verbatim. This is the
// single home of the value type-switch shared by every backend.
func buildArgs(p domain.Profile, opts argBuildOpts) []string {
	args := make([]string, 0, 2+2*len(p.Args)+len(p.ExtraArgs))
	if opts.modelFlag != "" {
		args = append(args, opts.modelFlag, p.Model)
	} else {
		args = append(args, p.Model)
	}

	skip := make(map[string]bool, len(opts.skipKeys))
	for _, k := range opts.skipKeys {
		skip[k] = true
	}

	keys := make([]string, 0, len(p.Args))
	for k := range p.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if skip[k] {
			continue
		}
		name := k
		if opts.canonical {
			name = domain.CanonicalFlag(k)
		}
		flag := "--" + name
		switch v := p.Args[k].(type) {
		case bool:
			if v {
				args = append(args, flag)
			}
		case string:
			args = append(args, flag, v)
		case int:
			args = append(args, flag, strconv.Itoa(v))
		case int32:
			args = append(args, flag, strconv.FormatInt(int64(v), 10))
		case int64:
			args = append(args, flag, strconv.FormatInt(v, 10))
		case float64:
			args = append(args, flag, formatFloat(v))
		case []any:
			parts := make([]string, len(v))
			for i, x := range v {
				parts[i] = fmt.Sprint(x)
			}
			args = append(args, flag, strings.Join(parts, ","))
		}
	}
	args = append(args, p.ExtraArgs...)
	return args
}

func buildLlamaArgs(p domain.Profile) []string {
	return buildArgs(p, argBuildOpts{skipKeys: []string{"model"}, modelFlag: "--model", canonical: true})
}

func buildSGLangArgs(p domain.Profile) []string {
	return buildArgs(p, argBuildOpts{skipKeys: []string{"model-path"}, modelFlag: "--model-path"})
}

func buildVLLMArgs(p domain.Profile, executable string) []string {
	// vLLM has two CLI shapes: "vllm serve" expects the model as a positional
	// argument; "python -m vllm.entrypoints.openai.api_server" expects --model.
	// The exact token "serve" in the command selects the positional shape.
	modelFlag := "--model"
	if hasToken(executable, "serve") {
		modelFlag = ""
	}
	return buildArgs(p, argBuildOpts{skipKeys: []string{"model"}, modelFlag: modelFlag})
}

// buildDFlashArgs builds args for the DFlash runtime (lucebox-hub
// dflash_server). The native server takes the target model as its first
// positional argument and rejects unknown options, so the model is emitted
// bare; every other flag in p.Args is emitted verbatim as --<key> <value>.
// The draft model is expected as the "draft" key in p.Args.
func buildDFlashArgs(p domain.Profile) []string {
	// "target" and "model" are alias guards for the positional model path
	// ("target" was the flag used by the retired Python wrapper).
	return buildArgs(p, argBuildOpts{skipKeys: []string{"target", "model"}, modelFlag: ""})
}

// buildUnslothArgs builds args for `unsloth studio run`. The model (HF repo or
// local GGUF path) is emitted under --model; every other flag in p.Args is
// emitted verbatim as --<key> <value>. --port is injected by prepareLaunch.
// The wrapper script forces the headless/loopback flags.
func buildUnslothArgs(p domain.Profile) []string {
	return buildArgs(p, argBuildOpts{skipKeys: []string{"model"}, modelFlag: "--model"})
}

func formatFloat(f float64) string {
	if f == math.Trunc(f) && !math.IsInf(f, 0) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// hasToken reports whether raw contains token as a standalone whitespace-
// delimited word. It avoids false positives from substrings (e.g. "serve"
// inside "api_server").
func hasToken(raw, token string) bool {
	for _, t := range strings.Fields(raw) {
		if t == token {
			return true
		}
	}
	return false
}
