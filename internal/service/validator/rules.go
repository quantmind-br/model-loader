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
	_, err := os.Stat(p.Model)
	if err == nil {
		return rep
	}
	// File does not exist locally. Only skip the error for
	// HuggingFace-style repo IDs (e.g. "meta-llama/Llama-3-8B")
	// so that sglang and other HF-capable backends can use them.
	// Local paths that happen to match the heuristic but exist
	// are caught by the os.Stat success path above.
	if domain.LooksLikeHFRepo(p.Model) {
		return rep
	}
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



