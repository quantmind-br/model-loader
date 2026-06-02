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
	// ctx-size and port are llama essentials; temp is not.
	if !containsStr(pres.Groups[0].Flags, "ctx-size") || !containsStr(pres.Groups[0].Flags, "port") {
		t.Fatalf("essentials missing: %+v", pres.Groups[0])
	}
	// temp must appear in some non-highlighted group.
	if !flagInAnyGroup(pres, "temp") {
		t.Fatalf("temp should be grouped somewhere: %+v", pres)
	}
}

func TestEssentialSeed_MatchesCuratedBackends(t *testing.T) {
	// Guards against the seed drifting from the curated essentials UX.
	wantDFlashHasPort := false
	for _, f := range essentialSeed[domain.BackendKindDFlash] {
		if f == "port" {
			wantDFlashHasPort = true
		}
	}
	if !wantDFlashHasPort {
		t.Fatal("dflash seed must include port")
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
	for _, want := range []string{"spec-type", "spec-dflash-cross-ctx", "flash-attn"} {
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
