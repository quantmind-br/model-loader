package benchmark

import (
	"regexp"
	"strings"
)

var codeFenceRe = regexp.MustCompile("(?s)```[A-Za-z0-9+]*[ \\t]*\\n(.*?)```")

// extractCode pulls the first fenced code block from a model reply; if there is
// no fence, the whole trimmed reply is treated as code.
func extractCode(response string) string {
	if m := codeFenceRe.FindStringSubmatch(response); m != nil {
		return strings.TrimRight(strings.TrimLeft(m[1], "\n"), "\n ")
	}
	return strings.TrimSpace(response)
}
