package app

import (
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/config"
)

// TestBenchmarkConfig_MapsDeepSWEBlock guards the mapping that bit us once: the
// DeepSWE block was wired into the TUI runner but not the CLI runner because
// each had its own benchmark.Config literal. Both now route through
// BenchmarkConfig, and this test fails if any DeepSWE field stops propagating.
func TestBenchmarkConfig_MapsDeepSWEBlock(t *testing.T) {
	cfg := config.AppConfig{}
	cfg.Benchmark.DeepSWE = config.DeepSWEConfig{
		Command:    "/opt/pier",
		TasksDir:   "/corpus/deep-swe/tasks",
		Agent:      "mini-swe-agent",
		Provider:   "hosted_vllm",
		ModelClass: "litellm",
		APIBase:    "http://host.docker.internal:4321/v1",
		Tasks:      []string{"abs-module-cache-flags", "foo-*"},
		NTasks:     7,
		SampleSeed: 42,
		Concurrent: 3,
		TimeoutSec: 1500,
		ExtraArgs:  []string{"--force-build", "--ae", "HTTP_PROXY=x"},
	}

	bc := BenchmarkConfig(cfg)

	if bc.DeepSWECmd != "/opt/pier" {
		t.Errorf("DeepSWECmd = %q, want /opt/pier", bc.DeepSWECmd)
	}
	if bc.DeepSWETasksDir != "/corpus/deep-swe/tasks" {
		t.Errorf("DeepSWETasksDir = %q, want /corpus/deep-swe/tasks", bc.DeepSWETasksDir)
	}
	if bc.DeepSWEAgent != "mini-swe-agent" {
		t.Errorf("DeepSWEAgent = %q", bc.DeepSWEAgent)
	}
	if bc.DeepSWEProvider != "hosted_vllm" {
		t.Errorf("DeepSWEProvider = %q", bc.DeepSWEProvider)
	}
	if bc.DeepSWEModelClass != "litellm" {
		t.Errorf("DeepSWEModelClass = %q", bc.DeepSWEModelClass)
	}
	if bc.DeepSWEAPIBase != "http://host.docker.internal:4321/v1" {
		t.Errorf("DeepSWEAPIBase = %q", bc.DeepSWEAPIBase)
	}
	if len(bc.DeepSWETasks) != 2 || bc.DeepSWETasks[0] != "abs-module-cache-flags" || bc.DeepSWETasks[1] != "foo-*" {
		t.Errorf("DeepSWETasks = %v", bc.DeepSWETasks)
	}
	if bc.DeepSWENTasks != 7 {
		t.Errorf("DeepSWENTasks = %d, want 7", bc.DeepSWENTasks)
	}
	if bc.DeepSWESampleSeed != 42 {
		t.Errorf("DeepSWESampleSeed = %d, want 42", bc.DeepSWESampleSeed)
	}
	if bc.DeepSWEConcurrent != 3 {
		t.Errorf("DeepSWEConcurrent = %d, want 3", bc.DeepSWEConcurrent)
	}
	if bc.DeepSWETimeout != 1500*time.Second {
		t.Errorf("DeepSWETimeout = %v, want 1500s", bc.DeepSWETimeout)
	}
	if len(bc.DeepSWEExtraArgs) != 3 || bc.DeepSWEExtraArgs[0] != "--force-build" {
		t.Errorf("DeepSWEExtraArgs = %v", bc.DeepSWEExtraArgs)
	}
}

// TestBenchmarkConfig_MapsSecondsToDuration confirms the *Sec int config fields
// are converted to time.Duration (not left as raw nanosecond-int seconds).
func TestBenchmarkConfig_MapsSecondsToDuration(t *testing.T) {
	cfg := config.AppConfig{}
	cfg.Benchmark.TimeoutSec = 120
	cfg.Benchmark.TerminalBench.TimeoutSec = 600
	cfg.Benchmark.SweBenchPro.TimeoutSec = 900
	cfg.Benchmark.DeepSWE.TimeoutSec = 1500

	bc := BenchmarkConfig(cfg)

	for _, tc := range []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"Timeout", bc.Timeout, 120 * time.Second},
		{"TerminalBenchTimeout", bc.TerminalBenchTimeout, 600 * time.Second},
		{"SweBenchProTimeout", bc.SweBenchProTimeout, 900 * time.Second},
		{"DeepSWETimeout", bc.DeepSWETimeout, 1500 * time.Second},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// TestBenchmarkConfig_MapsJudgeAndScalars spot-checks fields outside the agentic
// blocks so a future refactor of the shared mapper can't silently drop them.
func TestBenchmarkConfig_MapsJudgeAndScalars(t *testing.T) {
	cfg := config.AppConfig{}
	cfg.Benchmark.MaxTokens = 32768
	cfg.Benchmark.Limit = 5
	cfg.Benchmark.Judge.BaseURL = "https://judge.example/v1"
	cfg.Benchmark.Judge.Model = "gpt-x"
	cfg.Benchmark.Judge.Samples = 3

	bc := BenchmarkConfig(cfg)

	if bc.MaxTokens != 32768 {
		t.Errorf("MaxTokens = %d, want 32768", bc.MaxTokens)
	}
	if bc.Limit != 5 {
		t.Errorf("Limit = %d, want 5", bc.Limit)
	}
	if bc.Judge.BaseURL != "https://judge.example/v1" || bc.Judge.Model != "gpt-x" || bc.Judge.Samples != 3 {
		t.Errorf("Judge = %+v", bc.Judge)
	}
}

// TestBenchmarkConfig_MapsStallTimeoutsAndUnload guards the agentic hang-watchdog
// wiring: the generalized watchdog (shared by terminal-bench and deep-swe) reads
// its no-progress kill threshold from these per-mode *StallTimeout fields, and
// UnloadAfterRun frees the model when the run ends. If any stops propagating the
// safety net silently reverts to the built-in default (or, for unload, leaves
// VRAM pinned). Not covered by MapsSecondsToDuration, which only checks *Timeout.
// UIUX-012.
func TestBenchmarkConfig_MapsStallTimeoutsAndUnload(t *testing.T) {
	cfg := config.AppConfig{}
	cfg.Benchmark.UnloadAfterRun = true
	cfg.Benchmark.TerminalBench.StallTimeoutSec = 2700
	cfg.Benchmark.DeepSWE.StallTimeoutSec = 1800

	bc := BenchmarkConfig(cfg)

	if !bc.UnloadAfterRun {
		t.Errorf("UnloadAfterRun = %v, want true", bc.UnloadAfterRun)
	}
	if bc.TerminalBenchStallTimeout != 2700*time.Second {
		t.Errorf("TerminalBenchStallTimeout = %v, want 2700s", bc.TerminalBenchStallTimeout)
	}
	if bc.DeepSWEStallTimeout != 1800*time.Second {
		t.Errorf("DeepSWEStallTimeout = %v, want 1800s", bc.DeepSWEStallTimeout)
	}
}
