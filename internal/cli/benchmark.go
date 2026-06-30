package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
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
	var (
		profileID     string
		modeStr       string
		list          bool
		compare       bool
		asJSON        bool
		minSolve      float64
		transcript    string
		tbTasks       []string
		tbNTasks      int
		limit         int
		sweapInstance []string
		sweapHarness  string
		sweapPatches  string
		deepTasks     []string
		deepNTasks    int
		deepTasksDir  string
	)

	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Run, list, or compare profile benchmarks",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			out, errw := cmd.OutOrStdout(), cmd.ErrOrStderr()
			store := benchmarkstore.New(filepath.Join(cfg.Paths.StateDir, "benchmark", "runs"))

			if transcript != "" {
				return benchExit(benchPrintTranscript(out, store, transcript, asJSON))
			}
			if list {
				return benchExit(benchPrintList(out, store, asJSON))
			}
			if compare && profileID == "" {
				return benchExit(benchPrintCompare(out, store, asJSON))
			}
			if profileID == "" {
				fmt.Fprintln(errw, "usage:")
				fmt.Fprintln(errw, "  model-loader benchmark --profile <id> [--mode judge|longctx|llama-bench|terminal-bench|swe-bench-pro|deep-swe] [--limit N] [--tb-task <id>] [--tb-n-tasks N] [--sweap-instance <id>] [--deepswe-task <id>] [--deepswe-n-tasks N] [--json] [--min-solve N]")
				fmt.Fprintln(errw, "  model-loader benchmark --list [--json]")
				fmt.Fprintln(errw, "  model-loader benchmark --compare [--profile <id>] [--json]")
				fmt.Fprintln(errw, "  model-loader benchmark --transcript <run-id> [--json]")
				return &ExitError{Code: 1}
			}
			if compare {
				return benchExit(benchPrintHistory(out, store, profileID, asJSON))
			}

			mode, ok := parseBenchMode(modeStr)
			if !ok {
				fmt.Fprintf(errw, "unknown mode %q (want judge|longctx|llama-bench)\n", modeStr)
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

			run, err := runBenchmark(ctx, env.runner, benchmark.RunConfig{ProfileID: profileID, Mode: mode}, errw)
			if err != nil {
				// Persist whatever completed before the failure/SIGINT so the
				// partial data shows up (flagged) in the TUI and --list.
				if len(run.Problems) > 0 {
					if sErr := store.Save(run); sErr != nil {
						fmt.Fprintf(errw, "warning: could not save partial run: %v\n", sErr)
					} else {
						fmt.Fprintf(errw, "partial run saved: %s\n", run.ID)
					}
				}
				fmt.Fprintf(errw, "run failed: %v\n", err)
				return &ExitError{Code: 1}
			}
			return persistAndRenderBenchmarkRun(out, errw, store, run, asJSON, minSolve)
		},
	}

	cmd.Flags().StringVar(&profileID, "profile", "", "profile id to benchmark")
	cmd.Flags().StringVar(&modeStr, "mode", "judge", "scoring mode: judge | math-bench | codegen-bench | ragas-bench | summary-bench | llama-bench | longctx | instruction-bench | mmlu-bench | terminal-bench | swe-bench-pro | deep-swe")
	cmd.Flags().StringArrayVar(&tbTasks, "tb-task", nil, "terminal-bench: task id or glob to run (repeatable; overrides config.benchmark.terminalbench.tasks); only used with --mode terminal-bench")
	cmd.Flags().IntVar(&tbNTasks, "tb-n-tasks", 0, "terminal-bench: cap number of tasks (tb --n-tasks); overrides config when >0; only used with --mode terminal-bench")
	cmd.Flags().IntVar(&limit, "limit", 0, "reduced run: cap items per mode (dataset problems + llama-bench presets; also terminal-bench/deep-swe --n-tasks when no mode-specific task flag); 0 → full set")
	cmd.Flags().StringArrayVar(&sweapInstance, "sweap-instance", nil, "swe-bench-pro: instance_id to evaluate (repeatable; overrides config.benchmark.swebenchpro.instances); only used with --mode swe-bench-pro")
	cmd.Flags().StringVar(&sweapHarness, "sweap-harness", "", "swe-bench-pro: path to a cloned SWE-bench_Pro-os harness (overrides config.benchmark.swebenchpro.harness_dir)")
	cmd.Flags().StringVar(&sweapPatches, "sweap-patches", "", "swe-bench-pro: patches JSON or preds dir to evaluate (overrides config.benchmark.swebenchpro.patch_path)")
	cmd.Flags().StringArrayVar(&deepTasks, "deepswe-task", nil, "deep-swe: task id or glob to run (repeatable; overrides config.benchmark.deepswe.tasks); only used with --mode deep-swe")
	cmd.Flags().IntVar(&deepNTasks, "deepswe-n-tasks", 0, "deep-swe: cap number of tasks (pier --n-tasks); overrides config when >0; only used with --mode deep-swe")
	cmd.Flags().StringVar(&deepTasksDir, "deepswe-tasks", "", "deep-swe: path to a cloned deep-swe tasks/ dir (overrides config.benchmark.deepswe.tasks_dir)")
	cmd.Flags().BoolVar(&list, "list", false, "list saved runs and exit")
	cmd.Flags().BoolVar(&compare, "compare", false, "with --profile: that profile's run history; alone: latest run per profile")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of text")
	cmd.Flags().Float64Var(&minSolve, "min-solve", -1, "exit code 2 if solve rate < this (0..1); -1 disables")
	cmd.Flags().StringVar(&transcript, "transcript", "", "print the raw transcript of a saved run id and exit")
	rootCmd.AddCommand(cmd)
}

