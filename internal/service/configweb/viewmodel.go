package configweb

import (
	"strconv"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// ViewModel is the template data for the editor page.
type ViewModel struct {
	Draft  Draft
	Groups []GroupVM
}

type GroupVM struct {
	Name        string
	Highlighted bool
	Fields      []FieldVM
}

type FieldVM struct {
	Flag     string
	Label    string
	Help     string
	Widget   string // number | select | toggle | text
	Value    string
	Options  []string
	Required bool
	Min, Max *int
}

// BuildViewModel renders schema + presentation + draft into template data.
func BuildViewModel(d Draft, schema domain.BackendValidationSchema) ViewModel {
	vm := ViewModel{Draft: d}
	pres := schema.Presentation
	if pres == nil {
		// Defensive: a schema with no presentation renders a single group.
		var flags []string
		for long := range schema.Flags {
			flags = append(flags, long)
		}
		pres = &domain.Presentation{Groups: []domain.PresentationGroup{{Name: "Flags", Highlighted: true, Flags: flags}}}
	}
	for _, g := range pres.Groups {
		gvm := GroupVM{Name: g.Name, Highlighted: g.Highlighted}
		for _, long := range g.Flags {
			spec, ok := schema.Flags[long]
			if !ok {
				continue
			}
			gvm.Fields = append(gvm.Fields, fieldVM(long, spec, d))
		}
		vm.Groups = append(vm.Groups, gvm)
	}
	return vm
}

func fieldVM(long string, spec domain.FlagSpec, d Draft) FieldVM {
	f := FieldVM{
		Flag:     long,
		Label:    long,
		Help:     spec.HelpText,
		Required: spec.Required,
		Min:      spec.Min,
		Max:      spec.Max,
	}
	switch spec.Type {
	case domain.FlagTypeInt, domain.FlagTypeFloat:
		f.Widget = "number"
	case domain.FlagTypeBool:
		f.Widget = "toggle"
	case domain.FlagTypeEnum:
		f.Widget = "select"
		f.Options = spec.EnumValues
	default:
		f.Widget = "text"
	}
	if v, ok := d.Args[long]; ok {
		f.Value = v
	} else if spec.Default != nil {
		f.Value = defaultString(spec.Default)
	}
	return f
}

func defaultString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "on"
		}
		return "off"
	case int:
		return strconv.Itoa(x)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return ""
	}
}
