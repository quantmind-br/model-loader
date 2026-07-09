package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/service/benchmarkstore"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/service/proxysupervisor"
)

func init() {
	benchCmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Run, list, compare, and inspect profile benchmarks",
		Long: "Run, list, compare, and inspect profile benchmarks.\n\n" +
			"Runs are started and cancelled from `benchmark run`; the other\n" +
			"subcommands read the saved run store.",
		// A bare `benchmark` prints help (exit 0) — no hidden flag dispatch.
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	benchCmd.AddCommand(newBenchRunCmd())
	benchCmd.AddCommand(newBenchListCmd())
	benchCmd.AddCommand(newBenchCompareCmd())
	benchCmd.AddCommand(newBenchHistoryCmd())
	benchCmd.AddCommand(newBenchShowCmd())
	benchCmd.AddCommand(newBenchTranscriptCmd())
	benchCmd.AddCommand(newBenchExportCmd())
	benchCmd.AddCommand(newBenchDeleteCmd())
	benchCmd.AddCommand(newBenchWebCmd())
	rootCmd.AddCommand(benchCmd)
}

// newBenchRunCmd builds `benchmark run`: the headless run path. It reuses the
// proxy-supervisor + runner wiring verbatim and preserves the --min-solve gate
// (exit 2) and forced transcript persistence.
func newBenchRunCmd() *cobra.Command {
	var (
		profileID     string
		modeStr       string
		minSolve      float64
		tbTasks       []string
		tbNTasks      int
		limit         int
		sweapInstance []string
		sweapHarness  string
		sweapPatches  string
		deepTasks     []string
		deepNTasks    int
		deepTasksDir  string
		verbose       bool
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a benchmark against a profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
			store := benchmarkstore.New(filepath.Join(cfg.Paths.StateDir, "benchmark", "runs"))

			mode, ok := parseBenchMode(modeStr)
			if !ok {
				fmt.Fprintf(errw, "unknown mode %q (want %s)\n", modeStr, benchModeList())
				return &ExitError{Code: 1}
			}
			// --tb-task overrides the configured terminal-bench task list for this
			// run (handy for the one-task validation flow).
			if len(tbTasks) > 0 {
				cfg.Benchmark.TerminalBench.Tasks = tbTasks
			}
			if tbNTasks > 0 {
				cfg.Benchmark.TerminalBench.Tasks = nil
				cfg.Benchmark.TerminalBench.NTasks = tbNTasks
			}
			// --limit is the uniform reduced-run knob: it caps the in-process
			// dataset modes + llama-bench presets (benchmark.Config.Limit) and,
			// for the agentic terminal-bench/deep-swe modes, doubles as --n-tasks
			// when no mode-specific task flag was given (they have no shared
			// problem slice). swe-bench-pro keeps its instance filter.
			if limit > 0 {
				cfg.Benchmark.Limit = limit
				if len(tbTasks) == 0 && tbNTasks == 0 {
					cfg.Benchmark.TerminalBench.Tasks = nil
					cfg.Benchmark.TerminalBench.NTasks = limit
				}
				if len(deepTasks) == 0 && deepNTasks == 0 {
					cfg.Benchmark.DeepSWE.Tasks = nil
					cfg.Benchmark.DeepSWE.NTasks = limit
				}
			}
			// swe-bench-pro per-run overrides (handy for ad-hoc / smoke runs).
			if len(sweapInstance) > 0 {
				cfg.Benchmark.SweBenchPro.Instances = sweapInstance
			}
			if sweapHarness != "" {
				cfg.Benchmark.SweBenchPro.HarnessDir = sweapHarness
			}
			if sweapPatches != "" {
				cfg.Benchmark.SweBenchPro.PatchPath = sweapPatches
			}
			// deep-swe per-run overrides (handy for ad-hoc / smoke runs).
			if len(deepTasks) > 0 {
				cfg.Benchmark.DeepSWE.Tasks = deepTasks
			}
			if deepNTasks > 0 {
				cfg.Benchmark.DeepSWE.Tasks = nil
				cfg.Benchmark.DeepSWE.NTasks = deepNTasks
			}
			if deepTasksDir != "" {
				cfg.Benchmark.DeepSWE.TasksDir = deepTasksDir
			}

			svc, release, err := bootstrapWithLock(errw, logLevel)
			if err != nil {
				if errors.Is(err, errAnotherInstance) {
					fmt.Fprintln(errw, "another model-loader instance is running (TUI/serve) — close it first.")
				}
				return &ExitError{Code: 1}
			}
			defer release()

			env, err := buildBenchmarkEnvironment(svc, cfg, errw)
			if err != nil {
				return &ExitError{Code: 1}
			}

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			if err := env.supervisor.EnsureRunning(ctx); err != nil {
				fmt.Fprintf(errw, "start http proxy: %v\n", err)
				return &ExitError{Code: 1}
			}

			run, err := runBenchmark(ctx, env.runner, benchmark.RunConfig{
				ProfileID: profileID,
				Mode:      mode,
				// T7: persist partial progress during the run (flagged in
				// progress); the final save below overwrites it.
				Checkpoint: func(partial benchmark.Run) { _ = store.Save(partial) },
			}, errw, verbose)
			if err != nil {
				// Persist whatever completed before the failure/SIGINT so the
				// partial data shows up (flagged) in the TUI and `benchmark list`.
				if len(run.Problems) > 0 {
					if sErr := store.Save(run); sErr != nil {
						fmt.Fprintf(errw, "warning: could not save partial run: %v\n", sErr)
					} else {
						fmt.Fprintf(errw, "partial run saved: %s\n", run.ID)
					}
					// UIUX-028: show the partial summary (text or JSON) so a
					// cancelled/failed run still surfaces what completed, like
					// the success path. The run is already failing, so a render
					// error is only noted (inside renderRun), not escalated.
					_ = renderRun(out, errw, run, jsonOut)
				}
				fmt.Fprintf(errw, "run failed: %v\n", err)
				return &ExitError{Code: 1}
			}
			return persistAndRenderBenchmarkRun(out, errw, store, run, jsonOut, minSolve)
		},
	}
	cmd.Flags().StringVar(&profileID, "profile", "", "profile id to benchmark")
	cmd.Flags().StringVar(&modeStr, "mode", "judge", "scoring mode: "+benchModeList())
	cmd.Flags().StringArrayVar(&tbTasks, "tb-task", nil, "terminal-bench: task id or glob to run (repeatable; overrides config.benchmark.terminalbench.tasks); only used with --mode terminal-bench")
	cmd.Flags().IntVar(&tbNTasks, "tb-n-tasks", 0, "terminal-bench: cap number of tasks (tb --n-tasks); overrides config when >0; only used with --mode terminal-bench")
	cmd.Flags().IntVar(&limit, "limit", 0, "reduced run: cap items per mode (dataset problems + llama-bench presets; also terminal-bench/deep-swe --n-tasks when no mode-specific task flag); 0 → full set")
	cmd.Flags().StringArrayVar(&sweapInstance, "sweap-instance", nil, "swe-bench-pro: instance_id to evaluate (repeatable; overrides config.benchmark.swebenchpro.instances); only used with --mode swe-bench-pro")
	cmd.Flags().StringVar(&sweapHarness, "sweap-harness", "", "swe-bench-pro: path to a cloned SWE-bench_Pro-os harness (overrides config.benchmark.swebenchpro.harness_dir)")
	cmd.Flags().StringVar(&sweapPatches, "sweap-patches", "", "swe-bench-pro: patches JSON or preds dir to evaluate (overrides config.benchmark.swebenchpro.patch_path)")
	cmd.Flags().StringArrayVar(&deepTasks, "deepswe-task", nil, "deep-swe: task id or glob to run (repeatable; overrides config.benchmark.deepswe.tasks); only used with --mode deep-swe")
	cmd.Flags().IntVar(&deepNTasks, "deepswe-n-tasks", 0, "deep-swe: cap number of tasks (pier --n-tasks); overrides config when >0; only used with --mode deep-swe")
	cmd.Flags().StringVar(&deepTasksDir, "deepswe-tasks", "", "deep-swe: path to a cloned deep-swe tasks/ dir (overrides config.benchmark.deepswe.tasks_dir)")
	cmd.Flags().Float64Var(&minSolve, "min-solve", -1, "exit code 2 if solve rate < this (0..1); -1 disables")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "include harness/stream activity lines in the live output")
	_ = cmd.MarkFlagRequired("profile")
	return cmd
}

