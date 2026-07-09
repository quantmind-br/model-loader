package benchmark

// Ceilings are the per-metric upper bounds used to normalize scorecard,
// leaderboard, and compare bars. TPS is higher-is-better (a value fills toward
// TPS); TTFTms and VRAMMB are the full-scale denominators for their bars.
type Ceilings struct {
	TPS    float64 // tok/s
	TTFTms float64
	VRAMMB float64
}

// Fixed fallback ceilings, applied per-field when no complete peer run supplies a
// larger observed value. They match the historical hard-coded scorecard scales
// (100 tok/s, 2000 ms, 24 GiB reference rig).
const (
	defaultCeilingTPS    = 100
	defaultCeilingTTFTms = 2000
	defaultCeilingVRAMMB = 24 * 1024
)

// CeilingsFor derives data-relative ceilings from the COMPLETE runs of mode: each
// field is the max observed across those runs × 1.1, falling back per-field to the
// fixed default ONLY when no complete peer run recorded that metric. Runs with a
// non-empty Err (partial / in-progress / failed) are excluded so a broken run
// never skews the scale.
func CeilingsFor(runs []Run, mode Mode) Ceilings {
	c := Ceilings{TPS: defaultCeilingTPS, TTFTms: defaultCeilingTTFTms, VRAMMB: defaultCeilingVRAMMB}
	var tps, ttft, vram float64
	for _, r := range runs {
		if r.Mode != mode || r.Err != "" {
			continue
		}
		if v := r.Aggregate.AvgTokensPerSecond; v > tps {
			tps = v
		}
		if v := r.Aggregate.AvgTTFTms; v > ttft {
			ttft = v
		}
		if v := float64(r.Aggregate.PeakVRAMMB); v > vram {
			vram = v
		}
	}
	if tps > 0 {
		c.TPS = tps * 1.1
	}
	if ttft > 0 {
		c.TTFTms = ttft * 1.1
	}
	if vram > 0 {
		c.VRAMMB = vram * 1.1
	}
	return c
}
