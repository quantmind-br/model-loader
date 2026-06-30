package benchmark

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParsePresets_DefaultWhenEmpty(t *testing.T) {
	got, err := parsePresets(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(defaultPresets) {
		t.Fatalf("want %d default presets, got %d", len(defaultPresets), len(got))
	}
	if got[0] != (tpPreset{FillPct: 5, GenTokens: 256}) {
		t.Errorf("first default preset = %+v", got[0])
	}
}

func TestParsePresets_Valid(t *testing.T) {
	got, err := parsePresets([]string{"50%/256", " 90% / 128 "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []tpPreset{{50, 256}, {90, 128}}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("preset %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParsePresets_Invalid(t *testing.T) {
	for _, bad := range []string{"512", "x/128", "512/0", "0/128", "512/y", "512abc/128", "512/128/2", "512/128abc", "512/128", "150%/64", "50%/0"} {
		if _, err := parsePresets([]string{bad}); err == nil {
			t.Errorf("expected error for %q, got nil", bad)
		}
	}
}

func TestDefaultPresets_Expanded(t *testing.T) {
	got, err := parsePresets(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []tpPreset{{5, 256}, {25, 256}, {50, 256}, {90, 128}}
	if len(got) != len(want) {
		t.Fatalf("got %d default presets, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("preset %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCountForMode_LlamaBench(t *testing.T) {
	r := &Runner{presets: []tpPreset{{FillPct: 50, GenTokens: 128}, {FillPct: 90, GenTokens: 256}}, problems: make([]Problem, 5)}
	if got := r.CountForMode(ModeLlamaBench); got != 2 {
		t.Errorf("CountForMode(llama-bench) = %d, want 2", got)
	}
	if got := r.CountForMode(ModeLongContext); got != 1 {
		t.Errorf("CountForMode(longctx) = %d, want 1", got)
	}
}

func TestTPSStats(t *testing.T) {
	mean, stddev, min, max := tpsStats([]float64{40, 50, 60})
	if mean != 50 {
		t.Errorf("mean = %v, want 50", mean)
	}
	if math.Abs(stddev-math.Sqrt(200.0/3.0)) > 1e-9 {
		t.Errorf("stddev = %v, want %v", stddev, math.Sqrt(200.0/3.0))
	}
	if min != 40 || max != 60 {
		t.Errorf("min/max = %v/%v, want 40/60", min, max)
	}
	if m, s, _, _ := tpsStats(nil); m != 0 || s != 0 {
		t.Errorf("empty samples should yield zeros")
	}
}

func TestRunLlamaBench_VarianceAndServerTimings(t *testing.T) {
	// Each rep reports a different predicted_per_second so the spread is real.
	speeds := []float64{40, 50, 60}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1)) - 1
		spd := speeds[n%len(speeds)]
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x y\"}}]}\n\n")
		fmt.Fprintf(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":2},"+
			"\"timings\":{\"prompt_per_second\":1000,\"predicted_per_second\":%g}}\n\n", spd)
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	r := &Runner{cfg: Config{MaxTokens: 64, Timeout: 5 * time.Second}, reps: 3, warmup: 0, runCtxTokens: 16}
	res, _ := r.runLlamaBench(context.Background(), srv.URL, "m", tpPreset{FillPct: 50, GenTokens: 2})
	if res.Err != "" {
		t.Fatalf("unexpected error: %s", res.Err)
	}
	if !res.Resolved {
		t.Fatalf("expected resolved, detail %q", res.Detail)
	}
	if res.TokensPerSecond != 50 {
		t.Errorf("mean tok/s = %v, want 50 (from server timings)", res.TokensPerSecond)
	}
	if res.TPSMin != 40 || res.TPSMax != 60 {
		t.Errorf("min/max = %v/%v, want 40/60", res.TPSMin, res.TPSMax)
	}
	if res.TPSStdDev <= 0 {
		t.Errorf("TPSStdDev = %v, want > 0", res.TPSStdDev)
	}
	if !strings.Contains(res.Detail, "server timings") {
		t.Errorf("Detail should note server timings: %q", res.Detail)
	}
	if !strings.Contains(res.Detail, "±") {
		t.Errorf("Detail should include spread: %q", res.Detail)
	}
	if !strings.Contains(res.Detail, "fill 50%") {
		t.Errorf("Detail should note fill level: %q", res.Detail)
	}
}
