package ui

import "sort"

// FormatArgs converts a map of arguments to sorted --flag value lines.
// Keys are sorted alphabetically for deterministic output.
// Empty values produce "--key" without a trailing space.
func FormatArgs(args map[string]string) []string {
	if len(args) == 0 {
		return nil
	}

	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(args))
	for _, k := range keys {
		v := args[k]
		if v == "" {
			out = append(out, "--"+k)
		} else {
			out = append(out, "--"+k+" "+v)
		}
	}
	return out
}
