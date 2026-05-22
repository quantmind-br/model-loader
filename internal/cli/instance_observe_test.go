package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/metricsstore"
)

func TestPrintLogsOnce_CopiesTail(t *testing.T) {
	m := &fakeManager{
		running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}},
		tail:    "line one\nline two\n",
	}
	var out strings.Builder
	if err := printLogsOnce(&out, m, "100"); err != nil {
		t.Fatalf("logs: %v", err)
	}
	if !strings.Contains(out.String(), "line one") || !strings.Contains(out.String(), "line two") {
		t.Fatalf("log copy missing: %q", out.String())
	}
}

func TestPrintMetricsOnce_ReadsLatest(t *testing.T) {
	dir := t.TempDir()
	metricsDir := filepath.Join(dir, "metrics")
	if err := metricsstore.Append(metricsDir, "alpha", metricsstore.Record{
		TS: time.Now().Unix(), TokensPerSec: 42.5, RPS: 1.2,
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	m := &fakeManager{running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}}}
	var out strings.Builder
	if err := printMetricsOnce(&out, m, metricsDir, "100", false); err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if !strings.Contains(out.String(), "42.5") {
		t.Fatalf("metrics missing tok/s: %q", out.String())
	}
}

func TestPrintMetricsOnce_JSON(t *testing.T) {
	dir := t.TempDir()
	metricsDir := filepath.Join(dir, "metrics")
	_ = metricsstore.Append(metricsDir, "alpha", metricsstore.Record{TS: time.Now().Unix(), TokensPerSec: 5})
	m := &fakeManager{running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}}}
	var out strings.Builder
	if err := printMetricsOnce(&out, m, metricsDir, "100", true); err != nil {
		t.Fatalf("metrics json: %v", err)
	}
	if !strings.Contains(out.String(), "tokens_per_sec") {
		t.Fatalf("expected JSON record field, got %q", out.String())
	}
}
