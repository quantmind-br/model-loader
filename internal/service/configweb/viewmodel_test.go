package configweb

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestBuildViewModel_NilPresentationUsesCuratedGroups(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer,
		Flags: map[string]domain.FlagSpec{
			"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt, Group: "common"},
			"port":     {Long: "port", Type: domain.FlagTypeInt, Group: "common"},
			"temp":     {Long: "temp", Type: domain.FlagTypeFloat, Group: "sampling"},
		},
		Presentation: nil, // no persisted presentation
	}
	d := Draft{Args: map[string]string{}}
	vm := BuildViewModel(d, schema, nil)

	if len(vm.Groups) <= 1 {
		t.Fatalf("expected more than one group (curated + other), got %d: %+v", len(vm.Groups), vm.Groups)
	}
	first := vm.Groups[0]
	if first.Name == "Flags" {
		t.Fatal("flat 'Flags' fallback must not be used; expect curated groups")
	}
	if !first.Highlighted {
		t.Fatalf("first group must be highlighted essentials, got %+v", first)
	}
	// ctx-size and port are llama essentials — must appear in first group.
	hasCtxSize := false
	hasPort := false
	for _, f := range first.Fields {
		if f.Flag == "ctx-size" {
			hasCtxSize = true
		}
		if f.Flag == "port" {
			hasPort = true
		}
	}
	if !hasCtxSize || !hasPort {
		t.Fatalf("essentials missing from first group: %+v", first.Fields)
	}
}

func TestBuildViewModel_OrdersGroupsAndWidgets(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer,
		Flags: map[string]domain.FlagSpec{
			"ctx-size":   {Long: "ctx-size", Type: domain.FlagTypeInt, Default: 8192},
			"flash-attn": {Long: "flash-attn", Type: domain.FlagTypeEnum, EnumValues: []string{"on", "off", "auto"}},
		},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{
			{Name: "Essentials", Highlighted: true, Flags: []string{"ctx-size", "flash-attn"}},
		}},
	}
	d := Draft{Args: map[string]string{"ctx-size": "4096"}}
	vm := BuildViewModel(d, schema, nil)
	if len(vm.Groups) != 1 || vm.Groups[0].Name != "Essentials" {
		t.Fatalf("groups wrong: %+v", vm.Groups)
	}
	f0 := vm.Groups[0].Fields[0]
	if f0.Flag != "ctx-size" || f0.Widget != "number" || f0.Value != "4096" {
		t.Fatalf("field 0 wrong: %+v", f0)
	}
	f1 := vm.Groups[0].Fields[1]
	if f1.Widget != "select" || len(f1.Options) != 3 {
		t.Fatalf("enum widget wrong: %+v", f1)
	}
}

func TestBuildViewModel_UntouchedFlagStaysUnconfigured(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer,
		Flags: map[string]domain.FlagSpec{
			"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt, Default: 8192},
		},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{
			{Name: "Essentials", Highlighted: true, Flags: []string{"ctx-size"}},
		}},
	}
	// No draft arg for ctx-size: it must stay empty (not configured), with the
	// schema default surfaced only as a placeholder hint.
	vm := BuildViewModel(Draft{Args: map[string]string{}}, schema, nil)
	f := vm.Groups[0].Fields[0]
	if f.Value != "" {
		t.Fatalf("untouched flag must have empty Value, got %q", f.Value)
	}
	if f.Default != "8192" {
		t.Fatalf("default hint should be 8192, got %q", f.Default)
	}
}
