package cli

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/service/benchmarkstore"
)

// newBenchListCmd builds `benchmark list` — the saved-runs table (newest first).
func newBenchListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List saved benchmark runs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, _, err := openBenchStore(cmd)
			if err != nil {
				return &ExitError{Code: 1}
			}
			return benchExit(benchPrintList(cmd.OutOrStdout(), cmd.ErrOrStderr(), store, jsonOut))
		},
	}
}

// newBenchCompareCmd builds `benchmark compare` — latest run per profile.
func newBenchCompareCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "compare",
		Short: "Compare the latest run of each profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, _, err := openBenchStore(cmd)
			if err != nil {
				return &ExitError{Code: 1}
			}
			return benchExit(benchPrintCompare(cmd.OutOrStdout(), cmd.ErrOrStderr(), store, jsonOut))
		},
	}
}

// newBenchHistoryCmd builds `benchmark history <profile-id>` — one profile's runs.
func newBenchHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history <profile-id>",
		Short: "Show a profile's benchmark history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, err := openBenchStore(cmd)
			if err != nil {
				return &ExitError{Code: 1}
			}
			return benchExit(benchPrintHistory(cmd.OutOrStdout(), cmd.ErrOrStderr(), store, args[0], jsonOut))
		},
	}
}

// newBenchShowCmd builds `benchmark show <run-id>` — one run's full result.
func newBenchShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <run-id>",
		Short: "Show a saved run's full result",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, err := openBenchStore(cmd)
			if err != nil {
				return &ExitError{Code: 1}
			}
			return benchExit(benchPrintShow(cmd.OutOrStdout(), cmd.ErrOrStderr(), store, args[0], jsonOut))
		},
	}
}

// newBenchTranscriptCmd builds `benchmark transcript <run-id>` — raw per-problem
// I/O captured during a run.
func newBenchTranscriptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "transcript <run-id>",
		Short: "Print the raw transcript of a saved run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, err := openBenchStore(cmd)
			if err != nil {
				return &ExitError{Code: 1}
			}
			return benchExit(benchPrintTranscript(cmd.OutOrStdout(), cmd.ErrOrStderr(), store, args[0], jsonOut))
		},
	}
}

// newBenchExportCmd builds `benchmark export <run-id> [--dir]` — writes the run
// to JSON + CSV for external analysis.
func newBenchExportCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "export <run-id>",
		Short: "Export a saved run to JSON + CSV",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, cfg, err := openBenchStore(cmd)
			if err != nil {
				return &ExitError{Code: 1}
			}
			out := dir
			if out == "" {
				out = filepath.Join(cfg.Paths.StateDir, "benchmark", "exports")
			}
			return benchExit(benchExport(cmd.OutOrStdout(), cmd.ErrOrStderr(), store, args[0], out))
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "output directory (default <state-dir>/benchmark/exports)")
	return cmd
}

// newBenchDeleteCmd builds `benchmark delete <run-id> --yes` — removes a run.
func newBenchDeleteCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <run-id>",
		Short: "Delete a saved run (requires --yes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, _, err := openBenchStore(cmd)
			if err != nil {
				return &ExitError{Code: 1}
			}
			return benchExit(benchDelete(cmd.OutOrStdout(), cmd.ErrOrStderr(), store, args[0], yes))
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm deletion (required — this is destructive)")
	return cmd
}

// benchPrintList renders the saved-runs table. The full run id is the last
// column so it can be fed to show/export/delete/transcript.
func benchPrintList(out, errw io.Writer, store benchmarkstore.Store, asJSON bool) int {
	runs, err := store.List()
	if err != nil {
		fmt.Fprintf(errw, "list runs: %v\n", err)
		return 1
	}
	if asJSON {
		if err := emitJSON(out, runs); err != nil {
			fmt.Fprintf(errw, "encode json: %v\n", err)
			return 1
		}
		return 0
	}
	if len(runs) == 0 {
		fmt.Fprintln(out, "no benchmark runs yet")
		return 0
	}
	rows := make([][]string, 0, len(runs))
	for _, r := range runs {
		mode := r.Mode.Title()
		if r.Err != "" {
			mode = "! " + mode // partial run
		}
		rows = append(rows, []string{
			r.StartedAt.Format("2006-01-02 15:04"),
			clip(r.ProfileName, 20),
			clip(mode, 22),
			fmt.Sprintf("%.0f%%", r.Aggregate.SolveRate*100),
			fmt.Sprintf("%.1f", r.Aggregate.AvgTokensPerSecond),
			r.ID,
		})
	}
	printTable(out, []string{"when", "profile", "mode", "solve", "tok/s", "id"}, rows)
	return 0
}

