package configweb

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestBuildViewModel_OrdersGroupsAndWidgets(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer,
		Flags: map[string]domain.FlagSpec{
			"ctx-size":   {Long: "ctx-size", Type: domain.FlagTypeInt, Default: 8192},
			"flash-attn": {Long: "flash-attn", Type: domain.FlagTypeEnum, EnumValues: []string{"on", "off", "auto"}},
		},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{
			{Name: "Essenciais", Highlighted: true, Flags: []string{"ctx-size", "flash-attn"}},
		}},
	}
	d := Draft{Args: map[string]string{"ctx-size": "4096"}}
	vm := BuildViewModel(d, schema)
	if len(vm.Groups) != 1 || vm.Groups[0].Name != "Essenciais" {
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
