package components

import (
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

func TestFilterLine(t *testing.T) {
	theme.RebuildStyles()
	cases := []struct {
		name     string
		active   bool
		filter   string
		contains []string
		empty    bool
	}{
		{name: "inactive empty", active: false, filter: "", empty: true},
		// The cursor must appear as soon as filter mode is entered, even
		// before the first character is typed.
		{name: "active empty shows cursor", active: true, filter: "", contains: []string{"filter: ", "█"}},
		{name: "active with text", active: true, filter: "qwen", contains: []string{"filter: ", "qwen█"}},
		{name: "inactive applied filter quoted", active: false, filter: "qwen", contains: []string{`filter: "qwen"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FilterLine(tc.active, tc.filter)
			if tc.empty {
				if got != "" {
					t.Fatalf("FilterLine = %q, want empty", got)
				}
				return
			}
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("FilterLine = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}