// benchExit maps legacy int return of bench print helpers (0 ok, 1 err) onto cobra error contract.
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
	// The headless CLI always persists transcripts so `benchmark --transcript
	// <run-id>` can replay a run later; the TUI shows them live and config-gates.
	bc.SaveTranscripts = true
	runner, err := benchmark.NewRunner(svc.Store, mon, supervisor, bc)
	if err != nil {
		svc.Logger.Error("benchmark_engine_init_failed", "err", err)
		fmt.Fprintf(errw, "benchmark engine: %v\n", err)
		return nil, err
	}
	return &benchmarkEnv{supervisor: supervisor, runner: runner}, nil
}

// runBenchmark runs the benchmark while streaming progress lines to errw, then
// returns the completed (possibly partial) run. The progress goroutine is fully
// drained before returning.
func runBenchmark(ctx context.Context, runner *benchmark.Runner, rc benchmark.RunConfig, errw io.Writer) (benchmark.Run, error) {
	progress := make(chan benchmark.Progress, 32)
	drained := make(chan struct{})
	go func() {
		runBenchmarkProgressListener(progress, errw)
		close(drained)
	}()
	run, err := runner.Run(ctx, rc, progress)
	close(progress)
	<-drained
	return run, err
}

// runBenchmarkProgressListener drains progress events, rendering launch/infer/
// score phases to errw until the channel closes.
func runBenchmarkProgressListener(progress <-chan benchmark.Progress, errw io.Writer) {
	for p := range progress {
		switch p.Phase {
		case "launch":
			fmt.Fprintln(errw, "loading profile via proxy — waiting for backend health (large models can take minutes)…")
		case "infer", "score":
			fmt.Fprintln(errw, benchmark.FormatBenchProgress(p.Index, p.Total, p.ProblemName, p.Phase))
		}
	}
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
		fmt.Fprintf(errw, "inspect raw output: model-loader benchmark --transcript %s\n", run.ID)
	}
	if asJSON {
		_ = emitJSON(out, run)
	} else {
		printRun(out, run)
	}
	if minSolve >= 0 && run.Aggregate.SolveRate < minSolve {
		fmt.Fprintf(errw, "FAIL: solve rate %.2f below --min-solve %.2f\n", run.Aggregate.SolveRate, minSolve)
		return &ExitError{Code: 2}
	}
	return nil
}

func printRun(out io.Writer, run benchmark.Run) {
	a := run.Aggregate
	fmt.Fprintf(out, "Profile: %s (%s)\n", run.ProfileName, run.ProfileID)
	fmt.Fprintf(out, "Mode:    %s\n", run.Mode.Title())
	fmt.Fprintf(out, "Model:   %s   quant=%s  cache k/v=%s/%s  ctx=%d\n",
		run.Profile.Model, dashOr(run.Profile.Quantization), dashOr(run.Profile.CacheTypeK), dashOr(run.Profile.CacheTypeV), run.Profile.CtxSize)
	if run.Err != "" {
		fmt.Fprintf(out, "Error:   %s (partial run)\n", run.Err)
	}
	solve := fmt.Sprintf("Solve:   %.0f%% (%d/%d)   avg score %.2f", a.SolveRate*100, a.Resolved, a.Total, a.AvgScore)
	if a.Errored > 0 {
		solve += fmt.Sprintf("   errored %d (excluded from rates)", a.Errored)
	}
	fmt.Fprintln(out, solve)
	fmt.Fprintf(out, "Speed:   tok/s %.1f   TTFT %.0fms   total %.1fs\n", a.AvgTokensPerSecond, a.AvgTTFTms, float64(a.TotalMs)/1000)
	fmt.Fprintf(out, "Tokens:  in %d / out %d\n", a.TotalPromptTokens, a.TotalCompletionTokens)
	fmt.Fprintf(out, "GPU:     peak VRAM %dMB   util %.0f%%\n", a.PeakVRAMMB, a.AvgGPUUtil)
	fmt.Fprintln(out, "Problems:")
	for _, pr := range run.Problems {
		verdict := "fail"
		if pr.Resolved {
			verdict = "pass"
		}
		detail := pr.Detail
		if pr.Err != "" {
			detail = "err: " + pr.Err
		}
		fmt.Fprintf(out, "  [%s] %-28s score=%.2f tok/s=%.1f ttft=%dms  %s\n",
			verdict, pr.ProblemName, pr.Score, pr.TokensPerSecond, pr.TTFTms, detail)
	}
}

