package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type slotsPoller struct {
	baseURL  string
	client   HTTPDoer
	interval time.Duration
	out      chan<- MonitorEvent
}

func newSlotsPoller(baseURL string, client HTTPDoer, interval time.Duration, out chan<- MonitorEvent) *slotsPoller {
	if client == nil {
		// A backend that accepts the TCP connection but never answers (/health
		// mid-model-load) must not wedge the poller for the subscription's
		// lifetime; 5s ≫ the 1s poll tick, so a slow backend degrades to
		// skipped ticks, never a hang (audit N-P3).
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &slotsPoller{baseURL: baseURL, client: client, interval: interval, out: out}
}

func (p *slotsPoller) run(ctx context.Context) {
	tick := time.NewTicker(p.interval)
	defer tick.Stop()
	// First poll immediately.
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

func (p *slotsPoller) pollOnce(ctx context.Context) {
	p.fetchHealth(ctx)
	p.fetchSlots(ctx)
}

func (p *slotsPoller) fetchHealth(ctx context.Context) {
	h := HealthStatus{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/health", nil)
	if err != nil || req == nil {
		h.OK = false
		h.Status = "unreachable"
		p.emit(MonitorEvent{Timestamp: time.Now(), Source: SourceHealth, Data: h})
		return
	}
	resp, err := p.client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil || resp == nil {
		h.OK = false
		h.Status = "unreachable"
	} else if resp.StatusCode == 200 {
		h.OK = true
		h.Status = "ok"
	} else {
		h.OK = false
		h.Status = resp.Status
	}
	p.emit(MonitorEvent{Timestamp: time.Now(), Source: SourceHealth, Data: h})
}

func (p *slotsPoller) fetchSlots(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/slots", nil)
	if err != nil || req == nil {
		return
	}
	resp, err := p.client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil || resp == nil {
		return
	}
	if resp.StatusCode != 200 {
		return
	}
	var slots []Slot
	if err := json.NewDecoder(resp.Body).Decode(&slots); err != nil {
		return
	}
	p.emit(MonitorEvent{Timestamp: time.Now(), Source: SourceSlots, Data: SlotSnapshot{Slots: slots}})
}

func (p *slotsPoller) emit(ev MonitorEvent) {
	select {
	case p.out <- ev:
	default:
		// drop on backpressure; UI will catch up next tick.
	}
}