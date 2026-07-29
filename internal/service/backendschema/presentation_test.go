package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestBuildPresentation_HighlightsEssentialsFirst(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer,
		Flags: map[string]domain.FlagSpec{
			"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt, Group: "common"},
			"temp":     {Long: "temp", Type: domain.FlagTypeFloat, Group: "sampling"},
			"port":     {Long: "port", Type: domain.FlagTypeInt, Group: "common"},
		},
	}
	pres := BuildPresentation(schema)
	if len(pres.Groups) == 0 || !pres.Groups[0].Highlighted {
		t.Fatalf("first group must be highlighted essentials: %+v", pres)
	}
	// ctx-size is a llama essential; temp is not. port is manager-owned and
	// must never be seeded into essentials.
	if !containsStr(pres.Groups[0].Flags, "ctx-size") {
		t.Fatalf("essentials missing: %+v", pres.Groups[0])
	}
	if containsStr(pres.Groups[0].Flags, "port") {
		t.Fatalf("reserved flag port must not be seeded into essentials: %+v", pres.Groups[0])
	}
	// temp must appear in some non-highlighted group.
	if !flagInAnyGroup(pres, "temp") {
		t.Fatalf("temp should be grouped somewhere: %+v", pres)
	}
}

func TestEssentialSeed_MatchesCuratedBackends(t *testing.T) {
	// Guards against the seed drifting from the curated essentials UX.
	// port is manager-owned (auto-allocated) and must not appear in any seed.
	for kind, flags := range essentialSeed {
		for _, f := range flags {
			if f == "port" {
				t.Fatalf("seed for %s must not include reserved flag port", kind)
			}
		}
	}
	for _, f := range essentialSeed[domain.BackendKindBuunLlamaCpp] {
		if f == "batch-size" || f == "ubatch-size" {
			t.Fatalf("buun seed must not contain llama-only flag %q", f)
		}
	}
	for _, want := range []string{"spec-type", "spec-draft-model"} {
		found := false
		for _, f := range essentialSeed[domain.BackendKindBuunLlamaCpp] {
			if f == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("buun seed missing curated flag %q", want)
		}
	}
	for _, want := range []string{"spec-type", "kv-tail-tokens", "flash-attn"} {
		found := false
		for _, f := range essentialSeed[domain.BackendKindBeeLlamaCpp] {
			if f == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("beellama seed missing curated flag %q", want)
		}
	}
}

