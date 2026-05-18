package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

type ProfileRef struct {
	ID   string
	Name string
}

type InfoPanel struct {
	Filename       string
	Path           string
	SizeOnDisk     int64
	Mtime          time.Time
	ParameterCount string
	SizeLabel      string
	Quantization   string
	Architecture   string
	BlockCount     uint64
	UsedByProfiles []ProfileRef
}

func humanSize(n int64) string {
	const kb = 1024
	const mb = kb * 1024
	const gb = mb * 1024
	switch {
	case n >= gb:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.2f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.2f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func (p InfoPanel) Render(width int) string {
	var b strings.Builder
	b.WriteString(theme.Title.Render("Model Info") + "\n\n")
	b.WriteString("File: " + p.Filename + "\n")
	b.WriteString("Path: " + p.Path + "\n")
	b.WriteString("Size: " + humanSize(p.SizeOnDisk) + "\n")
	b.WriteString("Modified: " + p.Mtime.Format(time.RFC3339) + "\n\n")

	b.WriteString(theme.Subtitle.Render("Metadata") + "\n")
	if p.ParameterCount != "" {
		b.WriteString("Parameters: " + p.ParameterCount + "\n")
	}
	if p.SizeLabel != "" {
		b.WriteString("Size label: " + p.SizeLabel + "\n")
	}
	if p.Quantization != "" {
		b.WriteString("Quantization: " + p.Quantization + "\n")
	}
	if p.Architecture != "" {
		b.WriteString("Architecture: " + p.Architecture + "\n")
	}
	if p.BlockCount > 0 {
		b.WriteString(fmt.Sprintf("Block count: %d\n", p.BlockCount))
	}
	b.WriteString("\n")

	b.WriteString(theme.Subtitle.Render("Used by Profiles") + "\n")
	if len(p.UsedByProfiles) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, ref := range p.UsedByProfiles {
			b.WriteString("- " + ref.Name + " (" + ref.ID + ")\n")
		}
	}

	style := lipgloss.NewStyle().Width(width).Padding(1)
	return style.Render(b.String())
}
