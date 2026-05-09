package validator

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
)

var shortToLong = map[string]string{
	"ngl": "n-gpu-layers",
}

func canonicalFlag(key string) string {
	if long, ok := shortToLong[key]; ok {
		return long
	}
	return key
}

func applyTypeRules(p domain.Profile, schema domain.FlagSchema, rep Report) Report {
	for key, val := range p.Args {
		canonical := canonicalFlag(key)
		spec, ok := schema.Lookup(canonical)
		if !ok {
			rep = appendIssue(rep, FieldIssue{
				Field:    key,
				Message:  "unknown flag (not in backend schema)",
				Severity: SeverityError,
			})
			continue
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
		switch v := val.(type) {
		case int, int32, int64:
			return ""
		case float64:
			if v == math.Trunc(v) && !math.IsInf(v, 0) && !math.IsNaN(v) {
				return ""
			}
			return fmt.Sprintf("expected int, got %v", v)
		case float32:
			vf := float64(v)
			if v == float32(math.Trunc(vf)) && !math.IsInf(vf, 0) && !math.IsNaN(vf) {
				return ""
			}
			return fmt.Sprintf("expected int, got %v", v)
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
				Message:  fmt.Sprintf("ubatch-size (%d) exceeds batch-size (%d)", ubatch, batch),
				Severity: SeverityWarning,
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

func applyExtraArgsRules(p domain.Profile, schema domain.FlagSchema, rep Report) Report {
	i := 0
	for i < len(p.ExtraArgs) {
		arg := p.ExtraArgs[i]
		if !strings.HasPrefix(arg, "--") {
			rep = appendIssue(rep, FieldIssue{
				Field:    arg,
				Message:  "expected --flag, got bare value",
				Severity: SeverityError,
			})
			i++
			continue
		}

		flag, value, hasValue := parseExtraArg(arg)
		if !hasValue && i+1 < len(p.ExtraArgs) && !strings.HasPrefix(p.ExtraArgs[i+1], "--") {
			value = p.ExtraArgs[i+1]
			hasValue = true
			i++
		}

		canonical := canonicalFlag(flag)
		spec, ok := schema.Lookup(canonical)
		if !ok {
			rep = appendIssue(rep, FieldIssue{
				Field:    flag,
				Message:  "unknown flag in extra args (not in backend schema)",
				Severity: SeverityError,
			})
			i++
			continue
		}

		if spec.Type == domain.FlagTypeBool {
			if hasValue {
				rep = appendIssue(rep, FieldIssue{
					Field:    flag,
					Message:  "bool flag should not have a value",
					Severity: SeverityError,
				})
			}
		} else {
			if !hasValue {
				rep = appendIssue(rep, FieldIssue{
					Field:    flag,
					Message:  "missing value for flag",
					Severity: SeverityError,
				})
			} else if msg := checkExtraArgType(spec, value); msg != "" {
				rep = appendIssue(rep, FieldIssue{
					Field:    flag,
					Message:  msg,
					Severity: SeverityError,
				})
			}
		}
		i++
	}
	return rep
}

func parseExtraArg(arg string) (flag string, value string, hasValue bool) {
	body := strings.TrimPrefix(arg, "--")
	if eq := strings.Index(body, "="); eq >= 0 {
		return body[:eq], body[eq+1:], true
	}
	return body, "", false
}

func checkExtraArgType(spec domain.FlagSpec, val string) string {
	switch spec.Type {
	case domain.FlagTypeInt:
		if _, err := strconv.Atoi(val); err != nil {
			return fmt.Sprintf("expected int, got %q", val)
		}
	case domain.FlagTypeFloat:
		if _, err := strconv.ParseFloat(val, 64); err != nil {
			return fmt.Sprintf("expected float, got %q", val)
		}
	case domain.FlagTypeEnum:
		for _, v := range spec.EnumValues {
			if v == val {
				return ""
			}
		}
		return fmt.Sprintf("%q not in %v", val, spec.EnumValues)
	}
	return ""
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
