package components

import (
	"regexp"
	"strings"
	"testing"
)

func TestHelpMarkdownCoversAllHints(t *testing.T) {
	pages := []struct {
		name  string
		hints string
	}{
		{"Profiles", "[n] new  [d] duplicate  [x] delete  [enter] edit  [L] launch  [e] export  [p] pin  [I] import  [u] undo  [/] filter"},
		{"Models", "[/] filter  [R] rescan  [s] search HF  [enter] actions  [i] info  [→/g] sizing"},
		{"Launcher", "[b] toggle bg/fg  [enter] launch  [k] kill  [r] refresh"},
		{"Server", "[v] cycle views  [Space] pause  [k] kill  [r] restart  [H] history  [s] start proxy  [x] stop proxy"},
		{"Backends", "[enter/e] edit  [n] new  [x] del  [D] default  [R] refresh  [P] probe  [/] filter"},
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
}
