package llamahelp

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// cacheTypeEnum lists the KV-cache quant types accepted by --cache-type-{k,v}.
// The --help output renders these as TYPE; we hardcode the well-known set so
// the editor can offer a select.
var cacheTypeEnum = []string{"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1"}

// ParseHelp scans the full --help output and returns a FlagSchema.
// Lines before the first section header are skipped (CUDA banner etc).
// Continuation lines are ignored; only the first line of each flag is parsed.
func ParseHelp(data []byte) (domain.FlagSchema, error) {
	schema := domain.FlagSchema{Flags: make(map[string]domain.FlagSpec)}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	currentGroup := ""
	for scanner.Scan() {
		line := scanner.Text()
		if header := parseSectionHeader(line); header != "" {
			currentGroup = header
			continue
		}
		if currentGroup == "" {
			continue
		}
		spec, ok := parseFlagLine(line)
		if !ok {
			continue
		}
		spec.Group = currentGroup
		schema.Flags[spec.Long] = spec
	}
	if err := scanner.Err(); err != nil {
		return domain.FlagSchema{}, err
	}
	return schema, nil
}

var sectionHeaderRe = regexp.MustCompile(`^-{5}\s+(.+?)\s+params\s+-{5}$`)

var (
	bracketEnumRe = regexp.MustCompile(`^\[([^\]]+)\]$`)
	braceEnumRe   = regexp.MustCompile(`^\{([^}]+)\}$`)
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

// hardcodedFlagOverrides applies post-parse fixes for flags whose --help
// representation does not expose enum values.
func hardcodedFlagOverrides(spec domain.FlagSpec) domain.FlagSpec {
	switch spec.Long {
	case "cache-type-k", "cache-type-v", "cache-type-k-draft", "cache-type-v-draft":
		spec.Type = domain.FlagTypeEnum
		spec.EnumValues = cacheTypeEnum
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
	if spec.Type == domain.FlagTypeEnum {
		spec.EnumValues = parseEnumPlaceholder(placeholder)
	}
	if d := extractDefault(descChunk); d != nil {
		spec.Default = coerceDefault(spec.Type, d)
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

// inferType maps the placeholder token to a FlagType.
func inferType(placeholder string) domain.FlagType {
	switch {
	case placeholder == "":
		return domain.FlagTypeBool
	case parseEnumPlaceholder(placeholder) != nil:
		return domain.FlagTypeEnum
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
// Returns nil if absent.
func extractDefault(desc string) any {
	m := defaultRe.FindStringSubmatch(desc)
	if m == nil {
		return nil
	}
	return strings.TrimSpace(m[1])
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
		case "true", "yes", "1":
			return true
		case "false", "no", "0":
			return false
		}
	case domain.FlagTypeEnum:
		return strings.Trim(s, "'\"")
	}
	return s
}
