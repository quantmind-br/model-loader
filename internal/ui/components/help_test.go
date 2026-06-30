package components

import (
	"regexp"
	"strings"
	"testing"
)

func TestHelpMarkdownCoversAllHints(t *testing.T) {
	// No page implements HelpContextProvider yet, so each page's Hints()
	// string (default state, mirrored here) is the contract HelpMarkdown
	// must cover.
	pages := []struct {
		name  string
		hints string
	}{
		{"Profiles", "[enter] launch  [e] edit  [n] new  [d] duplicate  [X] delete  [K] unload  [R] refresh  [E] export  [p] pin  [I] import  [u] undo  [/] filter"},
		{"Models", "[/] filter  [R] rescan  [s] search HF  [enter] actions  [i] info  [→/g] sizing"},
		{"Server", "[v] cycle views  [Space] pause  [K] kill  [R] restart  [h] history  [s] start proxy  [x] stop proxy"},
		{"Backends", "[enter/e] edit  [n] new  [X] del  [D] default  [R] refresh  [P] probe  [/] filter"},
		{"Benchmark", "[b] run  [←→] mode  [enter] details  [c] compare  [E] export  [X] del  [R] reload  [h] history"},
	}
	re := regexp.MustCompile(`\[([^\]]+)\]`)
	for _, pg := range pages {
		for _, m := range re.FindAllStringSubmatch(pg.hints, -1) {
			tok := m[1]
			if strings.Contains(tok, " ") || strings.Contains(tok, "/") {
				continue
			}
			needle := "`" + tok + "`"
			if !strings.Contains(HelpMarkdown, needle) {
				t.Errorf("HelpMarkdown missing key [%s] from %s hints", tok, pg.name)
			}
		}
	}
	// g/G jump-to-top/bottom works in every list (help viewport, benchmark
	// run list, benchmark profile picker) and is documented globally.
	for _, needle := range []string{"`g`", "`G`"} {
		if !strings.Contains(HelpMarkdown, needle) {
			t.Errorf("HelpMarkdown missing global %s jump key", needle)
		}
	}
}
