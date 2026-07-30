package llamahelp

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

// cacheTypeEnum lists the KV-cache quant types accepted by --cache-type-{k,v}.
// The --help output renders these as TYPE; we hardcode the well-known set so
// the editor can offer a select.
var cacheTypeEnum = []string{"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1"}

// ParseHelp scans the full --help output and returns a FlagSchema.
// Lines before the first section header are skipped (CUDA banner etc).
// Flag descriptions include every indented continuation line up to the next
// flag, section header, or environment annotation.
func ParseHelp(data []byte) (domain.FlagSchema, error) {
	schema := domain.FlagSchema{Flags: make(map[string]domain.FlagSpec)}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return domain.FlagSchema{}, err
	}

	currentGroup := ""
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if header := parseSectionHeader(line); header != "" {
			currentGroup = header
			continue
		}
		if currentGroup == "" {
			continue
		}
		if !isFlagDefLine(line) {
			continue
		}
		continuations := flagContinuations(lines, i+1)
		spec, ok := parseFlagLine(line)
		if !ok && len(continuations) > 0 {
			spec, ok = parseFlagLine(line + "  " + continuations[0])
			continuations = continuations[1:]
		}
		if ok && len(continuations) > 0 {
			spec.HelpText = strings.TrimSpace(strings.Join(append([]string{spec.HelpText}, continuations...), " "))
			rawDefault := extractDefault(spec.HelpText)
			if spec.Type == domain.FlagTypeInt && looksFloat(rawDefault) {
				spec.Type = domain.FlagTypeFloat
			}
			if rawDefault != nil {
				spec.Default = coerceDefault(spec.Type, rawDefault)
				// Mirror parseFlagLine's numeric cleanup: if coercion
				// produced a non-numeric default for a numeric type
				// (e.g. "read from model"), drop it.
				if (spec.Type == domain.FlagTypeInt || spec.Type == domain.FlagTypeFloat) && spec.Default != nil {
					if _, ok := spec.Default.(string); ok {
						spec.Default = nil
					}
				}
			}
			spec = hardcodedFlagOverrides(spec)
		}
		if !ok {
			continue
		}
		if values := allowedValuesFromContinuations(lines, i+1); len(values) > 0 {
			spec.Type = domain.FlagTypeEnum
			spec.EnumValues = values
		}
		spec.Group = currentGroup
		schema.Flags[spec.Long] = spec
	}
	return schema, nil
}

