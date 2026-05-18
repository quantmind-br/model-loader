package components

import "github.com/quantmind-br/model-loader/internal/ui/theme"

func EmptyState(title, action string) string {
	head := theme.Subtitle.Render(title + ".")
	if action == "" {
		return head
	}
	return head + "\n" + theme.Subtitle.Render(action)
}
