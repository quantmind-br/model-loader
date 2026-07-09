package backendcatalog

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/shellsplit"
)

// ProbeStatus is the health result of probing a single backend.
type ProbeStatus string

const (
	ProbeStatusOK   ProbeStatus = "OK"
	ProbeStatusWarn ProbeStatus = "WARN"
	ProbeStatusErr  ProbeStatus = "ERR"
)

// ProbeConfig controls per-backend probe behaviour.
type ProbeConfig struct {
	Timeout time.Duration
}

// ProbeEvent is emitted once per backend during a probe run, plus a final
// done event with Done == true.
type ProbeEvent struct {
	BackendID string
	Status    ProbeStatus
	Latency   time.Duration
	Detail    string
	Err       error
	Done      bool
}

// Prober runs health checks against every backend in the catalog.
type Prober struct {
	store Store
	cfg   ProbeConfig
}

// NewProber constructs a prober wired to a catalog store.
func NewProber(store Store, cfg ProbeConfig) *Prober {
	return &Prober{store: store, cfg: cfg}
}

// Probe runs health checks for all catalog backends and streams events on the
// returned channel. The caller must drain the channel. The final event has
// Done == true.
func (p *Prober) Probe(ctx context.Context) (<-chan ProbeEvent, error) {
	catalog, err := p.store.Load()
	if err != nil {
		return nil, fmt.Errorf("load catalog: %w", err)
	}

	ch := make(chan ProbeEvent)
	go func() {
		defer close(ch)
		for _, b := range catalog.Backends {
			event := p.probeOne(ctx, b)
			select {
			case ch <- event:
			case <-ctx.Done():
				return
			}
		}
		select {
		case ch <- ProbeEvent{Done: true}:
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

func (p *Prober) probeOne(ctx context.Context, backend domain.Backend) ProbeEvent {
	start := time.Now()

	resolved, err := resolveExecutable(backend)
	if err != nil {
		return ProbeEvent{
			BackendID: backend.ID,
			Status:    ProbeStatusErr,
			Latency:   time.Since(start),
			Detail:    "executable not found: " + err.Error(),
			Err:       err,
		}
	}

	probeCtx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()

	status, detail, _ := runProbe(probeCtx, resolved, "--version")
	if status == ProbeStatusOK {
		return ProbeEvent{
			BackendID: backend.ID,
			Status:    ProbeStatusOK,
			Latency:   time.Since(start),
			Detail:    detail,
		}
	}

	status, detail, err = runProbe(probeCtx, resolved, "--help")
	return ProbeEvent{
		BackendID: backend.ID,
		Status:    status,
		Latency:   time.Since(start),
		Detail:    detail,
		Err:       err,
	}
}

func runProbe(ctx context.Context, resolvedCmd string, flag string) (ProbeStatus, string, error) {
	fields, err := shellsplit.Split(resolvedCmd)
	if err != nil || len(fields) == 0 {
		if err == nil {
			err = fmt.Errorf("empty command")
		}
		return ProbeStatusErr, "", fmt.Errorf("parse command %q: %w", resolvedCmd, err)
	}
	args := append(fields[1:], flag)
	cmd := exec.CommandContext(ctx, fields[0], args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return ProbeStatusErr, "timeout", fmt.Errorf("probe timed out")
		}
		return ProbeStatusWarn, strings.TrimSpace(string(out)), err
	}
	return ProbeStatusOK, strings.TrimSpace(string(out)), nil
}