func TestEnsurePresentations_SeedsCatalog(t *testing.T) {
	dir := t.TempDir()
	catalogStore := backendcatalog.NewFSStore(dir)
	schemaStore := backendcatalog.NewFSSchemaStore(dir)

	// Build a catalog with one llama-server backend.
	backend := domain.Backend{
		ID:         "test-llama",
		Name:       "test-llama",
		Kind:       domain.BackendKindLlamaServer,
		Executable: "llama-server",
		SchemaRef:  "schemas/test-llama.json",
	}
	catalog := domain.BackendCatalog{
		SchemaVersion:    1,
		DefaultBackendID: backend.ID,
		Backends:         []domain.Backend{backend},
	}
	if err := catalogStore.Save(catalog); err != nil {
		t.Fatalf("save catalog: %v", err)
	}

	// Schema without Presentation.
	schema := domain.BackendValidationSchema{
		SchemaVersion: 1,
		BackendID:     backend.ID,
		BackendKind:   domain.BackendKindLlamaServer,
		Flags: map[string]domain.FlagSpec{
			"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt, Group: "common"},
			"port":     {Long: "port", Type: domain.FlagTypeInt, Group: "common"},
			"temp":     {Long: "temp", Type: domain.FlagTypeFloat, Group: "sampling"},
		},
	}
	ref := backendcatalog.SchemaStoreRef(backend.SchemaRef)
	if err := schemaStore.Save(ref, schema); err != nil {
		t.Fatalf("save schema: %v", err)
	}

	// First call must seed 1 presentation.
	n, err := EnsurePresentations(catalogStore, schemaStore)
	if err != nil {
		t.Fatalf("EnsurePresentations: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 updated, got %d", n)
	}

	// Reloaded schema must have a non-empty Presentation with highlighted first group.
	updated, err := schemaStore.Load(ref)
	if err != nil {
		t.Fatalf("load updated schema: %v", err)
	}
	if updated.Presentation == nil {
		t.Fatal("Presentation must not be nil after seeding")
	}
	if len(updated.Presentation.Groups) == 0 {
		t.Fatal("Presentation.Groups must not be empty")
	}
	if !updated.Presentation.Groups[0].Highlighted {
		t.Fatalf("first group must be highlighted: %+v", updated.Presentation.Groups[0])
	}

	// Second call must be idempotent (0 updated).
	n2, err := EnsurePresentations(catalogStore, schemaStore)
	if err != nil {
		t.Fatalf("EnsurePresentations (2nd): %v", err)
	}
	if n2 != 0 {
		t.Fatalf("want 0 on 2nd call, got %d", n2)
	}
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func flagInAnyGroup(p domain.Presentation, flag string) bool {
	for _, g := range p.Groups {
		if containsStr(g.Flags, flag) {
			return true
		}
	}
	return false
}

// S14: ReconcilePresentation is what lets a customized layout survive a
// regeneration without hiding newly added backend flags or naming removed ones.
func TestReconcilePresentation_KeepsOrderDropsRemovedAppendsAdded(t *testing.T) {
	prev := domain.Presentation{Groups: []domain.PresentationGroup{
		{Name: "MyEssentials", Highlighted: true, Flags: []string{"ctx-size", "gone", "threads"}},
		{Name: "Empty", Flags: []string{"also-gone"}},
	}}
	next := domain.BackendValidationSchema{
		Flags: map[string]domain.FlagSpec{
			"ctx-size":  {Long: "ctx-size", Group: "common"},
			"threads":   {Long: "threads", Group: "common"},
			"brand-new": {Long: "brand-new", Group: "sampling"},
			"ungrouped": {Long: "ungrouped"},
		},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{
			{Name: "MyEssentials", Flags: []string{"brand-new"}},
		}},
	}

	got := ReconcilePresentation(prev, next)

	if len(got.Groups) != 2 {
		t.Fatalf("want the surviving group plus one for the ungrouped flag: %+v", got.Groups)
	}
	first := got.Groups[0]
	if first.Name != "MyEssentials" || !first.Highlighted {
		t.Fatalf("custom group identity lost: %+v", first)
	}
	// Order preserved, removed flags dropped, and the flag the regenerated
	// presentation assigns to this group appended.
	want := []string{"ctx-size", "threads", "brand-new"}
	if len(first.Flags) != len(want) {
		t.Fatalf("flags = %v, want %v", first.Flags, want)
	}
	for i, f := range want {
		if first.Flags[i] != f {
			t.Fatalf("flags = %v, want %v", first.Flags, want)
		}
	}
	if got.Groups[1].Name != "other" || !containsStr(got.Groups[1].Flags, "ungrouped") {
		t.Fatalf("a flag with no group must land in \"other\": %+v", got.Groups[1])
	}
	if flagInAnyGroup(got, "gone") || flagInAnyGroup(got, "also-gone") {
		t.Fatalf("removed flags must not survive: %+v", got.Groups)
	}
}

// S14: rules referencing a flag the regeneration dropped must not survive —
// configweb's rule editor refuses to save a schema containing one.
func TestReconcileRules_DropsRulesNamingRemovedFlags(t *testing.T) {
	next := domain.BackendValidationSchema{Flags: map[string]domain.FlagSpec{
		"ctx-size": {Long: "ctx-size"},
	}}
	got := ReconcileRules([]domain.CrossFieldRule{
		{ID: "keep", When: domain.Cond{Flag: "ctx-size"}},
		{ID: "drop-when", When: domain.Cond{Flag: "threads"}},
		{ID: "drop-then", When: domain.Cond{Flag: "ctx-size"}, Then: domain.Effect{Flag: "threads"}},
	}, next)
	if len(got) != 1 || got[0].ID != "keep" {
		t.Fatalf("rules = %+v, want only \"keep\"", got)
	}
}
