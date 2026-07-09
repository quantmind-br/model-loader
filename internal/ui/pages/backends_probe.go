package pages

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

type backendProberIface interface {
	Probe(context.Context) (<-chan backendcatalog.ProbeEvent, error)
}

type backendProbeResult struct {
	status  backendcatalog.ProbeStatus
	detail  string
	latency time.Duration
}

type probeEventMsg struct {
	event backendcatalog.ProbeEvent
	epoch int
}

// WithProber wires a backend prober for health checks.
func (p BackendsPage) WithProber(prober backendProberIface) BackendsPage {
	p.prober = prober
	return p
}

func (p BackendsPage) askProbeAll() (tea.Model, tea.Cmd) {
	if p.prober == nil {
		p, fc := p.withFlashError("prober not available")
		return p, fc
	}
	p.pendingProbe = true
	p.probeStartTime = time.Now()
	p.probeEpoch++
	p.probeResults = make(map[string]backendProbeResult)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.prober.Probe(ctx)
	if err != nil {
		cancel()
		p.pendingProbe = false
		p, fc := p.withFlashError("probe failed: " + err.Error())
		return p, fc
	}
	p.probeCh = ch
	p.probeCancel = cancel
	// Arm the spinner tick only now that a probe is in flight (audit N-P5).
	return p, tea.Batch(p.readNextProbeEvent(p.probeEpoch), p.spinnerModel.Tick)
}

func (p BackendsPage) readNextProbeEvent(epoch int) tea.Cmd {
	return func() tea.Msg {
		if p.probeCh == nil {
			return probeEventMsg{event: backendcatalog.ProbeEvent{Done: true}, epoch: epoch}
		}
		ev, ok := <-p.probeCh
		if !ok {
			return probeEventMsg{event: backendcatalog.ProbeEvent{Done: true}, epoch: epoch}
		}
		return probeEventMsg{event: ev, epoch: epoch}
	}
}

func (p BackendsPage) handleProbeEvent(m probeEventMsg) (tea.Model, tea.Cmd) {
	if m.epoch != p.probeEpoch {
		return p, nil
	}
	if m.event.Done {
		p.probeCh = nil
		p.pendingProbe = false
		if p.probeCancel != nil {
			p.probeCancel() // producer finished; release the ctx (audit N-C14)
			p.probeCancel = nil
		}
		p, fc := p.withFlash("probe complete")
		return p, fc
	}
	p.probeResults[m.event.BackendID] = backendProbeResult{
		status:  m.event.Status,
		detail:  m.event.Detail,
		latency: m.event.Latency,
	}
	return p, p.readNextProbeEvent(p.probeEpoch)
}

func (p BackendsPage) handleSpinnerTick(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if p.pendingProbe && time.Since(p.probeStartTime) > 3*time.Second {
		p.pendingProbe = false
		p.probeCh = nil
		if p.probeCancel != nil {
			p.probeCancel() // stop the producer goroutine (audit N-C14)
			p.probeCancel = nil
		}
		p, fc := p.withFlashError("probe timed out")
		return p, fc
	}
	if !p.pendingProbe && !p.pendingRefresh {
		return p, nil // idle: don't re-arm the tick loop (audit N-P5)
	}
	updated, cmd := p.spinnerModel.Update(msg)
	p.spinnerModel = updated
	return p, cmd
}
