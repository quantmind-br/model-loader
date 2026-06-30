package app

import (
	"time"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

// BenchmarkConfig maps the persisted config.AppConfig benchmark block onto the
// benchmark.Config the runner consumes. It is the single source of truth for
// this mapping: both the TUI entrypoint (cmd/model-loader) and the CLI
// (internal/cli) MUST build the runner through it so a newly added field is
// wired into every surface at once. Duplicating this literal once cost the
// DeepSWE block its CLI wiring — never reintroduce a second copy.
func BenchmarkConfig(cfg config.AppConfig) benchmark.Config {
	b := cfg.Benchmark
	return benchmark.Config{
		MaxTokens:         b.MaxTokens,
		Limit:             b.Limit,
		Temperature:       b.Temperature,
		Timeout:           time.Duration(b.TimeoutSec) * time.Second,
		LongContextTokens: b.LongContextTokens,
		SaveTranscripts:   b.SaveTranscripts,
		Judge: benchmark.JudgeEndpoint{
			BaseURL: b.Judge.BaseURL,
			APIKey:  b.Judge.APIKey,
			Model:   b.Judge.Model,
			Samples: b.Judge.Samples,
		},
		LlamaBenchPresets: b.LlamaBench.Presets,
		LlamaBenchReps:    b.LlamaBench.Repetitions,
		LlamaBenchWarmup:  b.LlamaBench.Warmup,
		EmbeddingsBaseURL: b.Embeddings.BaseURL,

		TerminalBenchCmd:        b.TerminalBench.Command,
		TerminalBenchAgent:      b.TerminalBench.Agent,
		TerminalBenchDataset:    b.TerminalBench.Dataset,
		TerminalBenchProvider:   b.TerminalBench.Provider,
		TerminalBenchTasks:      b.TerminalBench.Tasks,
		TerminalBenchNTasks:     b.TerminalBench.NTasks,
		TerminalBenchConcurrent: b.TerminalBench.Concurrent,
		TerminalBenchTimeout:    time.Duration(b.TerminalBench.TimeoutSec) * time.Second,
		TerminalBenchExtraArgs:  b.TerminalBench.ExtraArgs,

		SweBenchProHarnessDir:    b.SweBenchPro.HarnessDir,
		SweBenchProRawSample:     b.SweBenchPro.RawSamplePath,
		SweBenchProScriptsDir:    b.SweBenchPro.ScriptsDir,
		SweBenchProDockerhubUser: b.SweBenchPro.DockerhubUser,
		SweBenchProPython:        b.SweBenchPro.Python,
		SweBenchProNumWorkers:    b.SweBenchPro.NumWorkers,
		SweBenchProUseModal:      b.SweBenchPro.UseModal,
		SweBenchProInstances:     b.SweBenchPro.Instances,
		SweBenchProPatchPath:     b.SweBenchPro.PatchPath,
		SweBenchProAgentCmd:      b.SweBenchPro.AgentCmd,
		SweBenchProTimeout:       time.Duration(b.SweBenchPro.TimeoutSec) * time.Second,
		SweBenchProExtraArgs:     b.SweBenchPro.ExtraArgs,

		DeepSWECmd:        b.DeepSWE.Command,
		DeepSWETasksDir:   b.DeepSWE.TasksDir,
		DeepSWEAgent:      b.DeepSWE.Agent,
		DeepSWEProvider:   b.DeepSWE.Provider,
		DeepSWEModelClass: b.DeepSWE.ModelClass,
		DeepSWEAPIBase:    b.DeepSWE.APIBase,
		DeepSWETasks:      b.DeepSWE.Tasks,
		DeepSWENTasks:     b.DeepSWE.NTasks,
		DeepSWESampleSeed: b.DeepSWE.SampleSeed,
		DeepSWEConcurrent: b.DeepSWE.Concurrent,
		DeepSWETimeout:    time.Duration(b.DeepSWE.TimeoutSec) * time.Second,
		DeepSWEExtraArgs:  b.DeepSWE.ExtraArgs,
	}
}
