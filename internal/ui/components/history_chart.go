package components

import (
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/service/metricsstore"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

type HistoryChart struct {
	records []metricsstore.Record
	window  time.Duration
	width   int
	height  int
}

func NewHistoryChart(records []metricsstore.Record, window time.Duration) HistoryChart {
	return HistoryChart{records: records, window: window}
}

func (h *HistoryChart) SetSize(width, height int) {
	h.width = width
	h.height = height
}

func (h HistoryChart) View() string {
	cutoff := time.Now().Add(-h.window).Unix()
	var vals []float64
	for _, r := range h.records {
		if r.TS >= cutoff {
			vals = append(vals, r.TokensPerSec)
		}
	}

	var body string
	if len(vals) == 0 {
		body = "No data for selected window"
	} else {
		body = Sparkline(vals, h.width-4)
	}

	footer := "[1] 1h  [2] 6h  [3] 24h  [4] 7d"
	return lipgloss.JoinVertical(lipgloss.Left, body, "", theme.Subtitle.Render(footer))
}
