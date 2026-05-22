package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// jsonOut is bound to the global persistent --json flag. Commands check it to
// choose machine-readable JSON over human tables.
var jsonOut bool

// emitJSON writes v as indented JSON followed by a newline.
func emitJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// printTable writes a left-aligned, space-padded table.
func printTable(w io.Writer, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	_ = tw.Flush()
}

// dashOr renders empty strings as "-" for table cells.
func dashOr(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// clip truncates s to max runes, appending an ellipsis when shortened.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}

// indent prefixes every line of s with pad; "(empty)" for the empty string.
func indent(s, pad string) string {
	if s == "" {
		return pad + "(empty)"
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}
