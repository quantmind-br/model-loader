package monitor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGPUPoller_FallsBackToNvidiaSmi(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "nvidia-smi")
	// Write fake nvidia-smi that always returns one CSV line.
	body := "#!/bin/sh\necho '4096, 8192, 42'\n"
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan MonitorEvent, 4)
	p := newGPUPoller(0 /* pid: gopsutil disabled in test */, stub, 50*time.Millisecond, out)
	go p.run(ctx)

	deadline := time.After(2 * time.Second)
	select {
	case ev := <-out:
		if ev.Source != SourceGPU {
			t.Fatalf("source = %v", ev.Source)
		}
		s := ev.Data.(GPUStats)
		if s.Source != "nvidia-smi" {
			t.Fatalf("source = %q, want nvidia-smi", s.Source)
		}
		if s.VRAMUsedMB != 4096 || s.VRAMTotalMB != 8192 || s.Utilization != 42 {
			t.Fatalf("stats = %+v", s)
		}
	case <-deadline:
		t.Fatal("timeout")
	}
}

func TestGPUPoller_MissingNvidiaSmiSilent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	out := make(chan MonitorEvent, 4)
	p := newGPUPoller(0, "/nonexistent/nvidia-smi", 50*time.Millisecond, out)
	go p.run(ctx)

	for {
		select {
		case ev := <-out:
			t.Fatalf("expected no event, got %+v", ev)
		case <-ctx.Done():
			return // happy path: nothing emitted
		}
	}
}

func TestGPUPoller_MalformedNvidiaSmiSilent(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "nvidia-smi")
	body := "#!/bin/sh\necho 'N/A, N/A, N/A'\n"
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	out := make(chan MonitorEvent, 4)
	p := newGPUPoller(0, stub, 50*time.Millisecond, out)
	go p.run(ctx)

	for {
		select {
		case ev := <-out:
			t.Fatalf("expected no event on malformed CSV, got %+v", ev)
		case <-ctx.Done():
			return
		}
	}
}

func TestGPUCSVAllPhysicalDevices(t *testing.T) {
	s, ok := parseGPUCSV("1, GPU-b, 23087, 24576, 90\n0, GPU-a, 875, 24576, 10\n")
	if !ok || len(s.Devices) != 2 || s.Devices[0].UUID != "GPU-b" || s.VRAMUsedMB != 23962 || s.Utilization != 50 || s.MetricVersion != 2 {
		t.Fatalf("multi-device snapshot: %+v, ok=%v", s, ok)
	}
	for _, raw := range []string{"0, GPU-a, 1, 2, NaN", "0, GPU-a, 1, 2, 0\n1, GPU-a, 1, 2, 0", "0, GPU-a, N/A, 2, 0"} {
		if _, ok := parseGPUCSV(raw); ok {
			t.Fatalf("accepted invalid snapshot %q", raw)
		}
	}
}
