package monitor

import (
	"context"
	"encoding/csv"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type gpuPoller struct {
	pid           int
	nvidiaSmiPath string
	interval      time.Duration
	out           chan<- MonitorEvent
}

func newGPUPoller(pid int, nvidiaSmi string, interval time.Duration, out chan<- MonitorEvent) *gpuPoller {
	return &gpuPoller{pid: pid, nvidiaSmiPath: nvidiaSmi, interval: interval, out: out}
}

func (p *gpuPoller) run(ctx context.Context) {
	tick := time.NewTicker(p.interval)
	defer tick.Stop()
	p.pollOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			p.pollOnce(ctx)
		}
	}
}

func (p *gpuPoller) pollOnce(ctx context.Context) {
	// gopsutil first (process-aware) — skipped when pid==0 (test mode).
	if p.pid > 0 {
		if stats, ok := p.tryGopsutil(ctx); ok {
			p.emit(stats)
			return
		}
	}
	if stats, ok := p.tryNvidiaSmi(ctx); ok {
		p.emit(stats)
	}
}

// tryGopsutil — placeholder; we keep gopsutil out of the test path because
// it can panic on unsupported drivers. Real implementation:
//
//	import "github.com/shirou/gopsutil/v3/process"
//	import "github.com/shirou/gopsutil/v3/host"
//
// On Linux without GPU vendor support, gopsutil will not expose VRAM, so
// in practice this almost always falls through to nvidia-smi.
func (p *gpuPoller) tryGopsutil(ctx context.Context) (GPUStats, bool) {
	return GPUStats{}, false
}

func (p *gpuPoller) tryNvidiaSmi(ctx context.Context) (GPUStats, bool) {
	if p.nvidiaSmiPath == "" {
		return GPUStats{}, false
	}
	cmd := exec.CommandContext(ctx, p.nvidiaSmiPath,
		"--query-gpu=index,uuid,memory.used,memory.total,utilization.gpu",
		"--format=csv,noheader,nounits")
	stdout, err := cmd.Output()
	if err != nil {
		return GPUStats{}, false
	}
	return parseGPUCSV(string(stdout))
}

func parseGPUCSV(raw string) (GPUStats, bool) {
	r := csv.NewReader(strings.NewReader(strings.TrimSpace(raw)))
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1
	result := GPUStats{Source: "nvidia-smi", MetricVersion: 2, Scope: "physical_devices"}
	seen := map[string]bool{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return GPUStats{}, false
		}
		// Three-column fixtures and older exporters remain readable.
		d := GPUDeviceStats{Index: len(result.Devices)}
		if len(rec) == 5 {
			d.Index, err = strconv.Atoi(strings.TrimSpace(rec[0]))
			d.UUID = strings.TrimSpace(rec[1])
			if err != nil || d.UUID == "" || seen[d.UUID] {
				return GPUStats{}, false
			}
			seen[d.UUID] = true
			rec = rec[2:]
		}
		if len(rec) != 3 {
			return GPUStats{}, false
		}
		d.VRAMUsedMB, err = strconv.ParseUint(strings.TrimSpace(rec[0]), 10, 64)
		if err != nil {
			return GPUStats{}, false
		}
		d.VRAMTotalMB, err = strconv.ParseUint(strings.TrimSpace(rec[1]), 10, 64)
		if err != nil {
			return GPUStats{}, false
		}
		d.Utilization, err = strconv.ParseFloat(strings.TrimSpace(rec[2]), 64)
		if err != nil || math.IsNaN(d.Utilization) || math.IsInf(d.Utilization, 0) || d.Utilization < 0 || d.Utilization > 100 {
			return GPUStats{}, false
		}
		result.Devices = append(result.Devices, d)
		result.VRAMUsedMB += d.VRAMUsedMB
		result.VRAMTotalMB += d.VRAMTotalMB
		result.Utilization += d.Utilization
	}
	if len(result.Devices) == 0 {
		return GPUStats{}, false
	}
	result.Utilization /= float64(len(result.Devices))
	return result, true
}

func (p *gpuPoller) emit(s GPUStats) {
	ev := MonitorEvent{Timestamp: time.Now(), Source: SourceGPU, Data: s, PID: p.pid}
	select {
	case p.out <- ev:
	default:
	}
}
