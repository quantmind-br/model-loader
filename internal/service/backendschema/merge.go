package backendschema

import (
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
	merged.Presentation = curated.Presentation
	merged.Rules = curated.Rules

	if merged.Flags == nil {
		merged.Flags = map[string]domain.FlagSpec{}
	}

	index := map[string]string{}
	for k, spec := range merged.Flags {
		if spec.Long != "" {
			index[spec.Long] = k
		}
		if spec.Short != "" {
			index[spec.Short] = k
		}
		for _, a := range spec.Aliases {
			if a != "" {
				index[a] = k
			}
		}
	}

	for key, curatedSpec := range curated.Flags {
		matchKey := ""
		if _, ok := merged.Flags[key]; ok {
			matchKey = key
		} else if mk, ok := index[curatedSpec.Long]; ok {
			matchKey = mk
		} else if mk, ok := index[curatedSpec.Short]; ok {
			matchKey = mk
		} else {
			for _, a := range curatedSpec.Aliases {
				if mk, ok := index[a]; ok {
					matchKey = mk
					break
				}
			}
		}

		if matchKey != "" {
			fullSpec := merged.Flags[matchKey]
			if curatedSpec.HelpText != "" {
				fullSpec.HelpText = curatedSpec.HelpText
			}
			if curatedSpec.Group != "" {
				fullSpec.Group = curatedSpec.Group
			}
			if len(curatedSpec.Aliases) > 0 {
				for _, alias := range curatedSpec.Aliases {
					seen := false
					for _, existing := range fullSpec.Aliases {
						if existing == alias {
							seen = true
							break
						}
					}
					if !seen {
						fullSpec.Aliases = append(fullSpec.Aliases, alias)
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
				fullSpec.EnumValues = curatedSpec.EnumValues
			}
			// Honor the curated Type for list-valued enums. The live --help parses
			// comma-list flags (e.g. llama-server --spec-type) as a plain string
			// (Type:3); without this overlay the validator's List per-element check
			// never engages and typos slip through. Gated on curatedSpec.List so the
			// parsed Type is preserved for every other flag (including beellama/buun
			// spec-type, which is list:false).
			if curatedSpec.List && curatedSpec.Type == domain.FlagTypeEnum {
				fullSpec.Type = curatedSpec.Type
			}
			fullSpec.IsPort = curatedSpec.IsPort
			fullSpec.Required = curatedSpec.Required
			fullSpec.List = curatedSpec.List
			fullSpec.Keywords = curatedSpec.Keywords
			// Arity is per-binary: the live --help metavar count is authoritative
			// when it found one, so a curated 0 (unset) must not erase it. A
			// curated value only overrides when it actually declares multi-token.
			if curatedSpec.Arity > 1 {
				fullSpec.Arity = curatedSpec.Arity
			}
			merged.Flags[matchKey] = fullSpec
		} else if appendMissing {
			merged.Flags[key] = curatedSpec
		}
	}

	if !appendMissing {
		merged.Presentation = prunePresentation(merged.Presentation, merged.Flags)
	}

	return merged
}

// prunePresentation returns a copy of pres with every group's flag list
// filtered to names present in flags, dropping references the authoritative
// backend surface does not define. Group order and highlighting are preserved.
func prunePresentation(pres *domain.Presentation, flags map[string]domain.FlagSpec) *domain.Presentation {
	if pres == nil {
		return nil
	}
	groups := make([]domain.PresentationGroup, 0, len(pres.Groups))
	for _, g := range pres.Groups {
		kept := make([]string, 0, len(g.Flags))
		for _, long := range g.Flags {
			if _, ok := flags[long]; ok {
				kept = append(kept, long)
			}
		}
		g.Flags = kept
		groups = append(groups, g)
	}
	return &domain.Presentation{Groups: groups}
}
