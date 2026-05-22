package validator

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func applyTypeRules(p domain.Profile, schema domain.FlagSchema, rep Report) Report {
	for key, val := range p.Args {
		canonical := domain.CanonicalFlag(key)
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

// checkInt returns "" if val is an integer or an integer-valued JSON number.
func checkInt(val any) string {
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
}

// checkFloat returns "" if val is any numeric type.
func checkFloat(val any) string {
	switch val.(type) {
	case float32, float64, int, int32, int64:
		return ""
	}
	return fmt.Sprintf("expected float, got %T", val)
}

// checkBool returns "" if val is a bool.
func checkBool(val any) string {
	if _, ok := val.(bool); ok {
		return ""
	}
	return fmt.Sprintf("expected bool, got %T", val)
}

// checkString returns "" if val is a string.
func checkString(val any) string {
	if _, ok := val.(string); ok {
		return ""
	}
	return fmt.Sprintf("expected string, got %T", val)
}

// checkEnum returns "" if val is a string present in spec.Choices.
func checkEnum(spec domain.FlagSpec, val any) string {
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

func checkType(spec domain.FlagSpec, val any) string {
	switch spec.Type {
	case domain.FlagTypeInt:
		if msg := checkInt(val); msg != "" {
			return msg
		}
		return checkIntRange(spec, val)
	case domain.FlagTypeFloat:
		if msg := checkFloat(val); msg != "" {
			return msg
		}
		return checkFloatRange(spec, val)
	case domain.FlagTypeBool:   return checkBool(val)
	case domain.FlagTypeString: return checkString(val)
	case domain.FlagTypeEnum:   return checkEnum(spec, val)
	}
	return ""
}

func toInt64(val any) (int64, bool) {
	switch v := val.(type) {
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		if v == math.Trunc(v) && !math.IsInf(v, 0) && !math.IsNaN(v) {
			return int64(v), true
		}
	}
	return 0, false
}

func toFloat64(val any) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

func checkIntRange(spec domain.FlagSpec, val any) string {
	n, ok := toInt64(val)
	if !ok {
		return ""
	}
	if spec.IsPort {
		if n < 1 || n > 65535 {
			return fmt.Sprintf("expected valid port (1-65535), got %d", n)
		}
		return ""
	}
	if spec.Min != nil && n < int64(*spec.Min) {
		return fmt.Sprintf("expected >= %d, got %d", *spec.Min, n)
	}
	if spec.Max != nil && n > int64(*spec.Max) {
		return fmt.Sprintf("expected <= %d, got %d", *spec.Max, n)
	}
	return ""
}

func checkFloatRange(spec domain.FlagSpec, val any) string {
	f, ok := toFloat64(val)
	if !ok {
		return ""
	}
	if spec.FloatMin != nil && f < *spec.FloatMin {
		return fmt.Sprintf("expected >= %v, got %v", *spec.FloatMin, f)
	}
	if spec.FloatMax != nil && f > *spec.FloatMax {
		return fmt.Sprintf("expected <= %v, got %v", *spec.FloatMax, f)
	}
	return ""
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

		canonical := domain.CanonicalFlag(flag)
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
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Sprintf("expected int, got %q", val)
		}
		return checkIntRange(spec, int64(n))
	case domain.FlagTypeFloat:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return fmt.Sprintf("expected float, got %q", val)
		}
		return checkFloatRange(spec, f)
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

func applyExistenceRules(p domain.Profile, kind domain.BackendKind, rep Report) Report {
	if p.Model == "" {
		return rep
	}
	_, err := os.Stat(p.Model)
	if err == nil {
		return rep
	}
	// File does not exist locally. Only skip the error for
	// HuggingFace-style repo IDs (e.g. "meta-llama/Llama-3-8B")
	// on backends that support them natively (vLLM, SGLang, TabbyAPI).
	// Local paths that happen to match the heuristic but exist
	// are caught by the os.Stat success path above.
	if domain.LooksLikeHFRepo(p.Model) && supportsHFRepo(kind) {
		return rep
	}
	if os.IsNotExist(err) {
		return appendIssue(rep, FieldIssue{
			Field:    "model",
			Message:  "model file does not exist",
			Severity: SeverityError,
		})
	}
	if os.IsPermission(err) {
		return appendIssue(rep, FieldIssue{
			Field:    "model",
			Message:  "permission denied for model path",
			Severity: SeverityError,
		})
	}
	return appendIssue(rep, FieldIssue{
		Field:    "model",
		Message:  "model path stat failed: " + err.Error(),
		Severity: SeverityError,
	})
}

func supportsHFRepo(kind domain.BackendKind) bool {
	switch kind {
	case domain.BackendKindVLLM, domain.BackendKindSGLang, domain.BackendKindTabbyAPI:
		return true
	}
	return false
}

// applyRequiredRules flags any schema flag marked Required that is absent from
// both Args and ExtraArgs.
func applyRequiredRules(p domain.Profile, schema domain.FlagSchema, rep Report) Report {
	for long, spec := range schema.Flags {
		if !spec.Required {
			continue
		}
		if _, ok := p.Args[long]; ok {
			continue
		}
		if _, ok := p.Args[domain.CanonicalFlag(spec.Short)]; spec.Short != "" && ok {
			continue
		}
		if extraArgsContain(p.ExtraArgs, spec) {
			continue
		}
		rep = appendIssue(rep, FieldIssue{
			Field:    long,
			Message:  "required flag is missing",
			Severity: SeverityError,
		})
	}
	return rep
}

func extraArgsContain(extra []string, spec domain.FlagSpec) bool {
	for _, a := range extra {
		flag, _, _ := parseExtraArg(a)
		c := domain.CanonicalFlag(flag)
		if c == spec.Long || c == spec.Short {
			return true
		}
		for _, al := range spec.Aliases {
			if c == al {
				return true
			}
		}
	}
	return false
}