func flagContinuations(lines []string, start int) []string {
	var out []string
	skippingAllowedValues := false
	for j := start; j < len(lines); j++ {
		line := lines[j]
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			continue
		case isFlagDefLine(line), parseSectionHeader(line) != "":
			return out
		case strings.HasPrefix(trimmed, "(env:"):
			return out
		case strings.HasPrefix(trimmed, allowedValuesPrefix):
			skippingAllowedValues = true
			continue
		case skippingAllowedValues && strings.HasPrefix(trimmed, "(default:"):
			skippingAllowedValues = false
		case skippingAllowedValues:
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// isFlagDefLine reports whether the line begins a flag definition: a token at
// column 0 starting with '-'. Such a line is a flag whose description (if any)
// lives on the following continuation line — description and wrapped lines are
// always indented, so they never start at column 0. This covers multi-alias
// lines ending in a placeholder ("--threads-draft N") and inline value lists
// ("--spec-type none,draft-simple,...") that the older alias-only heuristic
// dropped because their trailing token does not start with '-'.
func isFlagDefLine(line string) bool {
	return strings.HasPrefix(line, "-")
}

var sectionHeaderRe = regexp.MustCompile(`^-{5}\s+(.+?)\s+params\s+-{5}$`)

var (
	bracketEnumRe     = regexp.MustCompile(`^\[([^\]]+)\]$`)
	braceEnumRe       = regexp.MustCompile(`^\{([^}]+)\}$`)
	angleBracketIntRe = regexp.MustCompile(`^<(\d+)(?:\|(\d+)|\.{2,3}(\d+))>$`)
)

// parseSectionHeader returns the section name (e.g., "common") for a header
// line, or "" if the line is not a section header.
func parseSectionHeader(line string) string {
	trimmed := strings.TrimSpace(line)
	m := sectionHeaderRe.FindStringSubmatch(trimmed)
	if m == nil {
		return ""
	}
	return m[1]
}

// flagLineRe matches the canonical "<aliases>  <description>" layout.
// Group 1 = alias chunk (left), Group 2 = description chunk (right).
// Two or more spaces separate the alias chunk from the description.
var flagLineRe = regexp.MustCompile(`^(.*[^ ])\s{2,}(\S.*)$`)

// defaultRe extracts "(default: X)" or ", default: X" — first occurrence wins.
// The opening paren is optional because enums may use ", default: X" format.
var defaultRe = regexp.MustCompile(`\(?default:\s*([^,)]+)`)

var allowedValuesPrefix = "allowed values:"

// hardcodedFlagOverrides applies post-parse fixes for flags whose --help
// representation does not expose enum values.
func hardcodedFlagOverrides(spec domain.FlagSpec) domain.FlagSpec {
	switch spec.Long {
	case "cache-type-k", "cache-type-v", "cache-type-k-draft", "cache-type-v-draft":
		spec.Type = domain.FlagTypeEnum
		spec.EnumValues = cacheTypeEnum
	case "defrag-thold":
		// llama-server --help uses placeholder N but the flag accepts float values (0.0–1.0).
		spec.Type = domain.FlagTypeFloat
	case "cors-methods":
		// The documented default is a comma-list ("GET, POST, DELETE, OPTIONS");
		// defaultRe stops at the first comma (intentional for explanatory defaults
		// like ctx-size "0, 0 = loaded from model"), so restore the full value here.
		spec.Default = "GET, POST, DELETE, OPTIONS"
	}
	return spec
}

// parseFlagLine parses a single help line into a FlagSpec. Returns false when
// the line is not a flag definition (header, blank, continuation).
func parseFlagLine(line string) (domain.FlagSpec, bool) {
	if strings.HasPrefix(strings.TrimSpace(line), "(env:") {
		return domain.FlagSpec{}, false
	}
	m := flagLineRe.FindStringSubmatch(strings.TrimRight(line, " "))
	if m == nil {
		return domain.FlagSpec{}, false
	}
	aliasChunk, descChunk := m[1], strings.TrimSpace(m[2])

	short, longs, placeholder := splitAliases(aliasChunk)
	if len(longs) == 0 {
		return domain.FlagSpec{}, false
	}

	canonicalIdx := len(longs) - 1
	// If the last alias is a negation, prefer the positive form earlier in the list.
	if strings.HasPrefix(longs[canonicalIdx], "no-") {
		for i, l := range longs {
			if !strings.HasPrefix(l, "no-") {
				canonicalIdx = i
				break
			}
		}
	}
	spec := domain.FlagSpec{
		Long:     longs[canonicalIdx],
		Short:    short,
		HelpText: descChunk,
	}
	if len(longs) > 1 {
		aliases := make([]string, 0, len(longs)-1)
		aliases = append(aliases, longs[:canonicalIdx]...)
		aliases = append(aliases, longs[canonicalIdx+1:]...)
		spec.Aliases = aliases
	}
	spec.Type = inferType(placeholder)
	if n := metavarArity(aliasChunk); n > 1 {
		spec.Arity = n
	}
	if spec.Type == domain.FlagTypeEnum {
		spec.EnumValues = parseEnumPlaceholder(placeholder)
	}
	// Inline comma-separated enum lists (e.g. --spec-type none,draft-simple,...)
	// appear as a bare comma-separated token without brackets. The placeholder
	// IS the list; parse it as an enum so the live-parse path can preserve
	// per-backend allowed values (prisma dspark vs upstream dflash).
	// Only match when every element looks like a kebab-case identifier
	// (lowercase alphanumeric with hyphens/underscores); exclude patterns
	// like FNAME:SCALE,... or <dev1,dev2,..> that also contain commas.
	if len(spec.EnumValues) == 0 && placeholder != "" && !strings.Contains(placeholder, " ") && strings.Contains(placeholder, ",") && !strings.ContainsAny(placeholder, "<>:") {
		values := splitAndTrim(placeholder, ",")
		if len(values) > 1 && isInlineEnumValues(values) {
			spec.Type = domain.FlagTypeEnum
			spec.EnumValues = values
		}
	}
	// Numeric angle-bracket placeholders like <0|1> or <0...100> carry
	// implicit range constraints. Extract min/max so the validator can
	// enforce them even without curated metadata.
	if spec.Type == domain.FlagTypeInt && angleBracketIntRe.MatchString(placeholder) {
		if m := angleBracketIntRe.FindStringSubmatch(placeholder); m != nil {
			lo, _ := strconv.Atoi(m[1])
			spec.Min = ptrutil.Ptr(lo)
			hiStr := m[2]
			if hiStr == "" {
				hiStr = m[3]
			}
			if hiStr != "" {
				hi, _ := strconv.Atoi(hiStr)
				spec.Max = ptrutil.Ptr(hi)
			}
		}
	}
	rawDefault := extractDefault(descChunk)
	// llama-server overloads the "N" placeholder for both ints and floats
	// (e.g. --top-k N is int, --top-p N is float). When the inferred type is int
	// but the documented default is a decimal, trust the default and treat the
	// flag as a float — otherwise the validator rejects values like 0.95.
	if spec.Type == domain.FlagTypeInt && looksFloat(rawDefault) {
		spec.Type = domain.FlagTypeFloat
	}
	if rawDefault != nil {
		spec.Default = coerceDefault(spec.Type, rawDefault)
		// If coercion failed for a numeric type (result is still string),
		// drop the default so the validator doesn't choke on a non-numeric
		// literal like "read from model".
		if (spec.Type == domain.FlagTypeInt || spec.Type == domain.FlagTypeFloat) && spec.Default != nil {
			if _, ok := spec.Default.(string); ok {
				spec.Default = nil
			}
		}
	}
	spec = hardcodedFlagOverrides(spec)
	return spec, true
}

// splitAliases parses "-c, --ctx-size N" → short="c", longs=["ctx-size"], placeholder="N".
// Multi-alias: "-ngl, --gpu-layers, --n-gpu-layers N" → short="ngl",
// longs=["gpu-layers","n-gpu-layers"], placeholder="N".
// The last whitespace-delimited token in the alias chunk may be a placeholder
// such as "N", "TYPE", "[on|off|auto]", or "{a,b,c}". If the last token starts
// with '-', there is no placeholder.
func splitAliases(chunk string) (short string, longs []string, placeholder string) {
	parts := strings.Fields(chunk)
	if len(parts) == 0 {
		return "", nil, ""
	}
	// Detect placeholder: last token without leading '-'.
	last := parts[len(parts)-1]
	if !strings.HasPrefix(last, "-") {
		placeholder = last
		parts = parts[:len(parts)-1]
	}
	for _, p := range parts {
		p = strings.TrimSuffix(p, ",")
		if strings.HasPrefix(p, "--") {
			longs = append(longs, strings.TrimPrefix(p, "--"))
		} else if strings.HasPrefix(p, "-") && short == "" {
			short = strings.TrimPrefix(p, "-")
		}
	}
	return short, longs, placeholder
}

// metavarRe matches a standalone metavar token: START, END, FNAME, N, SEED.
var metavarRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// metavarArity counts the trailing standalone metavars in an alias chunk, i.e.
// how many argv tokens the flag's value occupies: 2 for
// "--control-vector-layer-range START END", 1 for "--ctx-size N". Punctuated
// placeholders are a single argv token even when they contain spaces
// ("<tensor name pattern>=<buffer type>,...", "FNAME:SCALE,..."), so the
// backwards scan stops at the first token that is not metavar-shaped.
func metavarArity(chunk string) int {
	parts := strings.Fields(chunk)
	n := 0
	for i := len(parts) - 1; i >= 0; i-- {
		if !metavarRe.MatchString(parts[i]) {
			break
		}
		n++
	}
	return n
}

// parseEnumPlaceholder returns the enum values when placeholder is "[a|b|c]"
// or "{a,b,c}". Otherwise returns nil.
func parseEnumPlaceholder(placeholder string) []string {
	if m := bracketEnumRe.FindStringSubmatch(placeholder); m != nil {
		return splitAndTrim(m[1], "|")
	}
	if m := braceEnumRe.FindStringSubmatch(placeholder); m != nil {
		return splitAndTrim(m[1], ",")
	}
	return nil
}

func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// isInlineEnumValues reports whether every string in values looks like an enum
// identifier: lowercase alphanumeric with hyphens and underscores only.
// This guards against false positives for comma-separated placeholders like
// "FNAME:SCALE,..." or "<dev1,dev2,..>".
func isInlineEnumValues(values []string) bool {
	for _, v := range values {
		if v == "" {
			return false
		}
		for _, r := range v {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
				return false
			}
		}
	}
	return true
}

func allowedValuesFromContinuations(lines []string, start int) []string {
	var chunks []string
	for j := start; j < len(lines); j++ {
		trimmed := strings.TrimSpace(lines[j])
		switch {
		case trimmed == "":
			continue
		case strings.HasPrefix(trimmed, "-"):
			return parseAllowedValues(strings.Join(chunks, " "))
		case parseSectionHeader(trimmed) != "":
			return parseAllowedValues(strings.Join(chunks, " "))
		case strings.HasPrefix(trimmed, "(env:"), strings.HasPrefix(trimmed, "(default:"):
			return parseAllowedValues(strings.Join(chunks, " "))
		}
		if _, values, ok := strings.Cut(trimmed, allowedValuesPrefix); ok {
			chunks = append(chunks, strings.TrimSpace(values))
			continue
		}
		if len(chunks) > 0 {
			chunks = append(chunks, trimmed)
		}
	}
	return parseAllowedValues(strings.Join(chunks, " "))
}

func parseAllowedValues(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.Trim(part, "'\""))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
// inferType maps the placeholder token to a FlagType.
func inferType(placeholder string) domain.FlagType {
	switch {
	case placeholder == "":
		return domain.FlagTypeBool
	case parseEnumPlaceholder(placeholder) != nil:
		return domain.FlagTypeEnum
	case angleBracketIntRe.MatchString(placeholder):
		return domain.FlagTypeInt
	}
	// Fallback: scalar. Distinguishing int vs float vs string is best-effort
	// using common llama-server placeholders.
	switch placeholder {
	case "N", "INDEX", "PORT":
		return domain.FlagTypeInt
	case "F", "RATE":
		return domain.FlagTypeFloat
	}
	return domain.FlagTypeString
}

// extractDefault pulls the first "(default: X)" payload from the description.
// Returns nil if absent or if the value is an inherited-description reference
// (e.g. "same as --cpu-strict", "same as --threads-draft").
func extractDefault(desc string) any {
	m := defaultRe.FindStringSubmatch(desc)
	if m == nil {
		return nil
	}
	raw := strings.TrimSpace(m[1])
	// Inherited defaults like "same as --cpu-strict" reference another flag
	// and are not literal values.
	if strings.Contains(raw, "--") {
		return nil
	}
	return raw
}

// looksFloat reports whether the raw default value is a decimal number (i.e.
// parses as a float and carries a fractional part / decimal point). Used to
// disambiguate the overloaded "N" placeholder. Integer-looking defaults such as
// "40" return false so genuine int flags keep their type.
func looksFloat(raw any) bool {
	s, ok := raw.(string)
	if !ok {
		return false
	}
	s = strings.TrimSpace(s)
	if !strings.Contains(s, ".") {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// coerceDefault converts the raw default string to the FlagSpec's typed value.
// Falls back to the original string on parse failure.
func coerceDefault(t domain.FlagType, raw any) any {
	s, ok := raw.(string)
	if !ok {
		return raw
	}
	switch t {
	case domain.FlagTypeInt:
		var n int
		if _, err := fmt.Sscanf(s, "%d", &n); err == nil {
			return n
		}
	case domain.FlagTypeBool:
		switch strings.ToLower(s) {
		case "true", "yes", "1", "on":
			return true
		case "false", "no", "0", "off":
			return false
		}
	case domain.FlagTypeFloat:
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return f
		}
	case domain.FlagTypeEnum:
		return strings.Trim(s, "'\"")
	}
	return s
}
