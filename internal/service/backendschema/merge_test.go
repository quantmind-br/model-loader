package backendschema

import (
	"reflect"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestMergeWithCuratedEnrich_NormalizesPresentationAndRetainsParsedIdentity(t *testing.T) {
	full := domain.BackendValidationSchema{Flags: map[string]domain.FlagSpec{
		"parsed-draft-model": {
			Long:    "draft-model",
			Short:   "md",
			Aliases: []string{"spec-draft-model"},
			Type:    domain.FlagTypeString,
		},
		"parsed-host": {
			Long: "host",
			Type: domain.FlagTypeString,
		},
		"parsed-sleep": {
			Long: "sleep-idle-seconds",
			Type: domain.FlagTypeString,
		},
		"parsed-top-p": {
			Long: "top-p",
			Type: domain.FlagTypeEnum,
		},
	}}

	merged := mergeWithCuratedEnrich(full, CuratedLlamaSchema())

	for _, group := range merged.Presentation.Groups {
		for _, ref := range group.Flags {
			if _, ok := merged.Flags[ref]; !ok {
				t.Errorf("presentation reference %q is not a retained map key", ref)
			}
		}
	}

	if _, ok := merged.Flags["spec-draft-model"]; ok {
		t.Fatal("enrich re-keyed the parsed draft-model flag")
	}
	if !presentationContains(merged.Presentation, "parsed-draft-model") {
		t.Fatal("presentation did not normalize spec-draft-model to parsed-draft-model")
	}
	if !presentationContains(merged.Presentation, "host") && !presentationContains(merged.Presentation, "parsed-host") {
		t.Fatal("presentation lost the parsed host flag")
	}

	draft := merged.Flags["parsed-draft-model"]
	if draft.Long != "draft-model" {
		t.Fatalf("parsed Long = %q, want draft-model", draft.Long)
	}
	if !reflect.DeepEqual(draft.Aliases, []string{"spec-draft-model", "model-draft"}) {
		t.Fatalf("parsed aliases = %v, want existing plus curated model-draft alias", draft.Aliases)
	}

	sleep := merged.Flags["parsed-sleep"]
	if sleep.Type != domain.FlagTypeInt || sleep.Default != -1 || sleep.Min == nil || *sleep.Min != 1 {
		t.Fatalf("merged sleep-idle-seconds = %+v, want int default -1 min 1", sleep)
	}
	if !reflect.DeepEqual(sleep.AllowedInts, []int{-1}) {
		t.Fatalf("merged sleep-idle-seconds AllowedInts = %v, want [-1]", sleep.AllowedInts)
	}

	if got := merged.Flags["parsed-host"].Type; got != domain.FlagTypeString {
		t.Fatalf("parser-compatible host type = %v, want string", got)
	}
	if got := merged.Flags["parsed-top-p"].Type; got != domain.FlagTypeEnum {
		t.Fatalf("concrete parser type top-p = %v, want enum", got)
	}
}

func TestMergeWithCuratedEnrich_BeeLlamaRepairsNumericFlagsAndAvoidsAliasCollision(t *testing.T) {
	full := domain.BackendValidationSchema{Flags: map[string]domain.FlagSpec{
		"parsed-draft-model": {
			Long:    "draft-model",
			Aliases: []string{"spec-draft-model"},
			Type:    domain.FlagTypeString,
		},
		"separate-model-draft": {
			Long: "model-draft",
			Type: domain.FlagTypeString,
		},
		"parsed-sleep": {
			Long: "sleep-idle-seconds",
			Type: domain.FlagTypeString,
		},
		"parsed-host": {
			Long: "host",
			Type: domain.FlagTypeString,
		},
	}}

	merged := mergeWithCuratedEnrich(full, CuratedBeeLlamaSchema())

	draft := merged.Flags["parsed-draft-model"]
	if draft.Long != "draft-model" {
		t.Fatalf("draft model Long = %q, want draft-model", draft.Long)
	}
	if !reflect.DeepEqual(draft.Aliases, []string{"spec-draft-model"}) {
		t.Fatalf("draft model aliases = %v, want existing parsed aliases only", draft.Aliases)
	}
	if got := merged.Flags["separate-model-draft"].Aliases; len(got) != 0 {
		t.Fatalf("separate parsed flag aliases changed: %v", got)
	}

	sleep := merged.Flags["parsed-sleep"]
	if sleep.Type != domain.FlagTypeInt || sleep.Default != -1 || sleep.Min == nil || *sleep.Min != 1 {
		t.Fatalf("merged BeeLlama sleep-idle-seconds = %+v, want int default -1 min 1", sleep)
	}
	if !reflect.DeepEqual(sleep.AllowedInts, []int{-1}) {
		t.Fatalf("merged BeeLlama sleep-idle-seconds AllowedInts = %v, want [-1]", sleep.AllowedInts)
	}
	if got := merged.Flags["parsed-host"].Type; got != domain.FlagTypeString {
		t.Fatalf("parser-compatible host type = %v, want string", got)
	}

	for _, group := range merged.Presentation.Groups {
		for _, ref := range group.Flags {
			if _, ok := merged.Flags[ref]; !ok {
				t.Errorf("BeeLlama presentation reference %q is not a retained map key", ref)
			}
		}
	}
	if !presentationContains(merged.Presentation, "parsed-draft-model") {
		t.Fatal("BeeLlama presentation did not normalize draft-model reference")
	}
}

func TestMergeWithCuratedEnrich_BuunRepairsVBRFloatsAndAllowedIntsCopy(t *testing.T) {
	full := domain.BackendValidationSchema{Flags: map[string]domain.FlagSpec{
		"parsed-reclaim": {
			Long: "vbr-reclaim-floor",
			Type: domain.FlagTypeString,
		},
		"parsed-reset": {
			Long: "vbr-reset-keep-frac",
			Type: domain.FlagTypeString,
		},
		"parsed-sleep": {
			Long: "sleep-idle-seconds",
			Type: domain.FlagTypeString,
		},
	}}

	curated := CuratedBuunSchema()
	merged := mergeWithCuratedEnrich(full, curated)

	for _, tc := range []struct {
		key  string
		want any
	}{
		{key: "parsed-reclaim", want: 8.125},
		{key: "parsed-reset", want: 0.25},
	} {
		spec := merged.Flags[tc.key]
		if spec.Type != domain.FlagTypeFloat {
			t.Errorf("%s type = %v, want float", tc.key, spec.Type)
		}
		if spec.Default != tc.want {
			t.Errorf("%s default = %v, want %v", tc.key, spec.Default, tc.want)
		}
	}

	sleep := merged.Flags["parsed-sleep"]
	if !reflect.DeepEqual(sleep.AllowedInts, []int{-1}) {
		t.Fatalf("Buun sleep-idle-seconds AllowedInts = %v, want [-1]", sleep.AllowedInts)
	}
	curated.Flags["sleep-idle-seconds"].AllowedInts[0] = 99
	if got := merged.Flags["parsed-sleep"].AllowedInts[0]; got != -1 {
		t.Fatalf("merged AllowedInts was not defensively copied: %v", merged.Flags["parsed-sleep"].AllowedInts)
	}
}

func TestMergeWithCurated_AddedAliasesAvoidSelfAndParsedCollisions(t *testing.T) {
	full := domain.BackendValidationSchema{Flags: map[string]domain.FlagSpec{
		"parsed-target": {
			Long: "new-flag",
			Type: domain.FlagTypeString,
		},
		"parsed-existing": {
			Long: "existing",
			Type: domain.FlagTypeString,
		},
	}}
	curated := domain.BackendValidationSchema{
		Flags: map[string]domain.FlagSpec{
			"new-flag": {
				Long:    "new-flag",
				Short:   "n",
				Aliases: []string{"new-flag", "existing", "new-alias"},
				Type:    domain.FlagTypeString,
			},
		},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{{Flags: []string{"new-flag"}}}},
	}

	merged := mergeWithCurated(full, curated)
	spec := merged.Flags["parsed-target"]
	if !reflect.DeepEqual(spec.Aliases, []string{"new-alias"}) {
		t.Fatalf("appended aliases = %v, want [new-alias]", spec.Aliases)
	}
	if got := resolveFlagKey(merged.Flags, "existing"); got != "parsed-existing" {
		t.Fatalf("existing alias resolved to %q, want parsed-existing", got)
	}
	if got := resolveFlagKey(merged.Flags, "new-alias"); got != "parsed-target" {
		t.Fatalf("new alias resolved to %q, want parsed-target", got)
	}
}

func TestMergeWithCurated_AppendedFlagKeepsNonConflictingAliases(t *testing.T) {
	curated := domain.BackendValidationSchema{
		Flags: map[string]domain.FlagSpec{
			"curated-only": {
				Long:    "curated-only",
				Aliases: []string{"legacy-curated-only"},
				Type:    domain.FlagTypeString,
			},
		},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{{Flags: []string{"curated-only"}}}},
	}

	merged := mergeWithCurated(domain.BackendValidationSchema{}, curated).ToFlagSchema()
	if _, ok := merged.Lookup("legacy-curated-only"); !ok {
		t.Fatal("appended curated flag lost its non-conflicting alias")
	}
}

func presentationContains(pres *domain.Presentation, want string) bool {
	if pres == nil {
		return false
	}
	for _, group := range pres.Groups {
		for _, ref := range group.Flags {
			if ref == want {
				return true
			}
		}
	}
	return false
}
