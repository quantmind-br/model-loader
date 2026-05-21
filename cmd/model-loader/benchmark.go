package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/service/benchmarkstore"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
)

// runBenchmark is the headless `model-loader benchmark` subcommand. It can run
// one evaluation against a profile, list saved runs, or print comparisons —
// reusing the same services as the TUI via bootstrap().
//
// Exit codes: 0 ok, 1 error/usage, 2 solve rate below --min-solve gate.
func runBenchmark() int {
	cliLevel := flag.String("log-level", "", "override log level (debug|info|warn|error)")
	profileID := flag.String("profile", "", "profile id to benchmark")
	modeStr := flag.String("mode", "judge", "scoring mode: judge (SWE-bench Lite) | longctx (needle diagnostic)")
	list := flag.Bool("list", false, "list saved runs and exit")
	compare := flag.Bool("compare", false, "with --profile: print that profile's run history; alone: latest run per profile")
	asJSON := flag.Bool("json", false, "emit JSON instead of text")
	minSolve := flag.Float64("min-solve", -1, "exit code 2 if solve rate < this (0..1); -1 disables")
	transcript := flag.String("transcript", "", "print the raw model/judge transcript of a saved run id and exit")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 1
	}
	store := benchmarkstore.New(filepath.Join(cfg.Paths.StateDir, "benchmark", "runs"))

	// Read-only queries: no backend, no lock, and crucially no bootstrap —
	// bootstrap's Reconcile rewrites instances.json, which must not happen for
	// a `--list`/`--compare`/`--transcript` invocation or while unguarded.
	if *transcript != "" {
		return benchPrintTranscript(store, *transcript, *asJSON)
	}
	if *list {
		return benchPrintList(store, *asJSON)
	}
	if *compare && *profileID == "" {
		return benchPrintCompare(store, *asJSON)
	}
	if *profileID == "" {
		fmt.Fprintln(os.Stderr, "usage:")
		fmt.Fprintln(os.Stderr, "  model-loader benchmark --profile <id> [--mode judge|longctx] [--json] [--min-solve N]")
		fmt.Fprintln(os.Stderr, "  model-loader benchmark --list [--json]")
		fmt.Fprintln(os.Stderr, "  model-loader benchmark --compare [--profile <id>] [--json]")
		fmt.Fprintln(os.Stderr, "  model-loader benchmark --transcript <run-id> [--json]")
		return 1
	}
	if *compare {
		return benchPrintHistory(store, *profileID, *asJSON)
	}

	mode, ok := parseBenchMode(*modeStr)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown mode %q (want judge|longctx)\n", *modeStr)
		return 1
	}

	// Acquire the single-instance lock BEFORE bootstrap: bootstrap constructs
	// processmgr and runs Reconcile, which rewrites instances.json. Guarding
	// first prevents clobbering a running TUI/serve registry.
	release, acquired, lErr := acquireSingleInstanceLock(cfg.Paths.StateDir)
	if lErr != nil {
		fmt.Fprintf(os.Stderr, "single-instance lock: %v\n", lErr)
	}
	if release != nil {
		defer release()
	}
	if !acquired {
		fmt.Fprintln(os.Stderr, "another model-loader instance is running (TUI/serve) — close it first.")
		return 1
	}

	_, logger, closeLog, svc, err := bootstrap(*cliLevel)
	if err != nil {
		return 1
	}
	defer closeLog()
	defer svc.mgr.Close()

	mon := monitor.New(monitor.Config{NvidiaSMIPath: "nvidia-smi"})
	runner, err := benchmark.NewRunner(svc.store, svc.mgr, mon, svc.resolver, benchmark.Config{
		MaxTokens:         cfg.Benchmark.MaxTokens,
		Temperature:       cfg.Benchmark.Temperature,
		Timeout:           time.Duration(cfg.Benchmark.TimeoutSec) * time.Second,
		LongContextTokens: cfg.Benchmark.LongContextTokens,
		SaveTranscripts:   true, // CLI always captures transcripts for debugging
		Judge: benchmark.JudgeEndpoint{
			BaseURL: cfg.Benchmark.Judge.BaseURL,
			APIKey:  cfg.Benchmark.Judge.APIKey,
			Model:   cfg.Benchmark.Judge.Model,
			Samples: cfg.Benchmark.Judge.Samples,
		},
	})
	if err != nil {
		logger.Error("benchmark_engine_init_failed", "err", err)
		fmt.Fprintf(os.Stderr, "benchmark engine: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	progress := make(chan benchmark.Progress, 32)
	drained := make(chan struct{})
	go func() {
		for p := range progress {
			switch p.Phase {
			case "launch":
				fmt.Fprintln(os.Stderr, "launching backend / waiting for /health…")
			case "infer", "score":
				fmt.Fprintf(os.Stderr, "[%d/%d] %s (%s)\n", p.Index, p.Total, p.ProblemName, p.Phase)
			}
		}
		close(drained)
	}()

	run, err := runner.Run(ctx, benchmark.RunConfig{ProfileID: *profileID, Mode: mode}, progress)
	close(progress)
	<-drained
	if err != nil {
		fmt.Fprintf(os.Stderr, "run failed: %v\n", err)
		return 1
	}
	if err := store.Save(run); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save run: %v\n", err)
	}
	if len(run.Transcript) > 0 {
		fmt.Fprintf(os.Stderr, "transcript: %s\n", store.TranscriptPath(run.ID))
		fmt.Fprintf(os.Stderr, "inspect raw output: model-loader benchmark --transcript %s\n", run.ID)
	}

	if *asJSON {
		emitJSON(run)
	} else {
		printRun(run)
	}
	if *minSolve >= 0 && run.Aggregate.SolveRate < *minSolve {
		fmt.Fprintf(os.Stderr, "FAIL: solve rate %.2f below --min-solve %.2f\n", run.Aggregate.SolveRate, *minSolve)
		return 2
	}
	return 0
}

