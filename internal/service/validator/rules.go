package validator

import (
	"fmt"
	"os"

	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
)

func applyTypeRules(p domain.Profile, schema domain.FlagSchema, rep Report) Report {
	for key, val := range p.Args {
		spec, ok := schema.Lookup(key)
		if !ok {
			continue // unknown flags are not type-checked here
		}
		if msg := checkType(spec, val); msg != "" {
			rep = appendIssue(rep, FieldIssue{Field: key, Message: msg, Severity: SeverityError})
		}
	}
	return rep
}

func checkType(spec domain.FlagSpec, val any) string {
	switch spec.Type {
	case domain.FlagTypeInt:
		switch val.(type) {
		case int, int32, int64, float64, float32:
			return ""
		}
		return fmt.Sprintf("expected int, got %T", val)
	case domain.FlagTypeFloat:
		switch val.(type) {
		case float32, float64, int, int32, int64:
			return ""
		}
		return fmt.Sprintf("expected float, got %T", val)
	case domain.FlagTypeBool:
		if _, ok := val.(bool); ok {
			return ""
		}
		return fmt.Sprintf("expected bool, got %T", val)
	case domain.FlagTypeString:
		if _, ok := val.(string); ok {
			return ""
		}
		return fmt.Sprintf("expected string, got %T", val)
	case domain.FlagTypeEnum:
		s, ok := val.(string)
		if !ok {
			return fmt.Sprintf("expected one of %v, got %T", spec.EnumValues, val)
		}
		for _, v := range spec.EnumValues {
			if v == s {
				return ""
			}
		}
		return fmt.Sprintf("%q not in %v", s, spec.EnumValues)
	}
	return ""
}

func applyCrossFieldRules(p domain.Profile, rep Report) Report {
	if batch, ok := intArg(p.Args, "batch-size"); ok {
		if ubatch, ok := intArg(p.Args, "ubatch-size"); ok && ubatch > batch {
			rep = appendIssue(rep, FieldIssue{
				Field:    "ubatch-size",
				Message:  fmt.Sprintf("ubatch-size (%d) must be ≤ batch-size (%d)", ubatch, batch),
				Severity: SeverityError,
			})
		}
	}
	if fa, ok := stringArg(p.Args, "flash-attn"); ok && fa == "on" {
		if k, _ := stringArg(p.Args, "cache-type-k"); k == "f16" {
			rep = appendIssue(rep, FieldIssue{
				Field:    "cache-type-k",
				Message:  "flash-attn=on with f16 KV cache is suboptimal; consider q8_0",
				Severity: SeverityWarning,
			})
		} else if v, _ := stringArg(p.Args, "cache-type-v"); v == "f16" {
			rep = appendIssue(rep, FieldIssue{
				Field:    "cache-type-v",
				Message:  "flash-attn=on with f16 KV cache is suboptimal; consider q8_0",
				Severity: SeverityWarning,
			})
		}
	}
	if ctx, ok := intArg(p.Args, "ctx-size"); ok && ctx > 32768 {
		ngl, hasNGL := intArg(p.Args, "ngl")
		if !hasNGL {
			ngl, hasNGL = intArg(p.Args, "n-gpu-layers")
		}
		if hasNGL && ngl < 99 {
			rep = appendIssue(rep, FieldIssue{
				Field:    "ngl",
				Message:  fmt.Sprintf("ctx-size %d with ngl %d may force CPU offload", ctx, ngl),
				Severity: SeverityWarning,
			})
		}
	}
	return rep
}

func applyExistenceRules(p domain.Profile, rep Report) Report {
	if p.Model == "" {
		return rep
	}
	if _, err := os.Stat(p.Model); err != nil {
		if os.IsNotExist(err) {
			return appendIssue(rep, FieldIssue{
				Field:    "model",
				Message:  "model file does not exist",
				Severity: SeverityError,
			})
		}
		return appendIssue(rep, FieldIssue{
			Field:    "model",
			Message:  "model path stat failed: " + err.Error(),
			Severity: SeverityError,
		})
	}
	return rep
}

func intArg(args map[string]any, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	}
	return 0, false
}

func stringArg(args map[string]any, key string) (string, bool) {
	v, ok := args[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
