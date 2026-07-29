package domain

// FlagType enumerates the supported llama-server flag value types.
type FlagType int

const (
	FlagTypeBool FlagType = iota
	FlagTypeInt
	FlagTypeFloat
	FlagTypeString
	FlagTypeEnum
)

// FlagSpec describes a single llama-server flag.
type FlagSpec struct {
	Long       string   // canonical long name (without leading --), e.g. "ctx-size"
	Short      string   // first short alias (without leading -), e.g. "c"; "" if absent
	Aliases    []string // additional long aliases (without leading --)
	Type       FlagType
	EnumValues []string
	List       bool `json:"list,omitempty"` // true => EnumValues is a comma-separated list (e.g. --spec-type); validator splits on "," and checks each element
	// Keywords lists non-numeric literals a numeric flag also accepts, e.g.
	// --n-gpu-layers takes an exact integer, "auto", or "all". When a value is a
	// string equal to one of these, the validator skips the type and range
	// checks for that flag. Empty for every flag that is purely numeric.
	Keywords []string `json:"keywords,omitempty"`
	// Arity is the number of whitespace-separated argv tokens the flag's value
	// occupies, e.g. 2 for `--control-vector-layer-range START END`. Zero and
	// one both mean the ordinary single-token form. It is per-backend, not per
	// flag name: ik-llama.cpp parses `--lora-scaled FNAME S` as two tokens
	// while llama-server takes one `FNAME:SCALE` token. Profile.Args values are
	// emitted as a single token, so a flag with Arity > 1 is rejected there and
	// must go through Profile.ExtraArgs, which is passed through verbatim
	// (BUGS.md S15).
	Arity int `json:"arity,omitempty"`
	Default    any
	HelpText   string
	Group      string // "common" | "sampling" | "example-specific" | "embedded"

	Min      *int     `json:"min,omitempty"`
	Max      *int     `json:"max,omitempty"`
	FloatMin *float64 `json:"floatMin,omitempty"`
	FloatMax *float64 `json:"floatMax,omitempty"`
	IsPort   bool     `json:"isPort,omitempty"`
	Required bool     `json:"required,omitempty"`
}

// FlagSchema is the parsed --help output keyed by long name.
type FlagSchema struct {
	Version     string
	BackendKind BackendKind
	Flags       map[string]FlagSpec
	Rules       []CrossFieldRule
}

// Lookup resolves a name (map key, long form, short form, or alias) to a FlagSpec.
// Returns the spec and true on hit.
func (s FlagSchema) Lookup(name string) (FlagSpec, bool) {
	if spec, ok := s.Flags[name]; ok {
		return spec, true
	}
	for _, spec := range s.Flags {
		if spec.Long == name {
			return spec, true
		}
		if spec.Short == name {
			return spec, true
		}
		for _, alias := range spec.Aliases {
			if alias == name {
				return spec, true
			}
		}
	}
	return FlagSpec{}, false
}
