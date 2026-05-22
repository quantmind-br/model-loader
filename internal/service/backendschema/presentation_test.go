package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
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