// benchPrintHistory renders one profile's run history.
func benchPrintHistory(out, errw io.Writer, store benchmarkstore.Store, profileID string, asJSON bool) int {
	runs, err := store.ListByProfile(profileID)
	if err != nil {
		fmt.Fprintf(errw, "history: %v\n", err)
		return 1
	}
	if asJSON {
		if err := emitJSON(out, runs); err != nil {
			fmt.Fprintf(errw, "encode json: %v\n", err)
			return 1
		}
		return 0
	}
	if len(runs) == 0 {
		fmt.Fprintf(out, "no runs for profile %q\n", profileID)
		return 0
	}
	fmt.Fprintf(out, "History — %s\n", runs[0].ProfileName)
	rows := make([][]string, 0, len(runs))
	for _, r := range runs {
		a := r.Aggregate
		rows = append(rows, []string{
			r.StartedAt.Format("2006-01-02 15:04"),
			clip(r.Mode.Title(), 22),
			fmt.Sprintf("%.0f%%", a.SolveRate*100),
			fmt.Sprintf("%.2f", a.AvgScore),
			fmt.Sprintf("%.1f", a.AvgTokensPerSecond),
		})
	}
	printTable(out, []string{"when", "mode", "solve", "score", "tok/s"}, rows)
	return 0
}

// benchPrintCompare renders the latest run of each profile.
func benchPrintCompare(out, errw io.Writer, store benchmarkstore.Store, asJSON bool) int {
	runs, err := store.List()
	if err != nil {
		fmt.Fprintf(errw, "compare: %v\n", err)
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
		if err := emitJSON(out, latest); err != nil {
			fmt.Fprintf(errw, "encode json: %v\n", err)
			return 1
		}
		return 0
	}
	if len(latest) == 0 {
		fmt.Fprintln(out, "no runs to compare")
		return 0
	}
	rows := make([][]string, 0, len(latest))
	for _, r := range latest {
		a := r.Aggregate
		rows = append(rows, []string{
			clip(r.ProfileName, 20),
			clip(r.Mode.Title(), 22),
			fmt.Sprintf("%.0f%%", a.SolveRate*100),
			fmt.Sprintf("%.2f", a.AvgScore),
			fmt.Sprintf("%.1f", a.AvgTokensPerSecond),
			fmt.Sprintf("%dMB", a.PeakVRAMMB),
			dashOr(r.Profile.Quantization),
		})
	}
	printTable(out, []string{"profile", "mode", "solve", "score", "tok/s", "vram", "quant"}, rows)
	return 0
}

// benchPrintShow loads a single run by id and renders its full result block.
func benchPrintShow(out, errw io.Writer, store benchmarkstore.Store, id string, asJSON bool) int {
	run, err := store.Load(id)
	if err != nil {
		fmt.Fprintf(errw, "show: %v\n", err)
		return 1
	}
	if asJSON {
		if err := emitJSON(out, run); err != nil {
			fmt.Fprintf(errw, "encode json: %v\n", err)
			return 1
		}
		return 0
	}
	printRun(out, run)
	return 0
}

// benchPrintTranscript prints the raw per-problem I/O captured for a run.
func benchPrintTranscript(out, errw io.Writer, store benchmarkstore.Store, id string, asJSON bool) int {
	tr, err := store.LoadTranscript(id)
	if err != nil {
		fmt.Fprintf(errw, "transcript: %v (run with benchmark.save_transcripts enabled)\n", err)
		return 1
	}
	if asJSON {
		if err := emitJSON(out, tr); err != nil {
			fmt.Fprintf(errw, "encode json: %v\n", err)
			return 1
		}
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

// benchExport writes a run to JSON + CSV under dir via benchmark.ExportRun.
func benchExport(out, errw io.Writer, store benchmarkstore.Store, id, dir string) int {
	run, err := store.Load(id)
	if err != nil {
		fmt.Fprintf(errw, "export: %v\n", err)
		return 1
	}
	jsonPath, csvPath, err := benchmark.ExportRun(run, dir)
	if err != nil {
		fmt.Fprintf(errw, "export: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "exported run %s:\n", run.ID)
	fmt.Fprintf(out, "  json: %s\n", jsonPath)
	fmt.Fprintf(out, "  csv:  %s\n", csvPath)
	return 0
}

// benchDelete removes a saved run. It refuses to act without an explicit --yes
// because deletion is destructive and headless.
func benchDelete(out, errw io.Writer, store benchmarkstore.Store, id string, yes bool) int {
	if !yes {
		fmt.Fprintln(errw, "refusing to delete without --yes")
		return 1
	}
	if err := store.Delete(id); err != nil {
		fmt.Fprintf(errw, "delete: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "deleted run %s\n", id)
	return 0
}

// printRun renders a completed run: the identity/aggregate block plus a
// per-problem table. Shared by `benchmark run` (final summary) and
// `benchmark show`.
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
	if !run.StartedAt.IsZero() && !run.FinishedAt.IsZero() {
		fmt.Fprintf(out, "Wall:    %s\n", fmtDur(run.FinishedAt.Sub(run.StartedAt)))
	}
	fmt.Fprintln(out, "Problems:")
	rows := make([][]string, 0, len(run.Problems))
	for _, pr := range run.Problems {
		verdict := "fail"
		switch {
		case pr.Err != "":
			verdict = "err"
		case pr.Resolved:
			verdict = "pass"
		}
		detail := pr.Detail
		if pr.Err != "" {
			detail = "err: " + pr.Err
		}
		rows = append(rows, []string{
			"[" + verdict + "]",
			clip(pr.ProblemName, 40),
			fmt.Sprintf("%.2f", pr.Score),
			fmt.Sprintf("%.1f", pr.TokensPerSecond),
			fmt.Sprintf("%dms", pr.TTFTms),
			detail,
		})
	}
	printTable(out, []string{"result", "problem", "score", "tok/s", "ttft", "detail"}, rows)
}
