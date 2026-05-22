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
		if k == "model" {
			continue
		}
		flag := "--" + domain.CanonicalFlag(k)
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

// buildDFlashArgs builds args for the DFlash runtime (lucebox-hub server.py).
// The model maps to --target; every other flag in p.Args is emitted verbatim
// as --<key> <value>. The draft model is expected as the "draft" key in p.Args.
func buildDFlashArgs(p domain.Profile) []string {
	args := make([]string, 0, 2+2*len(p.Args)+len(p.ExtraArgs))
	args = append(args, "--target", p.Model)

	keys := make([]string, 0, len(p.Args))
	for k := range p.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		// target is already emitted from p.Model above; "model" is an alias guard.
		if k == "target" || k == "model" {
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
