package pages

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

func listPaginationLine(l list.Model, noun string) string {
	n := len(l.Items())
	if n == 0 {
		return ""
	}
	return theme.Subtitle.Render(fmt.Sprintf("page %d/%d · %d %s",
		l.Paginator.Page+1, max(1, l.Paginator.TotalPages), n, noun))
}