// openBenchStore loads config and returns a filesystem-backed run store for the
// read-only benchmark subcommands. Config errors are reported to the command's
// stderr; the caller maps the error to exit 1.
func openBenchStore(cmd *cobra.Command) (benchmarkstore.Store, config.AppConfig, error) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
		return nil, config.AppConfig{}, err
	}
	return benchmarkstore.New(filepath.Join(cfg.Paths.StateDir, "benchmark", "runs")), cfg, nil
}

// benchExit maps the legacy int return of the bench print helpers (0 ok, 1 err)
// onto the cobra error contract.
func benchExit(code int) error {
	if code == 0 {
		return nil
	}
	return &ExitError{Code: code}
}

func parseBenchMode(s string) (benchmark.Mode, bool) {
	// Legacy aliases kept for compatibility with older scripts.
	switch s {
	case "long-context":
		return benchmark.ModeLongContext, true
	case "llamabench", "throughput":
		return benchmark.ModeLlamaBench, true
	}
	for _, m := range benchmark.ModesInOrder() {
		if s == string(m) {
			return m, true
		}
	}
	return "", false
}

// benchModeList renders every registered mode as a "a|b|c" list, derived from
// benchmark.ModesInOrder() so CLI messages never drift from the registry (BR8).
func benchModeList() string {
	modes := benchmark.ModesInOrder()
	parts := make([]string, len(modes))
	for i, m := range modes {
		parts[i] = string(m)
	}
	return strings.Join(parts, "|")
}

