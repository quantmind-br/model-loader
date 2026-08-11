package backendschema

import (
	"fmt"
	"sort"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

// essentialSeed lists, per backend kind, the flag long-names that should land
// in the highlighted "Essentials" group when synthesizing a default
// Presentation. Copied from the retired profile_editor essentials registry.
// "port" is intentionally absent because the process manager owns allocation.
var essentialSeed = map[domain.BackendKind][]string{
	domain.BackendKindLlamaServer:  {"n-gpu-layers", "ctx-size", "batch-size", "ubatch-size", "flash-attn", "cache-type-k", "cache-type-v"},
	domain.BackendKindBuunLlamaCpp: {"n-gpu-layers", "ctx-size", "cache-type-k", "cache-type-v", "spec-type", "spec-draft-model", "spec-dflash-default", "dflash-max-slots"},
	domain.BackendKindBeeLlamaCpp:  {"n-gpu-layers", "ctx-size", "flash-attn", "cache-type-k", "cache-type-v", "cache-type-k-swa", "cache-type-v-swa", "kv-tail-tokens", "kv-tail-type", "cache-ram", "kv-unified", "spec-type", "spec-draft-hf", "spec-draft-model", "spec-draft-ngl"},
	domain.BackendKindVLLM:         {"tensor-parallel-size", "gpu-memory-utilization", "max-model-len", "dtype", "quantization", "served-model-name"},
	domain.BackendKindSGLang:       {"tp-size", "dp-size", "mem-fraction-static", "dtype", "quantization", "context-length", "served-model-name"},
	domain.BackendKindDFlash:       {"draft", "max-ctx", "ddtree", "ddtree-budget", "cache-type-k", "cache-type-v", "fa-window"},
	domain.BackendKindUnsloth:      {"gguf-variant", "max-seq-length", "ctx-size", "n-gpu-layers", "gpu-memory-mode", "tensor-parallel", "parallel", "flash-attn", "cache-type-k", "cache-type-v", "speculative-type"},
	domain.BackendKindTabby:        {"max-seq-len", "cache-mode", "cache-size", "max-batch-size", "tensor-parallel", "gpu-split", "gpu-split-auto", "vision", "cpu-moe-offload-layers", "draft-mode", "tool-format"},
}

// BuildPresentation synthesizes a default Presentation from a schema:
// the highlighted "Essentials" group holds the seed flags that exist in the
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
		Name:        "Essentials",
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

// ReconcilePresentation carries an operator-curated layout across a
// regeneration: groups and their order come from prev, the flag set from next.
// Flags prev names that the regenerated schema no longer has are dropped;
// flags next added that no preserved group surfaces are appended to the group
// naming them in next.Presentation (falling back to FlagSpec.Group, then
// "other"), creating that group at the end when absent. Groups left empty are
// dropped, so a backend that lost a whole flag family loses its tab too.
func ReconcilePresentation(prev domain.Presentation, next domain.BackendValidationSchema) domain.Presentation {
	kept := map[string]bool{}
	groups := make([]domain.PresentationGroup, 0, len(prev.Groups))
	for _, g := range prev.Groups {
		flags := make([]string, 0, len(g.Flags))
		for _, long := range g.Flags {
			if _, ok := next.Flags[long]; !ok {
				continue
			}
			if kept[long] {
				continue // a duplicate listing survives only once
			}
			kept[long] = true
			flags = append(flags, long)
		}
		if len(flags) == 0 {
			continue
		}
		g.Flags = flags
		groups = append(groups, g)
	}

	byIndex := map[string]int{}
	for i, g := range groups {
		byIndex[g.Name] = i
	}
	missing := make([]string, 0, len(next.Flags))
	for long := range next.Flags {
		if !kept[long] {
			missing = append(missing, long)
		}
	}
	sort.Strings(missing)
	for _, long := range missing {
		target := nextGroupFor(long, next)
		i, ok := byIndex[target]
		if !ok {
			groups = append(groups, domain.PresentationGroup{Name: target})
			i = len(groups) - 1
			byIndex[target] = i
		}
		groups[i].Flags = append(groups[i].Flags, long)
	}
	return domain.Presentation{Groups: groups}
}

// nextGroupFor names the group a regenerated flag belongs to: the group that
// lists it in the regenerated presentation, else its FlagSpec.Group, else
// "other".
func nextGroupFor(long string, next domain.BackendValidationSchema) string {
	if next.Presentation != nil {
		for _, g := range next.Presentation.Groups {
			for _, f := range g.Flags {
				if f == long {
					return g.Name
				}
			}
		}
	}
	if g := next.Flags[long].Group; g != "" {
		return g
	}
	return "other"
}

// ReconcileRules carries operator cross-field rules across a regeneration,
// dropping any rule that references a flag the regenerated schema no longer
// has. Keeping such a rule would persist a schema configweb's own rule editor
// rejects ("rule references unknown flag"), locking the operator out of saving
// rules until they hand-edited the file.
func ReconcileRules(prev []domain.CrossFieldRule, next domain.BackendValidationSchema) []domain.CrossFieldRule {
	out := make([]domain.CrossFieldRule, 0, len(prev))
	for _, r := range prev {
		if _, ok := next.Flags[r.When.Flag]; !ok {
			continue
		}
		if r.Then.Flag != "" {
			if _, ok := next.Flags[r.Then.Flag]; !ok {
				continue
			}
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// EnsurePresentations seeds a default Presentation into every backend schema in
// the catalog that lacks one. Idempotent; returns the count updated. Does NOT
// mark Source.Editable (a synthesized default is not a manual edit).
func EnsurePresentations(catalogStore backendcatalog.Store, schemaStore backendcatalog.SchemaStore) (int, error) {
	catalog, err := catalogStore.Load()
	if err != nil {
		return 0, fmt.Errorf("load catalog: %w", err)
	}
	updated := 0
	for _, b := range catalog.Backends {
		ref := backendcatalog.SchemaStoreRef(b.SchemaRef)
		schema, err := schemaStore.Load(ref)
		if err != nil {
			continue // missing schema handled elsewhere; skip
		}
		if schema.Presentation != nil {
			continue
		}
		pres := BuildPresentation(schema)
		schema.Presentation = &pres
		if err := schemaStore.Save(ref, schema); err != nil {
			return updated, fmt.Errorf("save presentation for %s: %w", b.ID, err)
		}
		updated++
	}
	return updated, nil
}
