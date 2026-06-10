package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/service/benchmarkstore"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
)

func init() {
	var (
		profileID  string
		modeStr    string
		list       bool
		compare    bool
		asJSON     bool
		minSolve   float64
		transcript string
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
			store := benchmarkstore.New(filepath.Join(cfg.Paths.StateDir, "benchmark", "runs"))

			if transcript != "" {
				return benchExit(benchPrintTranscript(store, transcript, asJSON))
			}
			if list {
				return benchExit(benchPrintList(store, asJSON))
			}
			if compare && profileID == "" {
				return benchExit(benchPrintCompare(store, asJSON))
			}
			if profileID == "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "usage:")
				fmt.Fprintln(cmd.ErrOrStderr(), "  model-loader benchmark --profile <id> [--mode judge|longctx|llama-bench] [--json] [--min-solve N]")
				fmt.Fprintln(cmd.ErrOrStderr(), "  model-loader benchmark --list [--json]")
				fmt.Fprintln(cmd.ErrOrStderr(), "  model-loader benchmark --compare [--profile <id>] [--json]")
				fmt.Fprintln(cmd.ErrOrStderr(), "  model-loader benchmark --transcript <run-id> [--json]")
				return &ExitError{Code: 1}
			}
			if compare {
				return benchExit(benchPrintHistory(store, profileID, asJSON))
			}

			mode, ok := parseBenchMode(modeStr)
			if !ok {
				fmt.Fprintf(cmd.ErrOrStderr(), "unknown mode %q (want judge|longctx|llama-bench)\n", modeStr)
				return &ExitError{Code: 1}
			}

			release, acquired, lErr := app.AcquireSingleInstanceLock(cfg.Paths.StateDir)
			if lErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "single-instance lock: %v\n", lErr)
			}
			if release != nil {
				defer release()
			}
			if !acquired {
				fmt.Fprintln(cmd.ErrOrStderr(), "another model-loader instance is running (TUI/serve) — close it first.")
				return &ExitError{Code: 1}
			}

			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()

			mon := monitor.New(monitor.Config{NvidiaSMIPath: "nvidia-smi"})
			runner, err := benchmark.NewRunner(svc.Store, svc.Mgr, mon, svc.Resolver, benchmark.Config{
				MaxTokens:         cfg.Benchmark.MaxTokens,
				Temperature:       cfg.Benchmark.Temperature,
				Timeout:           time.Duration(cfg.Benchmark.TimeoutSec) * time.Second,
				LongContextTokens: cfg.Benchmark.LongContextTokens,
				SaveTranscripts:   true,
				Judge: benchmark.JudgeEndpoint{
					BaseURL: cfg.Benchmark.Judge.BaseURL,
					APIKey:  cfg.Benchmark.Judge.APIKey,
					Model:   cfg.Benchmark.Judge.Model,
					Samples: cfg.Benchmark.Judge.Samples,
				},
				LlamaBenchPresets: cfg.Benchmark.LlamaBench.Presets,
				LlamaBenchReps:    cfg.Benchmark.LlamaBench.Repetitions,
			})
			if err != nil {
				svc.Logger.Error("benchmark_engine_init_failed", "err", err)
				fmt.Fprintf(cmd.ErrOrStderr(), "benchmark engine: %v\n", err)
				return &ExitError{Code: 1}
			}

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			progress := make(chan benchmark.Progress, 32)
			drained := make(chan struct{})
			go func() {
				for p := range progress {
					switch p.Phase {
					case "launch":
						fmt.Fprintln(cmd.ErrOrStderr(), "launching backend / waiting for /health…")
					case "infer", "score":
						fmt.Fprintf(cmd.ErrOrStderr(), "[%d/%d] %s (%s)\n", p.Index, p.Total, p.ProblemName, p.Phase)
					}
				}
				close(drained)
			}()

			run, err := runner.Run(ctx, benchmark.RunConfig{ProfileID: profileID, Mode: mode}, progress)
			close(progress)
			<-drained
			if err != nil {
				// Persist whatever completed before the failure/SIGINT so the
				// partial data shows up (flagged) in the TUI and --list.
				if len(run.Problems) > 0 {
					if sErr := store.Save(run); sErr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not save partial run: %v\n", sErr)
					} else {
						fmt.Fprintf(cmd.ErrOrStderr(), "partial run saved: %s\n", run.ID)
					}
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "run failed: %v\n", err)
				return &ExitError{Code: 1}
			}
			if err := store.Save(run); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not save run: %v\n", err)
			}
			if len(run.Transcript) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "transcript: %s\n", store.TranscriptPath(run.ID))
				fmt.Fprintf(cmd.ErrOrStderr(), "inspect raw output: model-loader benchmark --transcript %s\n", run.ID)
			}

			if asJSON {
				_ = emitJSON(os.Stdout, run)
			} else {
				printRun(run)
			}
			if minSolve >= 0 && run.Aggregate.SolveRate < minSolve {
				fmt.Fprintf(cmd.ErrOrStderr(), "FAIL: solve rate %.2f below --min-solve %.2f\n", run.Aggregate.SolveRate, minSolve)
				return &ExitError{Code: 2}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&profileID, "profile", "", "profile id to benchmark")
	cmd.Flags().StringVar(&modeStr, "mode", "judge", "scoring mode: judge | math-bench | codegen-bench | ragas-bench | summary-bench | llama-bench | longctx | instruction-bench | mmlu-bench")
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

