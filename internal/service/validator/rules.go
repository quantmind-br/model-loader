package validator

import (
	"fmt"
	"math"
	"os"
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
		// Args values are emitted as a single argv token, so a flag whose value
		// spans several tokens (`--control-vector-layer-range START END`) can
		// never be launched correctly from here: the backend reads the next
		// flag as the missing value. Refuse it with the working alternative
		// instead of letting the launch fail on the backend side (BUGS.md S15).
		if spec.Arity > 1 {
			rep = appendIssue(rep, FieldIssue{
				Field:    key,
				Message:  fmt.Sprintf("takes %d space-separated values; supply all %d (the editor then emits them through extra args) or list --%s in extra args, which are passed through verbatim", spec.Arity, spec.Arity, spec.Long),
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

// checkEnum returns "" if val is valid for an enum flag. When spec.List is set
// the value is a comma-separated list (or a JSON array) and each element must
// appear in spec.EnumValues; a single value is also accepted. Otherwise the
// whole value must equal one of spec.EnumValues.
func checkEnum(spec domain.FlagSpec, val any) string {
	parts, ok := enumParts(spec, val)
	if !ok {
		return fmt.Sprintf("expected one of %v, got %T", spec.EnumValues, val)
	}
	if len(parts) == 0 {
		return fmt.Sprintf("expected one of %v, got %q", spec.EnumValues, val)
	}
	for _, p := range parts {
		if !enumContains(spec.EnumValues, p) {
			return fmt.Sprintf("%q not in %v", p, spec.EnumValues)
		}
	}
	return ""
}

// enumParts extracts the element(s) to validate from an enum arg value. A List
// flag accepts a comma-separated string ("a,b") or a JSON array ([]any of
// strings); a scalar enum accepts a single string.
func enumParts(spec domain.FlagSpec, val any) ([]string, bool) {
	if spec.List {
		switch v := val.(type) {
		case string:
			return splitTrim(v, ","), true
		case []any:
			out := make([]string, 0, len(v))
			for _, x := range v {
				s, ok := x.(string)
				if !ok {
					return nil, false
				}
				out = append(out, s)
			}
			return out, true
		case []string:
			return v, true
		}
		return nil, false
	}
	s, ok := val.(string)
	if !ok {
		return nil, false
	}
	return []string{s}, true
}

func enumContains(values []string, s string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}

// splitTrim splits s on sep, trims surrounding whitespace from each element, and
// drops empty elements — so "a, b" and "a,,b" both behave like "a,b".
func splitTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// matchesKeyword reports whether val is a string listed in spec.Keywords, i.e.
// one of the non-numeric literals a numeric flag also accepts ("auto", "all").
func matchesKeyword(spec domain.FlagSpec, val any) bool {
	s, ok := val.(string)
	if !ok {
		return false
	}
	for _, kw := range spec.Keywords {
		if s == kw {
			return true
		}
	}
	return false
}

func checkType(spec domain.FlagSpec, val any) string {
	if matchesKeyword(spec, val) {
		return ""
	}
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
	for _, allowed := range spec.AllowedInts {
		if n == int64(allowed) {
			return ""
		}
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

		flag, _, hasValue := parseExtraArg(arg)
		spec, known := schema.Lookup(domain.CanonicalFlag(flag))
		// Consume the value(s) supplied as "--flag value" (rather than
		// "--flag=value") so the following tokens are not misread as bare values
		// on the next iteration. A flag with Arity > 1 legitimately owns that
		// many bare tokens (`--control-vector-layer-range 0 31`) — extra args is
		// the only path that can express it, so it must not be reported as a
		// stray bare value (BUGS.md S15). The values themselves are not
		// validated — see the passthrough note below.
		if !hasValue {
			want := 1
			if known && spec.Arity > 1 {
				want = spec.Arity
			}
			for range want {
				if i+1 >= len(p.ExtraArgs) || strings.HasPrefix(p.ExtraArgs[i+1], "--") {
					break
				}
				i++
			}
		}

		// extraArgs is a raw passthrough to the backend binary: it is emitted
		// verbatim by processmgr. We deliberately do NOT type/enum/range-check
		// known flags here, because the curated schema can lag the binary (a
		// newer enum value such as a just-added draft-dflash, or a comma-list
		// flag) and enforcing a stale schema would block valid configurations
		// with no override (BUGS.md S1). The only diagnostic we emit is a
		// non-blocking warning for flags the schema does not recognize (likely
		// typos); the args path (applyTypeRules) still fully validates typed
		// flags, so validation is not weakened for the common case.
		if !known {
			rep = appendIssue(rep, FieldIssue{
				Field:    flag,
				Message:  "unknown flag in extra args (not in backend schema)",
				Severity: SeverityWarning,
			})
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
	// on backends that support them natively (vLLM, SGLang).
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
	case domain.BackendKindVLLM, domain.BackendKindSGLang, domain.BackendKindUnsloth:
		return true
	}
	return false
}

// applyRequiredRules flags a profile with no Model, plus any schema flag marked
// Required that is absent from both Args and ExtraArgs.
func applyRequiredRules(p domain.Profile, schema domain.FlagSchema, rep Report) Report {
	if strings.TrimSpace(p.Model) == "" {
		rep = appendIssue(rep, FieldIssue{
			Field:    "model",
			Message:  "required",
			Severity: SeverityError,
		})
	}
	for long, spec := range schema.Flags {
		if !spec.Required {
			continue
		}
		if _, ok := p.Args[long]; ok {
			continue
		}
		if spec.Short != "" {
			if _, ok := p.Args[domain.CanonicalFlag(spec.Short)]; ok {
				continue
			}
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
