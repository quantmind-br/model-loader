package benchmark

import (
	"errors"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/monitor"
)

// fakeMonitor is a minimal monitor.Manager for sampler lifecycle tests. ch is
// the event channel handed to the sampler; cancel closes it (mirroring the real
// monitor, whose cancel drains goroutines and closes the channel).
type fakeMonitor struct {
	ch  chan monitor.MonitorEvent
	err error
}

func (f *fakeMonitor) Subscribe(pid, port int, logPath string) (<-chan monitor.MonitorEvent, func() error, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.ch, func() error { close(f.ch); return nil }, nil
}

func gpuEvt(vram uint64, util float64) monitor.MonitorEvent {
	return monitor.MonitorEvent{Source: monitor.SourceGPU, Data: monitor.GPUStats{VRAMUsedMB: vram, Utilization: util}}
}

// stop() must block until the reader goroutine has fully drained the channel,
// so peak/util reads taken after stop() reflect every event the monitor sent.
func TestGPUSampler_StopDrainsBeforeReturning(t *testing.T) {
	fm := &fakeMonitor{ch: make(chan monitor.MonitorEvent, 8)}
	r := &Runner{mon: fm}
	g := r.startGPUSampler(123, "http://127.0.0.1:4321", "/tmp/x.log")

	fm.ch <- gpuEvt(100, 10)
	fm.ch <- gpuEvt(300, 30)
	fm.ch <- gpuEvt(200, 20)

	g.stop() // cancels (closes ch) then waits for the reader to drain + exit

	if got := g.peakVRAM(); got != 300 {
		t.Errorf("peakVRAM after stop = %d, want 300 (stop must wait for full drain)", got)
	}
	if got := g.avgUtil(); got != 20 {
		t.Errorf("avgUtil after stop = %v, want 20", got)
	}
}

// On a Subscribe error no goroutine starts and no subscription leaks; stop()
// must return promptly rather than block on a done channel that never closes.
func TestGPUSampler_SubscribeErrorStopDoesNotBlock(t *testing.T) {
	r := &Runner{mon: &fakeMonitor{err: errors.New("subscribe failed")}}
	g := r.startGPUSampler(123, "http://127.0.0.1:4321", "/tmp/x.log")

	done := make(chan struct{})
	go func() { g.stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stop() blocked after a Subscribe error")
	}
	if g.peakVRAM() != 0 {
		t.Errorf("peakVRAM = %d, want 0 on subscribe error", g.peakVRAM())
	}
}
