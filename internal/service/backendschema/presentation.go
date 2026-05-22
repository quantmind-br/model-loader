package backendschema

import (
	"sort"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// essentialSeed lists, per backend kind, the flag long-names that should land
// in the highlighted "Essenciais" group when synthesizing a default
// Presentation. Copied from the retired profile_editor essentials registry.
var essentialSeed = map[domain.BackendKind][]string{
	domain.BackendKindLlamaServer:  {"n-gpu-layers", "ctx-size", "batch-size", "ubatch-size", "port", "flash-attn", "cache-type-k", "cache-type-v"},
	domain.BackendKindBuunLlamaCpp: {"n-gpu-layers", "ctx-size", "batch-size", "ubatch-size", "port", "flash-attn", "cache-type-k", "cache-type-v"},
	domain.BackendKindVLLM:         {"tensor-parallel-size", "gpu-memory-utilization", "max-model-len", "dtype", "quantization", "port", "served-model-name"},
	domain.BackendKindSGLang:       {"tp-size", "dp-size", "mem-fraction-static", "dtype", "quantization", "context-length", "port", "served-model-name"},
	domain.BackendKindDFlash:       {"draft", "max-ctx", "budget", "verify-mode", "cache-type-k", "cache-type-v", "fa-window"},
}

// BuildPresentation synthesizes a default Presentation from a schema:
// the highlighted "Essenciais" group holds the seed flags that exist in the
// schema (in seed order); remaining flags are grouped by FlagSpec.Group
// (alphabetical group order, alphabetical flags within a group).
func BuildPresentation(schema domain.BackendValidationSchema) domain.Presentation {
	essentialSet := map[string]bool{}
	var essentials []string
	for _, long := range essentialSeed[schema.BackendKind] {
		if _, ok := schema.Flags[long]; ok {
			essentials = append(essentials, long)
			essentialSet[long] = true
		}
	}

	byGroup := map[string][]string{}
	for long, spec := range schema.Flags {
		if essentialSet[long] {
			continue
		}
		g := spec.Group
		if g == "" {
			g = "other"
		}
		byGroup[g] = append(byGroup[g], long)
	}

	groups := []domain.PresentationGroup{{
		Name:        "Essenciais",
		Highlighted: true,
		Flags:       essentials,
	}}

	groupNames := make([]string, 0, len(byGroup))
	for g := range byGroup {
		groupNames = append(groupNames, g)
	}
	sort.Strings(groupNames)
	for _, g := range groupNames {
		flags := byGroup[g]
		sort.Strings(flags)
		groups = append(groups, domain.PresentationGroup{Name: g, Flags: flags})
	}
	return domain.Presentation{Groups: groups}
}