func printRun(run benchmark.Run) {
	a := run.Aggregate
	fmt.Printf("Profile: %s (%s)\n", run.ProfileName, run.ProfileID)
	fmt.Printf("Mode:    %s\n", run.Mode.Title())
	fmt.Printf("Model:   %s   quant=%s  cache k/v=%s/%s  ctx=%d\n",
		run.Profile.Model, dashOr(run.Profile.Quantization), dashOr(run.Profile.CacheTypeK), dashOr(run.Profile.CacheTypeV), run.Profile.CtxSize)
	if run.Err != "" {
		fmt.Printf("Error:   %s (partial run)\n", run.Err)
	}
	solve := fmt.Sprintf("Solve:   %.0f%% (%d/%d)   avg score %.2f", a.SolveRate*100, a.Resolved, a.Total, a.AvgScore)
	if a.Errored > 0 {
		solve += fmt.Sprintf("   errored %d (excluded from rates)", a.Errored)
	}
	fmt.Println(solve)
	fmt.Printf("Speed:   tok/s %.1f   TTFT %.0fms   total %.1fs\n", a.AvgTokensPerSecond, a.AvgTTFTms, float64(a.TotalMs)/1000)
	fmt.Printf("Tokens:  in %d / out %d\n", a.TotalPromptTokens, a.TotalCompletionTokens)
	fmt.Printf("GPU:     peak VRAM %dMB   util %.0f%%\n", a.PeakVRAMMB, a.AvgGPUUtil)
	fmt.Println("Problems:")
	for _, pr := range run.Problems {
		verdict := "fail"
		if pr.Resolved {
			verdict = "pass"
		}
		detail := pr.Detail
		if pr.Err != "" {
			detail = "err: " + pr.Err
		}
		fmt.Printf("  [%s] %-28s score=%.2f tok/s=%.1f ttft=%dms  %s\n",
			verdict, pr.ProblemName, pr.Score, pr.TokensPerSecond, pr.TTFTms, detail)
	}
}

func benchPrintList(store benchmarkstore.Store, asJSON bool) int {
	runs, err := store.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "list runs: %v\n", err)
		return 1
	}
	if asJSON {
		_ = emitJSON(os.Stdout, runs)
		return 0
	}
	if len(runs) == 0 {
		fmt.Println("no benchmark runs yet")
		return 0
	}
	fmt.Printf("%-19s  %-20s  %-22s  %6s  %8s\n", "when", "profile", "mode", "solve", "tok/s")
	for _, r := range runs {
		mode := r.Mode.Title()
		if r.Err != "" {
			mode = "! " + mode // partial run
		}
		fmt.Printf("%-19s  %-20s  %-22s  %5.0f%%  %8.1f\n",
			r.StartedAt.Format("2006-01-02 15:04"), clip(r.ProfileName, 20), clip(mode, 22),
			r.Aggregate.SolveRate*100, r.Aggregate.AvgTokensPerSecond)
	}
	return 0
}

func benchPrintHistory(store benchmarkstore.Store, profileID string, asJSON bool) int {
	runs, err := store.ListByProfile(profileID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "history: %v\n", err)
		return 1
	}
	if asJSON {
		_ = emitJSON(os.Stdout, runs)
		return 0
	}
	if len(runs) == 0 {
		fmt.Printf("no runs for profile %q\n", profileID)
		return 0
	}
	fmt.Printf("History — %s\n", runs[0].ProfileName)
	fmt.Printf("%-19s  %-22s  %6s  %6s  %8s\n", "when", "mode", "solve", "score", "tok/s")
	for _, r := range runs {
		a := r.Aggregate
		fmt.Printf("%-19s  %-22s  %5.0f%%  %6.2f  %8.1f\n",
			r.StartedAt.Format("2006-01-02 15:04"), clip(r.Mode.Title(), 22), a.SolveRate*100, a.AvgScore, a.AvgTokensPerSecond)
	}
	return 0
}

func benchPrintCompare(store benchmarkstore.Store, asJSON bool) int {
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
		_ = emitJSON(os.Stdout, latest)
		return 0
	}
	if len(latest) == 0 {
		fmt.Println("no runs to compare")
		return 0
	}
	fmt.Printf("%-20s  %-22s  %6s  %6s  %8s  %8s  %s\n", "profile", "mode", "solve", "score", "tok/s", "vram", "quant")
	for _, r := range latest {
		a := r.Aggregate
		fmt.Printf("%-20s  %-22s  %5.0f%%  %6.2f  %8.1f  %6dMB  %s\n",
			clip(r.ProfileName, 20), clip(r.Mode.Title(), 22), a.SolveRate*100, a.AvgScore,
			a.AvgTokensPerSecond, a.PeakVRAMMB, dashOr(r.Profile.Quantization))
	}
	return 0
}

func benchPrintTranscript(store benchmarkstore.Store, id string, asJSON bool) int {
	tr, err := store.LoadTranscript(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "transcript: %v (run with benchmark.save_transcripts enabled)\n", err)
		return 1
	}
	if asJSON {
		_ = emitJSON(os.Stdout, tr)
		return 0
	}
	for _, t := range tr {
		fmt.Printf("════ %s (%s)  diffFound=%v\n", t.ProblemName, t.ProblemID, t.DiffFound)
		if t.Error != "" {
			fmt.Printf("  ERROR: %s\n", t.Error)
		}
		fmt.Println("  ── model response ──")
		fmt.Println(indent(t.ModelResponse, "    "))
		for i, jr := range t.JudgeRaw {
			fmt.Printf("  ── judge sample %d ──\n", i+1)
			fmt.Println(indent(jr, "    "))
		}
		fmt.Println()
	}
	return 0
}