// benchmarkEnv bundles the proxy supervisor and runner wired for a benchmark run.
type benchmarkEnv struct {
	supervisor *proxysupervisor.Supervisor
	runner     *benchmark.Runner
}

// buildBenchmarkEnvironment wires the proxy supervisor (the only client channel
// to backends: the benchmark swaps profiles in through it and addresses all
// inference at it) and the benchmark runner from config. A NewRunner failure is
// logged and reported to errw before returning the error.
func buildBenchmarkEnvironment(svc *app.Services, cfg config.AppConfig, errw io.Writer) (*benchmarkEnv, error) {
	supervisor := proxysupervisor.New(proxysupervisor.Config{
		StatePath: filepath.Join(svc.Cfg.Paths.StateDir, "proxy-state.json"),
		LogDir:    svc.Cfg.Paths.LogDir,
		Host:      svc.Cfg.Serve.Host,
		Port:      svc.Cfg.Serve.Port,
		Logger:    svc.Logger,
	})
	if err := supervisor.Reconcile(); err != nil {
		svc.Logger.Error("proxy_reconcile_failed", "err", err)
	}

	mon := monitor.New(monitor.Config{NvidiaSMIPath: "nvidia-smi"})
	bc := app.BenchmarkConfig(cfg)
	// The headless CLI always persists transcripts so `benchmark transcript
	// <run-id>` can replay a run later; the TUI shows them live and config-gates.
	bc.SaveTranscripts = true
	bc.Logger = svc.Logger
	runner, err := benchmark.NewRunner(svc.Store, mon, supervisor, bc)
	if err != nil {
		svc.Logger.Error("benchmark_engine_init_failed", "err", err)
		fmt.Fprintf(errw, "benchmark engine: %v\n", err)
		return nil, err
	}
	return &benchmarkEnv{supervisor: supervisor, runner: runner}, nil
}

// runBenchmark runs the benchmark while streaming a live view to errw (driven by
// the runner's authoritative feed), then returns the completed (possibly
// partial) run. The live renderer goroutine is fully drained before returning.
func runBenchmark(ctx context.Context, runner *benchmark.Runner, rc benchmark.RunConfig, errw io.Writer, verbose bool) (benchmark.Run, error) {
	progress := make(chan benchmark.Progress, 32)
	live := newBenchLive(errw, runner.Feed, verbose)
	drained := make(chan struct{})
	go func() {
		live.run(progress)
		close(drained)
	}()
	run, err := runner.Run(ctx, rc, progress)
	close(progress)
	<-drained
	return run, err
}

// persistAndRenderBenchmarkRun saves the completed run, prints the transcript
// hints, renders the result (text or JSON), and enforces the --min-solve gate
// (exit code 2 when the solve rate falls below the threshold).
func persistAndRenderBenchmarkRun(out, errw io.Writer, store benchmarkstore.Store, run benchmark.Run, asJSON bool, minSolve float64) error {
	if err := store.Save(run); err != nil {
		fmt.Fprintf(errw, "warning: could not save run: %v\n", err)
	}
	if len(run.Transcript) > 0 {
		fmt.Fprintf(errw, "transcript: %s\n", store.TranscriptPath(run.ID))
		fmt.Fprintf(errw, "inspect raw output: model-loader benchmark transcript %s\n", run.ID)
	}
	if err := renderRun(out, errw, run, asJSON); err != nil {
		return &ExitError{Code: 1}
	}
	if minSolve >= 0 {
		if errored := countErroredProblems(run.Problems); errored > 0 {
			fmt.Fprintf(errw, "note: %d/%d items errored and are excluded from the solve rate\n", errored, len(run.Problems))
		}
		if run.Aggregate.SolveRate < minSolve {
			fmt.Fprintf(errw, "FAIL: solve rate %.2f below --min-solve %.2f\n", run.Aggregate.SolveRate, minSolve)
			return &ExitError{Code: 2}
		}
	}
	return nil
}

// renderRun writes a run's final summary to out as JSON or the text block,
// shared by the success path and the partial (cancelled/failed) path so both
// surface the same result. A JSON encode failure is reported to errw and
// returned so the caller can exit nonzero. (UIUX-026, UIUX-028)
func renderRun(out, errw io.Writer, run benchmark.Run, asJSON bool) error {
	if asJSON {
		if err := emitJSON(out, run); err != nil {
			fmt.Fprintf(errw, "encode json: %v\n", err)
			return err
		}
		return nil
	}
	printRun(out, run)
	return nil
}

// countErroredProblems counts items that failed with a per-problem error
// (excluded from aggregate denominators — surfaced so a --min-solve pass over
// a thin scored subset is visible, BR2).
func countErroredProblems(problems []benchmark.ProblemResult) int {
	n := 0
	for _, p := range problems {
		if p.Err != "" {
			n++
		}
	}
	return n
}
