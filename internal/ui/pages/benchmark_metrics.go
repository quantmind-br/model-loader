package pages

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

// benchMetric is the primary comparison metric for a run, normalized for bars.
type benchMetric struct {
	Label  string  // "solve", "recall", "tok/s"
	Frac   float64 // 0..1 for bar fill (throughput needs external scaling -> 0)
	Raw    float64 // raw value (tok/s etc.) for series-max normalization
	Text   string  // pre-formatted cell ("50%", "42.0")
	Higher bool    // true = higher is better
	Rate   bool    // true = Raw is already normalized to [0,1] (rate modes)
}

// primaryMetric picks the headline metric per mode. Quality/knowledge modes
// trend on a 0..1 rate; the throughput probe trends on tok/s (Raw, scaled by
// the caller against the series max); longctx uses recall (AvgScore).
func primaryMetric(r benchmark.Run) benchMetric {
	a := r.Aggregate
	switch r.Mode {
	case benchmark.ModeLlamaBench:
		return benchMetric{Label: "tok/s", Raw: a.AvgTokensPerSecond,
			Text: fmt.Sprintf("%.1f", a.AvgTokensPerSecond), Higher: true}
	case benchmark.ModeLongContext:
		return benchMetric{Label: "recall", Frac: a.AvgScore, Raw: a.AvgScore,
			Text: fmt.Sprintf("%.0f%%", a.AvgScore*100), Higher: true, Rate: true}
	default:
		return benchMetric{Label: "solve", Frac: a.SolveRate, Raw: a.SolveRate,
			Text: fmt.Sprintf("%.0f%%", a.SolveRate*100), Higher: true, Rate: true}
	}
}
