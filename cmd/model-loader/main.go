// Command model-loader launches the TUI for managing LLM server profiles,
// or runs the headless HTTP proxy via `model-loader serve`.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/cli"
	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/service/benchmarkstore"
	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/quantmind-br/model-loader/internal/service/hfhub"
	"github.com/quantmind-br/model-loader/internal/service/modelscanner"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/service/proxysupervisor"
	"github.com/quantmind-br/model-loader/internal/ui"
	"github.com/quantmind-br/model-loader/internal/ui/pages"
)

func main() {
	cli.TUIRunner = runTUI
	os.Exit(cli.Execute())
}

func runTUI(cliLevel string) int {
	// Single-instance guard: a second interactive TUI sharing the same state
	// dir would race on instances.json / proxy-state.json. Acquire the lock
	// before bootstrap so we don't run the boot-time reconcile from a duplicate
	// session. Config-load failures here are non-fatal — Bootstrap reports them.
	if early, cErr := config.Load(); cErr == nil {
		release, acquired, lErr := app.AcquireSingleInstanceLock(early.Paths.StateDir)
		if lErr != nil {
			fmt.Fprintf(os.Stderr, "single-instance lock: %v\n", lErr)
		}
		if release != nil {
			defer release()
		}
		if !acquired {
			fmt.Fprintln(os.Stderr, "model-loader is already running (another instance holds the state lock).")
			fmt.Fprintln(os.Stderr, "Close the other session first — two instances would clobber instances.json / proxy-state.json.")
			return 1
		}
	}

	svc, err := app.Bootstrap(cliLevel)
	if err != nil {
		return 1
	}
	defer svc.Close()

	httpClient := &http.Client{Timeout: 30 * time.Second}
	hfClient := hfhub.NewClient(httpClient, "model-loader/dev")
	dlStateDir := filepath.Join(svc.Cfg.Paths.StateDir, "downloads")
	dlManager := downloadmgr.NewManager(dlStateDir, 3).WithUserAgent("model-loader/dev")
	if err := dlManager.Reconcile(); err != nil {
		svc.Logger.Error("download_reconcile_failed", "err", err)
	}
	dlManager.StartPolling()
	defer dlManager.Close()

	scanner := modelscanner.New()
	exportDir := resolveExportDir(svc.Cfg.Paths.StateDir, svc.Logger)

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
	// The proxy is the only client-facing channel to backends — make sure it
	// is up before the user loads anything.
	if !supervisor.Status().Running {
		startCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := supervisor.Start(startCtx); err != nil {
			svc.Logger.Error("proxy_autostart_failed", "err", err)
		}
		cancel()
	}

	profilesPage := pages.NewProfilesPage(svc.Store, svc.DefaultSchema).
		WithModelScanner(scanner, svc.Cfg.Models.SearchPaths).
		WithBackendCatalog(svc.CatalogStore, svc.SchemaStore).
		WithExportDir(exportDir)
	modelsPage := pages.NewModelsPage(scanner, svc.Cfg.Models.SearchPaths).
		WithProfileStore(svc.Store).
		WithSearchPathPersister(config.UpdateSearchPaths).
		WithHFClient(hfClient).
		WithDownloadManager(dlManager)
	profilesPage = profilesPage.
		WithProxyController(supervisor).
		WithValidator(svc.Val).
		WithBackendResolver(svc.Resolver).
		WithLogger(svc.Logger)

	mon := monitor.New(monitor.Config{NvidiaSMIPath: "nvidia-smi"})
	serverPage := pages.NewServerPage(svc.Mgr, mon, svc.Store).
		WithMetricsDir(filepath.Join(svc.Cfg.Paths.StateDir, "metrics")).
		WithProxy(supervisor)
	prober := backendcatalog.NewProber(svc.CatalogStore, backendcatalog.ProbeConfig{Timeout: 10 * time.Second})
	backendsPage := pages.NewBackendsPage(svc.SchemaManager).WithStores(svc.CatalogStore, svc.SchemaStore).WithProber(prober)

	benchStore := benchmarkstore.New(filepath.Join(svc.Cfg.Paths.StateDir, "benchmark", "runs"))
	benchRunner, err := benchmark.NewRunner(svc.Store, mon, supervisor, benchmark.Config{
		MaxTokens:         svc.Cfg.Benchmark.MaxTokens,
		Limit:             svc.Cfg.Benchmark.Limit,
		Temperature:       svc.Cfg.Benchmark.Temperature,
		Timeout:           time.Duration(svc.Cfg.Benchmark.TimeoutSec) * time.Second,
		LongContextTokens: svc.Cfg.Benchmark.LongContextTokens,
		SaveTranscripts:   svc.Cfg.Benchmark.SaveTranscripts,
		Judge: benchmark.JudgeEndpoint{
			BaseURL: svc.Cfg.Benchmark.Judge.BaseURL,
			APIKey:  svc.Cfg.Benchmark.Judge.APIKey,
			Model:   svc.Cfg.Benchmark.Judge.Model,
			Samples: svc.Cfg.Benchmark.Judge.Samples,
		},
		LlamaBenchPresets: svc.Cfg.Benchmark.LlamaBench.Presets,
		LlamaBenchReps:    svc.Cfg.Benchmark.LlamaBench.Repetitions,
		LlamaBenchWarmup:  svc.Cfg.Benchmark.LlamaBench.Warmup,
		EmbeddingsBaseURL: svc.Cfg.Benchmark.Embeddings.BaseURL,

		TerminalBenchCmd:        svc.Cfg.Benchmark.TerminalBench.Command,
		TerminalBenchAgent:      svc.Cfg.Benchmark.TerminalBench.Agent,
		TerminalBenchDataset:    svc.Cfg.Benchmark.TerminalBench.Dataset,
		TerminalBenchProvider:   svc.Cfg.Benchmark.TerminalBench.Provider,
		TerminalBenchTasks:      svc.Cfg.Benchmark.TerminalBench.Tasks,
		TerminalBenchNTasks:     svc.Cfg.Benchmark.TerminalBench.NTasks,
		TerminalBenchConcurrent: svc.Cfg.Benchmark.TerminalBench.Concurrent,
		TerminalBenchTimeout:    time.Duration(svc.Cfg.Benchmark.TerminalBench.TimeoutSec) * time.Second,
		TerminalBenchExtraArgs:  svc.Cfg.Benchmark.TerminalBench.ExtraArgs,

		SweBenchProHarnessDir:    svc.Cfg.Benchmark.SweBenchPro.HarnessDir,
		SweBenchProRawSample:     svc.Cfg.Benchmark.SweBenchPro.RawSamplePath,
		SweBenchProScriptsDir:    svc.Cfg.Benchmark.SweBenchPro.ScriptsDir,
		SweBenchProDockerhubUser: svc.Cfg.Benchmark.SweBenchPro.DockerhubUser,
		SweBenchProPython:        svc.Cfg.Benchmark.SweBenchPro.Python,
		SweBenchProNumWorkers:    svc.Cfg.Benchmark.SweBenchPro.NumWorkers,
		SweBenchProUseModal:      svc.Cfg.Benchmark.SweBenchPro.UseModal,
		SweBenchProInstances:     svc.Cfg.Benchmark.SweBenchPro.Instances,
		SweBenchProPatchPath:     svc.Cfg.Benchmark.SweBenchPro.PatchPath,
		SweBenchProAgentCmd:      svc.Cfg.Benchmark.SweBenchPro.AgentCmd,
		SweBenchProTimeout:       time.Duration(svc.Cfg.Benchmark.SweBenchPro.TimeoutSec) * time.Second,
		SweBenchProExtraArgs:     svc.Cfg.Benchmark.SweBenchPro.ExtraArgs,

		DeepSWECmd:        svc.Cfg.Benchmark.DeepSWE.Command,
		DeepSWETasksDir:   svc.Cfg.Benchmark.DeepSWE.TasksDir,
		DeepSWEAgent:      svc.Cfg.Benchmark.DeepSWE.Agent,
		DeepSWEProvider:   svc.Cfg.Benchmark.DeepSWE.Provider,
		DeepSWEModelClass: svc.Cfg.Benchmark.DeepSWE.ModelClass,
		DeepSWEAPIBase:    svc.Cfg.Benchmark.DeepSWE.APIBase,
		DeepSWETasks:      svc.Cfg.Benchmark.DeepSWE.Tasks,
		DeepSWENTasks:     svc.Cfg.Benchmark.DeepSWE.NTasks,
		DeepSWESampleSeed: svc.Cfg.Benchmark.DeepSWE.SampleSeed,
		DeepSWEConcurrent: svc.Cfg.Benchmark.DeepSWE.Concurrent,
		DeepSWETimeout:    time.Duration(svc.Cfg.Benchmark.DeepSWE.TimeoutSec) * time.Second,
		DeepSWEExtraArgs:  svc.Cfg.Benchmark.DeepSWE.ExtraArgs,
	})
	if err != nil {
		svc.Logger.Error("benchmark_dataset_load_failed", "err", err)
		benchRunner = nil
	}
	benchmarkPage := pages.NewBenchmarkPage(svc.Store, benchStore, benchRunner,
		filepath.Join(svc.Cfg.Paths.StateDir, "benchmark", "exports"))

	root := ui.NewRoot(parseTab(svc.Cfg.UI.DefaultTab)).
		WithProfilesPage(profilesPage).
		WithModelsPage(modelsPage).
		WithServerPage(serverPage).
		WithBackendsPage(backendsPage).
		WithBenchmarkPage(benchmarkPage).
		WithProcessManager(svc.Mgr)

	prog := tea.NewProgram(root, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		svc.Logger.Error("tui_error", "err", err)
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		return 1
	}

	svc.Logger.Info("app_exit", "residual_instances", len(svc.Mgr.List()))
	if running := svc.Mgr.List(); len(running) > 0 {
		fmt.Fprintf(os.Stderr, "%d background instance(s) still running:\n", len(running))
		svc.Logger.Warn("orphan_background_instances", "count", len(running))
		for _, ri := range running {
			fmt.Fprintf(os.Stderr, "  PID %d (port %d)\n", ri.PID, ri.Port)
			svc.Logger.Warn("orphan_instance",
				"pid", ri.PID, "port", ri.Port, "profile_id", ri.ProfileID)
		}
		fmt.Fprintln(os.Stderr, "Restart the TUI to manage them.")
		svc.Logger.Warn("orphan_remediation_hint", "hint", "Restart the TUI to manage them.")
	}
	return 0
}

// resolveExportDir returns the directory ProfilesPage writes JSON export
// bundles into. It lives under <state-dir>/exports and is created lazily.
// On mkdir failure we log a warning and return "" so the [e] shortcut
// degrades to a "not configured" flash instead of crashing boot.
func resolveExportDir(stateDir string, logger *slog.Logger) string {
	if stateDir == "" {
		return ""
	}
	dir := filepath.Join(stateDir, "exports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warn("export_dir_unavailable", "dir", dir, "err", err)
		return ""
	}
	return dir
}

func parseTab(name string) ui.Tab {
	switch name {
	case "profiles":
		return ui.TabProfiles
	case "server":
		return ui.TabServer
	case "models":
		return ui.TabModels
	case "backends":
		return ui.TabBackends
	case "benchmark":
		return ui.TabBenchmark
	default:
		return ui.TabProfiles
	}
}
