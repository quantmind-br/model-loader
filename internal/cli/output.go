package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
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

// formatCandidates sorts labels, joins the first 10 with ", ", and appends
// ", …" when there are more than 10.
func formatCandidates(labels []string) string {
	cp := make([]string, len(labels))
	copy(cp, labels)
	sort.Strings(cp)
	const max = 10
	if len(cp) > max {
		return strings.Join(cp[:max], ", ") + ", …"
	}
	return strings.Join(cp, ", ")
}

// humanBytes renders a byte count in IEC-ish units (1024-based). Negative
// counts (unknown totals) render as "?".
func humanBytes(n int64) string {
	if n < 0 {
		return "?"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}
