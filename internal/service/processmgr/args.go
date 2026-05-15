package processmgr

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// shortToLong maps user-friendly short-form flag keys stored in Profile.Args
// to the canonical long-form llama-server accepts as `--<long>`. The UI
// editor stores keys like "ngl" / "ctx-size" because they read better; but
// llama-server only accepts the short form via the single-dash variant
// (`-ngl`), not `--ngl`. Translating here keeps existing profiles on disk
// working without a migration.
var shortToLong = map[string]string{
	"ngl": "n-gpu-layers",
}

// canonicalFlag returns the long-form name for a Profile.Args key.
func canonicalFlag(key string) string {
	if long, ok := shortToLong[key]; ok {
		return long
	}
	return key
}

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
	default:
		return nil, fmt.Errorf("unsupported backend kind for arg building: %s", kind)
	}
}

func buildLlamaArgs(p domain.Profile) []string {
	args := make([]string, 0, 2+2*len(p.Args)+len(p.ExtraArgs))
	args = append(args, "--model", p.Model)

	keys := make([]string, 0, len(p.Args))
	for k := range p.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		flag := "--" + canonicalFlag(k)
		switch v := p.Args[k].(type) {
		case bool:
			if v {
				args = append(args, flag)
			}
		case string:
			args = append(args, flag, v)
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

func buildSGLangArgs(p domain.Profile) []string {
	args := make([]string, 0, 2+2*len(p.Args)+len(p.ExtraArgs))
	args = append(args, "--model-path", p.Model)

	keys := make([]string, 0, len(p.Args))
	for k := range p.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		// model-path is already emitted from p.Model above.
		if k == "model-path" {
			continue
		}
		flag := "--" + k
		switch v := p.Args[k].(type) {
		case bool:
			if v {
				args = append(args, flag)
			}
		case string:
			args = append(args, flag, v)
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

func buildVLLMArgs(p domain.Profile, executable string) []string {
	args := make([]string, 0, 2+2*len(p.Args)+len(p.ExtraArgs))

	// vLLM has two CLI shapes:
	//   - "vllm serve" expects the model as a positional argument
	//   - "python -m vllm.entrypoints.openai.api_server" expects --model
	// We detect the exact token "serve" in the command to decide.
	if hasToken(executable, "serve") {
		args = append(args, p.Model)
	} else {
		args = append(args, "--model", p.Model)
	}

	keys := make([]string, 0, len(p.Args))
	for k := range p.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		// model is already emitted from p.Model above.
		if k == "model" {
			continue
		}
		flag := "--" + k
		switch v := p.Args[k].(type) {
		case bool:
			if v {
				args = append(args, flag)
			}
		case string:
			args = append(args, flag, v)
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
