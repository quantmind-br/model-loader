package domain

// FlagSpecRow mirrors the fields of FlagSpec for use in slice-based
// schema declarations.
type FlagSpecRow struct {
	Long       string
	Short      string
	Aliases    []string
	Type       FlagType
	EnumValues []string
	List       bool
	Keywords   []string
	Default    any
	HelpText   string
	Group      string
	Min        *int
	Max        *int
	FloatMin   *float64
	FloatMax   *float64
	IsPort     bool
}

// BuildFlagSchema converts a slice of FlagSpecRow into a FlagSchema.
func BuildFlagSchema(version string, rows []FlagSpecRow) FlagSchema {
	flags := make(map[string]FlagSpec, len(rows))
	for _, r := range rows {
		flags[r.Long] = FlagSpec{
			Long:       r.Long,
			Short:      r.Short,
			Aliases:    r.Aliases,
			Type:       r.Type,
			EnumValues: r.EnumValues,
			List:       r.List,
			Keywords:   r.Keywords,
			Default:    r.Default,
			HelpText:   r.HelpText,
			Group:      r.Group,
			Min:        r.Min,
			Max:        r.Max,
			FloatMin:   r.FloatMin,
			FloatMax:   r.FloatMax,
			IsPort:     r.IsPort,
		}
	}
	return FlagSchema{Version: version, Flags: flags}
}
