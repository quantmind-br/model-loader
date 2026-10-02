package benchmark

import (
	"net/url"
	"strconv"
	"sync"

	"github.com/quantmind-br/model-loader/internal/service/monitor"
)

// gpuSampler accumulates peak VRAM and average GPU utilization from the monitor
// in the background for the duration of a benchmark run.
type gpuSampler struct {
	mu      sync.Mutex
	peaks   map[string]uint64
	version int
	peak    uint64
	utilSum float64
	utilN   int
	stopFn  func() error
	done    chan struct{} // closed when the reader goroutine exits (always non-nil)
}

func (g *gpuSampler) peakVRAM() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.peak
}

func (g *gpuSampler) avgUtil() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.utilN == 0 {
		return 0
	}
	return g.utilSum / float64(g.utilN)
}

// stop cancels the subscription and blocks until the reader goroutine has
// drained the channel and exited, so peakVRAM/avgUtil reads after stop see the
// final accumulated values and nothing reads a closed subscription channel.
func (g *gpuSampler) stop() {
	if g.stopFn != nil {
		_ = g.stopFn()
	}
	<-g.done
}

// startGPUSampler subscribes to the monitor and accumulates GPU stats in the
// background. Failures are non-fatal: the sampler simply records nothing.
// base is the proxy base URL; its port feeds the monitor's HTTP pollers, whose
// per-instance /slots (and metrics-ish) probes go through the proxy catch-all,
// which forwards them to the loaded backend. The backend port stays hidden.
func (r *Runner) startGPUSampler(pid int, base, logPath string) *gpuSampler {
	g := &gpuSampler{done: make(chan struct{})}
	ch, cancel, err := r.mon.Subscribe(pid, portFromBase(base), logPath)
	if err != nil {
		// No subscription was created — close done so stop() doesn't block.
		close(g.done)
		return g
	}
	g.stopFn = cancel
	go func() {
		defer close(g.done)
		for evt := range ch {
			if evt.Source != monitor.SourceGPU {
				continue
			}
			stats, ok := evt.Data.(monitor.GPUStats)
			if !ok {
				continue
			}
			g.mu.Lock()
			if stats.MetricVersion > g.version {
				g.version = stats.MetricVersion
			}
			if g.peaks == nil {
				g.peaks = map[string]uint64{}
			}
			for _, d := range stats.Devices {
				key := d.UUID
				if key == "" {
					key = "index:" + strconv.Itoa(d.Index)
				}
				if d.VRAMUsedMB > g.peaks[key] {
					g.peaks[key] = d.VRAMUsedMB
				}
			}
			if stats.VRAMUsedMB > g.peak {
				g.peak = stats.VRAMUsedMB
			}
			g.utilSum += stats.Utilization
			g.utilN++
			g.mu.Unlock()
		}
	}()
	return g
}

// portFromBase extracts the TCP port from a base URL like
// "http://127.0.0.1:4321" for the monitor's port-oriented Subscribe API.
// Returns 0 (HTTP pollers no-op) when the URL has no parseable port.
func portFromBase(base string) int {
	u, err := url.Parse(base)
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return 0
	}
	return port
}

func (g *gpuSampler) annotate(a *Aggregate) {
	g.mu.Lock()
	defer g.mu.Unlock()
	a.GPUMetricVersion = g.version
	if len(g.peaks) > 0 {
		a.GPUPeakVRAMMB = make(map[string]uint64, len(g.peaks))
		for id, value := range g.peaks {
			a.GPUPeakVRAMMB[id] = value
		}
	}
}
