package configweb

import (
	"sort"
	"strconv"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
)

// ViewModel is the template data for the editor page.
type ViewModel struct {
	Draft     Draft
	Groups    []GroupVM
	BackendID string
	AllFlags  []FlagEditVM
	Rules     []domain.CrossFieldRule
	Backends  []domain.Backend
}

// FlagEditVM is the editable representation of one flag for customize mode.
type FlagEditVM struct {
	Flag     string
	Type     string // int|float|bool|enum|string
	Min      string
	Max      string
	Default  string
	Required bool
	Enum     string // comma-joined enum values
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
	Value    string // user-set value; empty means "not configured"
	Default  string // schema default, shown only as placeholder/hint
	Options  []string
	Required bool
	Min, Max *int
}

// BuildViewModel renders schema + presentation + draft into template data.
func BuildViewModel(d Draft, schema domain.BackendValidationSchema, backends []domain.Backend) ViewModel {
	vm := ViewModel{Draft: d}
	pres := schema.Presentation
	if pres == nil {
		// Defensive: schema with no persisted presentation — synthesize curated
		// groups on the fly so the UI is never a flat "Flags" dump.
		bp := backendschema.BuildPresentation(schema)
		pres = &bp
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
	vm.BackendID = schema.BackendID
	vm.Backends = backends
	vm.Rules = schema.Rules
	longs := make([]string, 0, len(schema.Flags))
	for long := range schema.Flags {
		longs = append(longs, long)
	}
	sort.Strings(longs)
	for _, long := range longs {
		vm.AllFlags = append(vm.AllFlags, flagEdit(long, schema.Flags[long]))
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
		Default:  defaultString(spec.Default),
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
	// Only a value the user explicitly set fills the field. An untouched flag
	// stays empty (= not configured) so it never leaks into the saved profile;
	// the schema default is surfaced only as a placeholder hint.
	if v, ok := d.Args[long]; ok {
		f.Value = v
	}
	return f
}

func flagEdit(long string, spec domain.FlagSpec) FlagEditVM {
	return FlagEditVM{
		Flag:     long,
		Type:     flagTypeString(spec.Type),
		Min:      iptrString(spec.Min),
		Max:      iptrString(spec.Max),
		Default:  defaultString(spec.Default),
		Required: spec.Required,
		Enum:     strings.Join(spec.EnumValues, ", "),
	}
}

func flagTypeString(t domain.FlagType) string {
	switch t {
	case domain.FlagTypeInt:
		return "int"
	case domain.FlagTypeFloat:
		return "float"
	case domain.FlagTypeBool:
		return "bool"
	case domain.FlagTypeEnum:
		return "enum"
	default:
		return "string"
	}
}

func iptrString(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
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
