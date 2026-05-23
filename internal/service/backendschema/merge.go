package backendschema

import (
	"github.com/quantmind-br/model-loader/internal/domain"
)

// mergeWithCurated overlays curated metadata onto a full flag schema.
// The full schema provides the complete flag set; the curated schema
// provides richer descriptions, groups, aliases, defaults, and the
// presentation layout.  Flags present only in the full schema keep
// their parsed metadata.
func mergeWithCurated(full domain.BackendValidationSchema, curated domain.BackendValidationSchema) domain.BackendValidationSchema {
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
				fullSpec.Aliases = curatedSpec.Aliases
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
			fullSpec.IsPort = curatedSpec.IsPort
			fullSpec.Required = curatedSpec.Required
			merged.Flags[matchKey] = fullSpec
		} else {
			merged.Flags[key] = curatedSpec
		}
	}

	return merged
}