func benchPrintList(out io.Writer, store benchmarkstore.Store, asJSON bool) int {
	runs, err := store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "list runs: %v\n", err)
		return 1
	}
	if asJSON {
		_ = emitJSON(out, runs)
		return 0
	}
	if len(runs) == 0 {
		fmt.Fprintln(out, "no benchmark runs yet")
		return 0
	}
	fmt.Fprintf(out, "%-19s  %-20s  %-22s  %6s  %8s\n", "when", "profile", "mode", "solve", "tok/s")
	for _, r := range runs {
		mode := r.Mode.Title()
		if r.Err != "" {
			mode = "! " + mode // partial run
		}
		fmt.Fprintf(out, "%-19s  %-20s  %-22s  %5.0f%%  %8.1f\n",
			r.StartedAt.Format("2006-01-02 15:04"), clip(r.ProfileName, 20), clip(mode, 22),
			r.Aggregate.SolveRate*100, r.Aggregate.AvgTokensPerSecond)
	}
	return 0
}

func benchPrintHistory(out io.Writer, store benchmarkstore.Store, profileID string, asJSON bool) int {
	runs, err := store.ListByProfile(profileID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "history: %v\n", err)
		return 1
	}
	if asJSON {
		_ = emitJSON(out, runs)
		return 0
	}
	if len(runs) == 0 {
		fmt.Fprintf(out, "no runs for profile %q\n", profileID)
		return 0
	}
	fmt.Fprintf(out, "History — %s\n", runs[0].ProfileName)
	fmt.Fprintf(out, "%-19s  %-22s  %6s  %6s  %8s\n", "when", "mode", "solve", "score", "tok/s")
	for _, r := range runs {
		a := r.Aggregate
		fmt.Fprintf(out, "%-19s  %-22s  %5.0f%%  %6.2f  %8.1f\n",
			r.StartedAt.Format("2006-01-02 15:04"), clip(r.Mode.Title(), 22), a.SolveRate*100, a.AvgScore, a.AvgTokensPerSecond)
	}
	return 0
}

func benchPrintCompare(out io.Writer, store benchmarkstore.Store, asJSON bool) int {
	runs, err := store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "compare: %v\n", err)
		return 1
	}
	seen := map[string]bool{}
	latest := make([]benchmark.Run, 0)
	for _, r := range runs { // newest-first
		if seen[r.ProfileID] {
			continue
		}
		seen[r.ProfileID] = true
		latest = append(latest, r)
	}
	if asJSON {
		_ = emitJSON(out, latest)
		return 0
	}
	if len(latest) == 0 {
		fmt.Fprintln(out, "no runs to compare")
		return 0
	}
	fmt.Fprintf(out, "%-20s  %-22s  %6s  %6s  %8s  %8s  %s\n", "profile", "mode", "solve", "score", "tok/s", "vram", "quant")
	for _, r := range latest {
		a := r.Aggregate
		fmt.Fprintf(out, "%-20s  %-22s  %5.0f%%  %6.2f  %8.1f  %6dMB  %s\n",
			clip(r.ProfileName, 20), clip(r.Mode.Title(), 22), a.SolveRate*100, a.AvgScore,
			a.AvgTokensPerSecond, a.PeakVRAMMB, dashOr(r.Profile.Quantization))
	}
	return 0
}

func benchPrintTranscript(out io.Writer, store benchmarkstore.Store, id string, asJSON bool) int {
	tr, err := store.LoadTranscript(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "transcript: %v (run with benchmark.save_transcripts enabled)\n", err)
		return 1
	}
	if asJSON {
		_ = emitJSON(out, tr)
		return 0
	}
	for _, t := range tr {
		fmt.Fprintf(out, "════ %s (%s)  diffFound=%v\n", t.ProblemName, t.ProblemID, t.DiffFound)
		if t.Error != "" {
			fmt.Fprintf(out, "  ERROR: %s\n", t.Error)
		}
		fmt.Fprintln(out, "  ── model response ──")
		fmt.Fprintln(out, indent(t.ModelResponse, "    "))
		for i, jr := range t.JudgeRaw {
			fmt.Fprintf(out, "  ── judge sample %d ──\n", i+1)
			fmt.Fprintln(out, indent(jr, "    "))
		}
		fmt.Fprintln(out)
	}
	return 0
}