func parseBenchMode(s string) (benchmark.Mode, bool) {
	switch s {
	case "judge":
		return benchmark.ModeJudge, true
	case "longctx", "long-context":
		return benchmark.ModeLongContext, true
	}
	return "", false
}

func emitJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func printRun(run benchmark.Run) {
	a := run.Aggregate
	fmt.Printf("Profile: %s (%s)\n", run.ProfileName, run.ProfileID)
	fmt.Printf("Mode:    %s\n", run.Mode.Title())
	fmt.Printf("Model:   %s   quant=%s  cache k/v=%s/%s  ctx=%d\n",
		run.Profile.Model, dashOr(run.Profile.Quantization), dashOr(run.Profile.CacheTypeK), dashOr(run.Profile.CacheTypeV), run.Profile.CtxSize)
	if run.Err != "" {
		fmt.Printf("Error:   %s\n", run.Err)
	}
	fmt.Printf("Solve:   %.0f%% (%d/%d)   avg score %.2f\n", a.SolveRate*100, a.Resolved, a.Total, a.AvgScore)
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
		emitJSON(runs)
		return 0
	}
	if len(runs) == 0 {
		fmt.Println("no benchmark runs yet")
		return 0
	}
	fmt.Printf("%-19s  %-20s  %-22s  %6s  %8s\n", "when", "profile", "mode", "solve", "tok/s")
	for _, r := range runs {
		fmt.Printf("%-19s  %-20s  %-22s  %5.0f%%  %8.1f\n",
			r.StartedAt.Format("2006-01-02 15:04"), clip(r.ProfileName, 20), clip(r.Mode.Title(), 22),
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
		emitJSON(runs)
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
		emitJSON(latest)
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
		emitJSON(tr)
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

func indent(s, pad string) string {
	if s == "" {
		return pad + "(empty)"
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

func dashOr(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}
