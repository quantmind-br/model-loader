package backendschema

import (
	"sort"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// mergeWithCurated overlays curated metadata onto a full flag schema and
// appends curated-only flags absent from the full set. Used for the embedded
// golden fallback and the Bee/Buun forks, whose curated overlays intentionally
// supply fork-only flags the parsed --help omits.
func mergeWithCurated(full domain.BackendValidationSchema, curated domain.BackendValidationSchema) domain.BackendValidationSchema {
	return mergeCurated(full, curated, true)
}

// mergeWithCuratedEnrich overlays curated metadata but never appends
// curated-only flags, so the parsed backend surface stays authoritative on flag
// existence. Used for the llama-server live-parse path: forks that trail
// upstream must not inherit curated flags their binary rejects at launch.
func mergeWithCuratedEnrich(full domain.BackendValidationSchema, curated domain.BackendValidationSchema) domain.BackendValidationSchema {
	return mergeCurated(full, curated, false)
}

// mergeCurated is the shared implementation. When appendMissing is false,
// curated flags with no match in full are dropped rather than injected.
func mergeCurated(full domain.BackendValidationSchema, curated domain.BackendValidationSchema, appendMissing bool) domain.BackendValidationSchema {
	merged := full
	merged.Presentation = clonePresentation(curated.Presentation)
	merged.Rules = curated.Rules

	if merged.Flags == nil {
		merged.Flags = map[string]domain.FlagSpec{}
	} else {
		flags := make(map[string]domain.FlagSpec, len(merged.Flags))
		for key, spec := range merged.Flags {
			flags[key] = cloneFlagSpec(spec)
		}
		merged.Flags = flags
	}

	// Resolve curated entries against the parsed surface before appending any
	// missing entries. This keeps a curated alias from accidentally matching a
	// different curated-only entry merely because map iteration appended it first.
	parsedFlags := make(map[string]domain.FlagSpec, len(merged.Flags))
	for key, spec := range merged.Flags {
		parsedFlags[key] = spec
	}
	curatedKeys := sortedFlagKeys(curated.Flags)
	matches := make(map[string]string, len(curatedKeys))
	for _, key := range curatedKeys {
		curatedSpec := curated.Flags[key]
		matchKey := resolveFlagKey(parsedFlags, key)
		if matchKey == "" {
			matchKey = resolveFlagKey(parsedFlags, curatedSpec.Long)
		}
		if matchKey == "" {
			matchKey = resolveFlagKey(parsedFlags, curatedSpec.Short)
		}
		if matchKey == "" {
			for _, alias := range curatedSpec.Aliases {
				if matchKey = resolveFlagKey(parsedFlags, alias); matchKey != "" {
					break
				}
			}
		}
		matches[key] = matchKey
	}

	for _, key := range curatedKeys {
		matchKey := matches[key]
		if matchKey == "" && appendMissing {
			matchKey = key
			spec := cloneFlagSpec(curated.Flags[key])
			spec.Aliases = mergeAddedAliases(spec, key, spec.Aliases, merged.Flags)
			merged.Flags[key] = spec
			matches[key] = matchKey
		}
	}

	for _, key := range curatedKeys {
		curatedSpec := curated.Flags[key]
		matchKey := matches[key]
		if matchKey != "" {
			fullSpec := merged.Flags[matchKey]
			if curatedSpec.HelpText != "" {
				fullSpec.HelpText = curatedSpec.HelpText
			}
			if curatedSpec.Group != "" {
				fullSpec.Group = curatedSpec.Group
			}
			if len(curatedSpec.Aliases) > 0 {
				if _, parsed := parsedFlags[matchKey]; parsed {
					for _, alias := range curatedSpec.Aliases {
						if alias == "" || identifiesFlag(fullSpec, matchKey, alias) || identifiesOtherFlag(merged.Flags, matchKey, alias) {
							continue
						}
						if !containsString(fullSpec.Aliases, alias) {
							fullSpec.Aliases = append(fullSpec.Aliases, alias)
						}
					}
				}
			}
			if curatedSpec.Short != "" {
				fullSpec.Short = curatedSpec.Short
			}
			if curatedSpec.Default != nil {
				fullSpec.Default = curatedSpec.Default
			}
			if curatedSpec.Min != nil {
				fullSpec.Min = curatedSpec.Min
			}
			if curatedSpec.Max != nil {
				fullSpec.Max = curatedSpec.Max
			}
			if curatedSpec.FloatMin != nil {
				fullSpec.FloatMin = curatedSpec.FloatMin
			}
			if curatedSpec.FloatMax != nil {
				fullSpec.FloatMax = curatedSpec.FloatMax
			}
			if len(curatedSpec.EnumValues) > 0 {
				fullSpec.EnumValues = append([]string(nil), curatedSpec.EnumValues...)
			}
			// The help parser cannot distinguish a numeric flag whose description
			// only says "string" from a genuinely textual flag. Curated numeric
			// metadata repairs that one parser limitation, but concrete parser types
			// remain authoritative. The list-enum case is the same parser-string
			// exception for comma-separated enum flags.
			if fullSpec.Type == domain.FlagTypeString && (curatedSpec.Type == domain.FlagTypeInt || curatedSpec.Type == domain.FlagTypeFloat) {
				fullSpec.Type = curatedSpec.Type
			} else if fullSpec.Type == domain.FlagTypeString && curatedSpec.List && curatedSpec.Type == domain.FlagTypeEnum {
				fullSpec.Type = curatedSpec.Type
			}
			fullSpec.IsPort = curatedSpec.IsPort
			fullSpec.Required = curatedSpec.Required
			fullSpec.List = curatedSpec.List
			fullSpec.Keywords = append([]string(nil), curatedSpec.Keywords...)
			if len(curatedSpec.AllowedInts) > 0 {
				fullSpec.AllowedInts = append([]int(nil), curatedSpec.AllowedInts...)
			}
			// Arity is per-binary: the live --help metavar count is authoritative
			// when it found one, so a curated 0 (unset) must not erase it. A
			// curated value only overrides when it actually declares multi-token.
			if curatedSpec.Arity > 1 {
				fullSpec.Arity = curatedSpec.Arity
			}
			merged.Flags[matchKey] = fullSpec
		}
	}
	merged.Presentation = normalizePresentation(merged.Presentation, merged.Flags, !appendMissing)

	return merged
}

func cloneFlagSpec(spec domain.FlagSpec) domain.FlagSpec {
	spec.Aliases = append([]string(nil), spec.Aliases...)
	spec.EnumValues = append([]string(nil), spec.EnumValues...)
	spec.Keywords = append([]string(nil), spec.Keywords...)
	spec.AllowedInts = append([]int(nil), spec.AllowedInts...)
	return spec
}

func clonePresentation(pres *domain.Presentation) *domain.Presentation {
	if pres == nil {
		return nil
	}
	groups := make([]domain.PresentationGroup, 0, len(pres.Groups))
	for _, g := range pres.Groups {
		g.Flags = append([]string(nil), g.Flags...)
		groups = append(groups, g)
	}
	return &domain.Presentation{Groups: groups}
}

// normalizePresentation rewrites each presentation reference to the retained
// schema map key. In enrich mode unresolved references are pruned because the
// parsed flag surface is authoritative; fallback merges retain unresolved
// references for compatibility with older curated layouts.
func normalizePresentation(pres *domain.Presentation, flags map[string]domain.FlagSpec, pruneUnknown bool) *domain.Presentation {
	if pres == nil {
		return nil
	}
	groups := make([]domain.PresentationGroup, 0, len(pres.Groups))
	for _, group := range pres.Groups {
		refs := make([]string, 0, len(group.Flags))
		for _, ref := range group.Flags {
			if key := resolveFlagKey(flags, ref); key != "" {
				refs = append(refs, key)
			} else if !pruneUnknown {
				refs = append(refs, ref)
			}
		}
		group.Flags = refs
		groups = append(groups, group)
	}
	return &domain.Presentation{Groups: groups}
}

func sortedFlagKeys(flags map[string]domain.FlagSpec) []string {
	keys := make([]string, 0, len(flags))
	for key := range flags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// resolveFlagKey resolves in the same order exposed by FlagSchema.Lookup,
// while choosing a stable map key if malformed parsed input has duplicate
// identifiers.
func resolveFlagKey(flags map[string]domain.FlagSpec, name string) string {
	if name == "" {
		return ""
	}
	if _, ok := flags[name]; ok {
		return name
	}
	keys := sortedFlagKeys(flags)
	for _, key := range keys {
		if flags[key].Long == name {
			return key
		}
	}
	for _, key := range keys {
		if flags[key].Short == name {
			return key
		}
	}
	for _, key := range keys {
		if containsString(flags[key].Aliases, name) {
			return key
		}
	}
	return ""
}

func identifiesFlag(spec domain.FlagSpec, key, name string) bool {
	return key == name || spec.Long == name || spec.Short == name || containsString(spec.Aliases, name)
}

func identifiesOtherFlag(flags map[string]domain.FlagSpec, targetKey, name string) bool {
	for key, spec := range flags {
		if key != targetKey && identifiesFlag(spec, key, name) {
			return true
		}
	}
	return false
}

func mergeAddedAliases(spec domain.FlagSpec, key string, aliases []string, flags map[string]domain.FlagSpec) []string {
	merged := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		if alias == "" || alias == key || alias == spec.Long || alias == spec.Short || identifiesOtherFlag(flags, key, alias) {
			continue
		}
		if !containsString(merged, alias) {
			merged = append(merged, alias)
		}
	}
	return merged
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
