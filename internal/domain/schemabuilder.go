package domain

// FlagSpecRow mirrors the fields of FlagSpec for use in slice-based
// schema declarations.
type FlagSpecRow struct {
	Long       string
	Short      string
	Aliases    []string
	Type       FlagType
	EnumValues []string
	Default    any
	HelpText   string
	Group      string
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
			Default:    r.Default,
			HelpText:   r.HelpText,
			Group:      r.Group,
		}
	}
	return FlagSchema{Version: version, Flags: flags}
}
