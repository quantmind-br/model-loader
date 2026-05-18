package components

import (
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

func NewLoadingSpinner() spinner.Model {
	return spinner.New(spinner.WithSpinner(spinner.Dot))
}

func LoadingLine(spin spinner.Model, label string, found int) string {
	text := label
	if found > 0 {
		text = fmt.Sprintf("%s — %d found", label, found)
	}
	return theme.Subtitle.Render(spin.View() + " " + text)
}
