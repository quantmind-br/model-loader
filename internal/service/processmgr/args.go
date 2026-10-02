package processmgr

import (
	"fmt"
	"math"
	"path/filepath"
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
	case domain.BackendKindIkLlamaCpp:
		return buildLlamaArgs(p), nil
	case domain.BackendKindUnsloth:
		return buildUnslothArgs(p), nil
	case domain.BackendKindTabby:
		return buildTabbyArgs(p), nil
	case domain.BackendKindLMStudio:
		return buildLMStudioArgs(p), nil
	case domain.BackendKindFreeToken:
		return buildFreeTokenArgs(p), nil
	case domain.BackendKindStrata:
		// The HTTP server owns the native stdin engine. --model verifies the
		// profile's GGUF against the prepared JSON; it does not replace the pack.
		return append([]string{"--engine", "strata"}, buildArgs(p, argBuildOpts{skipKeys: []string{"model", "engine"}, modelFlag: "--model"})...), nil
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
	return buildArgs(p, argBuildOpts{skipKeys: []string{"model"}, modelFlag: "--model", canonical: true})
}

// buildLMStudioArgs builds args for the lmstudio backend, launched through
// backends/lms/lmstudio-serve.sh. The profile's Model is the LM Studio model
// key (or local GGUF path) and is emitted under --model; every other flag in
// p.Args (--port included, injected by prepareLaunch) is emitted verbatim as
// --<key> <value>. The wrapper routes server-start flags (--port/--bind/
// --cors) to `lms server start` and the rest to `lms load`.
func buildLMStudioArgs(p domain.Profile) []string {
	return buildArgs(p, argBuildOpts{skipKeys: []string{"model"}, modelFlag: "--model"})
}

// buildFreeTokenArgs builds args for FreeToken (`ft serve`), launched through
// backends/freetoken/freetoken-serve.sh. The profile's Model is the checkpoint
// directory, FTW directory, or HF/ModelScope repo id and is emitted under
// --model; --model-path is the same argparse option under its canonical
// spelling, so both are skipped as p.Args keys to avoid emitting the model
// twice. --port is injected by prepareLaunch; the wrapper forces --host.
func buildFreeTokenArgs(p domain.Profile) []string {
	return buildArgs(p, argBuildOpts{skipKeys: []string{"model", "model-path"}, modelFlag: "--model"})
}

// tabbyNargsFlags are TabbyAPI list-valued flags whose argparse definition uses
// nargs="+", so each element must be emitted as its own whitespace-delimited
// token (e.g. --gpu-split 21 23), never a single comma-joined token. A profile
// supplies them as a string ("21,23" or "21 23") or a JSON list; both expand.
var tabbyNargsFlags = map[string]bool{
	"gpu-split":         true,
	"autosplit-reserve": true,
	"draft-gpu-split":   true,
}

// buildTabbyArgs builds args for TabbyAPI (the ExLlamaV2/V3 OpenAI server),
// launched through backends/tabby/tabby-serve.sh. TabbyAPI loads a model by
// directory + subfolder name, so the profile's absolute model dir is split into
// --model-dir (parent) + --model-name (basename). --port is injected by
// prepareLaunch; the wrapper forces --host/--disable-auth. List flags
// (gpu-split, autosplit-reserve, draft-gpu-split) emit one token per element.
func buildTabbyArgs(p domain.Profile) []string {
	args := make([]string, 0, 4+2*len(p.Args)+len(p.ExtraArgs))
	args = append(args, "--model-dir", filepath.Dir(p.Model), "--model-name", filepath.Base(p.Model))

	skip := map[string]bool{"model": true, "model-dir": true, "model-name": true}
	keys := make([]string, 0, len(p.Args))
	for k := range p.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if skip[k] {
			continue
		}
		flag := "--" + k
		if tabbyNargsFlags[k] {
			toks := tabbyListTokens(p.Args[k])
			if len(toks) == 0 {
				continue
			}
			args = append(args, flag)
			args = append(args, toks...)
			continue
		}
		switch v := p.Args[k].(type) {
		case bool:
			// TabbyAPI's argparser is generated from a Pydantic model: boolean
			// flags take an explicit value ("--vision true"), they are NOT
			// store_true. Always emit the value so defaults can be overridden.
			args = append(args, flag, strconv.FormatBool(v))
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
			args = append(args, flag)
			for _, x := range v {
				args = append(args, fmt.Sprint(x))
			}
		}
	}
	args = append(args, p.ExtraArgs...)
	return args
}

// tabbyListTokens normalizes a nargs="+" flag value into separate tokens. A
// JSON list yields one token per element; a string is split on commas and
// whitespace so "21,23", "21 23", and "21, 23" all become ["21","23"].
func tabbyListTokens(v any) []string {
	switch vv := v.(type) {
	case []any:
		toks := make([]string, 0, len(vv))
		for _, x := range vv {
			toks = append(toks, fmt.Sprint(x))
		}
		return toks
	case string:
		fields := strings.FieldsFunc(vv, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\t'
		})
		return fields
	default:
		s := strings.TrimSpace(fmt.Sprint(vv))
		if s == "" {
			return nil
		}
		return []string{s}
	}
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
