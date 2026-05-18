// Command model-loader launches the TUI for managing LLM server profiles,
// or runs the headless HTTP proxy via `model-loader serve`.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/quantmind-br/model-loader/internal/service/hfhub"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
	"github.com/quantmind-br/model-loader/internal/service/modelscanner"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/service/proxysupervisor"
	"github.com/quantmind-br/model-loader/internal/ui"
	"github.com/quantmind-br/model-loader/internal/ui/pages"
)

func main() {
	// Dispatch on the first positional argument. We strip the subcommand
	// from os.Args so the downstream flag.Parse only sees flags, which
	// keeps `model-loader serve --log-level=debug` working.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			os.Args = append(os.Args[:1], os.Args[2:]...)
			os.Exit(runServe())
		case "import":
			os.Args = append(os.Args[:1], os.Args[2:]...)
			os.Exit(runImport())
		}
	}
	os.Exit(runTUI())
}

func runTUI() int {
	cliLevel := flag.String("log-level", "", "override log level (debug|info|warn|error); also reads $MODEL_LOADER_LOG_LEVEL and config logging.level")
	flag.Parse()

	cfg, logger, closeLog, svc, err := bootstrap(*cliLevel)
	if err != nil {
		return 1
	}
	defer closeLog()
	defer svc.mgr.Close()

	httpClient := &http.Client{Timeout: 30 * time.Second}
	hfClient := hfhub.NewClient(httpClient, "model-loader/dev")
	dlManager := downloadmgr.NewManager(httpClient, 3).WithUserAgent("model-loader/dev")
	defer dlManager.Close()

	scanner := modelscanner.New()
	exportDir := resolveExportDir(cfg.Paths.StateDir, logger)

	supervisor := proxysupervisor.New(proxysupervisor.Config{
		StatePath: filepath.Join(cfg.Paths.StateDir, "proxy-state.json"),
		LogDir:    cfg.Paths.LogDir,
		Host:      cfg.Serve.Host,
		Port:      cfg.Serve.Port,
		Logger:    logger,
	})
	if err := supervisor.Reconcile(); err != nil {
		logger.Error("proxy_reconcile_failed", "err", err)
	}

	profilesPage := pages.NewProfilesPage(svc.store, svc.defaultSchema).
		WithModelScanner(scanner, cfg.Models.SearchPaths).
		WithBackendCatalog(svc.catalogStore, svc.schemaStore).
		WithExportDir(exportDir)
	modelsPage := pages.NewModelsPage(scanner, cfg.Models.SearchPaths).
		WithProfileStore(svc.store).
		WithHFClient(hfClient).
		WithDownloadManager(dlManager)
	launcherPage := pages.NewLauncherPage(svc.store, svc.mgr, svc.val).
		SetBackendResolver(svc.resolver).
		WithLogger(logger)

	mon := monitor.New(monitor.Config{NvidiaSMIPath: "nvidia-smi"})
	serverPage := pages.NewServerPage(svc.mgr, mon, svc.store).
		WithMetricsDir(filepath.Join(cfg.Paths.StateDir, "metrics")).
		SetBackendResolver(svc.resolver).
		WithProxy(supervisor)
	prober := backendcatalog.NewProber(svc.catalogStore, backendcatalog.ProbeConfig{Timeout: 10 * time.Second})
	backendsPage := pages.NewBackendsPage(svc.schemaManager).WithProber(prober)

	root := ui.NewRoot(parseTab(cfg.UI.DefaultTab)).
		WithProfilesPage(profilesPage).
		WithModelsPage(modelsPage).
		WithLauncherPage(launcherPage).
		WithServerPage(serverPage).
		WithBackendsPage(backendsPage).
		WithProcessManager(svc.mgr)

	prog := tea.NewProgram(root, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		logger.Error("tui_error", "err", err)
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		return 1
	}

	logger.Info("app_exit", "residual_instances", len(svc.mgr.List()))
	if running := svc.mgr.List(); len(running) > 0 {
		fmt.Fprintf(os.Stderr, "%d background instance(s) still running:\n", len(running))
		logger.Warn("orphan_background_instances", "count", len(running))
		for _, ri := range running {
			fmt.Fprintf(os.Stderr, "  PID %d (port %d)\n", ri.PID, ri.Port)
			logger.Warn("orphan_instance",
				"pid", ri.PID, "port", ri.Port, "profile_id", ri.ProfileID)
		}
		fmt.Fprintln(os.Stderr, "Restart the TUI to manage them.")
		logger.Warn("orphan_remediation_hint", "hint", "Restart the TUI to manage them.")
	}
	return 0
}

func runServe() int {
	cliLevel := flag.String("log-level", "", "override log level (debug|info|warn|error); also reads $MODEL_LOADER_LOG_LEVEL and config logging.level")
	cliHost := flag.String("host", "", "override bind host")
	cliPort := flag.Int("port", 0, "override bind port")
	flag.Parse()

	cfg, logger, closeLog, svc, err := bootstrap(*cliLevel)
	if err != nil {
		return 1
	}
	defer closeLog()
	defer svc.mgr.Close()

	if *cliHost != "" {
		cfg.Serve.Host = *cliHost
	}
	if *cliPort != 0 {
		cfg.Serve.Port = *cliPort
	}

	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := httpproxy.New(httpproxy.Config{
		Host:                cfg.Serve.Host,
		Port:                cfg.Serve.Port,
		HealthCheckTimeout:  120 * time.Second,
		MaxBodyBuffer:       8 << 20,
		ShutdownGracePeriod: 10 * time.Second,
	}, httpproxy.Deps{
		ProfileStore: svc.store,
		ProcessMgr:   svc.mgr,
		Logger:       logger,
	})

	if err := srv.Start(ctx); err != nil {
		logger.Error("serve_start_failed", "err", err)
		fmt.Fprintf(os.Stderr, "start: %v\n", err)
		return 1
	}
	fmt.Printf("Listening on %s:%d (logs: %s)\n",
		cfg.Serve.Host, cfg.Serve.Port, cfg.Paths.LogDir)
	logger.Info("serve_listening",
		"host", cfg.Serve.Host, "port", cfg.Serve.Port)

	<-ctx.Done()
	logger.Info("serve_signal_received")

	shCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Stop(shCtx); err != nil {
		logger.Error("serve_stop_failed", "err", err)
		fmt.Fprintf(os.Stderr, "stop: %v\n", err)
		return 1
	}
	return 0
}

// resolveExportDir returns the directory ProfilesPage writes JSON export
// bundles into. It lives under <state-dir>/exports and is created lazily.
// On mkdir failure we log a warning and return "" so the [e] shortcut
// degrades to a "not configured" flash instead of crashing boot.
func runImport() int {
	cliLevel := flag.String("log-level", "", "override log level")
	modeFlag := flag.String("mode", "merge", "conflict resolution: merge|overwrite|rename")
	flag.Parse()

	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: model-loader import <path> [--mode=merge|overwrite|rename]")
		return 1
	}
	path := flag.Arg(0)

	mode := profilestore.ConflictModeMerge
	if *modeFlag != "" {
		mode = profilestore.ConflictMode(*modeFlag)
	}

	_, logger, closeLog, svc, err := bootstrap(*cliLevel)
	if err != nil {
		return 1
	}
	defer closeLog()
	defer svc.mgr.Close()

	res, err := profilestore.ImportBundle(svc.store, path, mode)
	if err != nil {
		logger.Error("import_failed", "err", err)
		fmt.Fprintf(os.Stderr, "import failed: %v\n", err)
		return 1
	}
	fmt.Printf("Import result: added=%d skipped=%d renamed=%d replaced=%d\n",
		res.Added, res.Skipped, res.Renamed, res.Replaced)
	return 0
}

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

func ensureDefaultCatalog(catalogStore backendcatalog.Store, schemaStore backendcatalog.SchemaStore, _ *backendschema.Manager, fallbackBinary string, logger *slog.Logger) domain.FlagSchema {
	catalog, err := catalogStore.Load()
	if err != nil {
		logger.Error("default_catalog_load_failed", "err", err)
		fmt.Fprintf(os.Stderr, "backend catalog load error: %v\n", err)
		fmt.Fprintln(os.Stderr, "fix catalog.json or remove it to recreate defaults")
		logger.Warn("catalog_remediation_hint",
			"hint", "fix catalog.json or remove it to recreate defaults")
		return domain.BackendValidationSchema{}.ToFlagSchema()
	}
	if len(catalog.Backends) > 0 {
		resolver := backendcatalog.NewResolver(catalogStore, schemaStore, logger)
		if rb, err := resolver.Resolve(domain.Profile{}); err == nil {
			return rb.Schema.ToFlagSchema()
		}
		logger.Warn("default_catalog_schema_missing",
			"hint", "fix catalog.json or remove it to recreate defaults")
		fmt.Fprintf(os.Stderr, "warning: default backend schema missing/invalid, using fallback\n")
		return domain.BackendValidationSchema{}.ToFlagSchema()
	}
	if fallbackBinary == "" {
		fallbackBinary = "llama-server"
	}
	catalog = backendcatalog.DefaultCatalog(fallbackBinary)
	if err := catalogStore.Save(catalog); err != nil {
		logger.Error("default_catalog_save_failed", "err", err)
		fmt.Fprintf(os.Stderr, "save default catalog: %v\n", err)
		return domain.BackendValidationSchema{}.ToFlagSchema()
	}
	backend := catalog.Backends[0]
	_ = backendschema.WriteEmbeddedFallback(schemaStore, backend.ID, backend.SchemaRef)
	resolver := backendcatalog.NewResolver(catalogStore, schemaStore, logger)
	if rb, err := resolver.Resolve(domain.Profile{}); err == nil {
		return rb.Schema.ToFlagSchema()
	}
	return domain.BackendValidationSchema{}.ToFlagSchema()
}

func buildResolver(resolver backendcatalog.Resolver) func(domain.Profile) (string, error) {
	return func(p domain.Profile) (string, error) {
		rb, err := resolver.Resolve(p)
		if err != nil {
			return "", err
		}
		return rb.ExecutablePath, nil
	}
}

func parseTab(name string) ui.Tab {
	switch name {
	case "launcher":
		return ui.TabLauncher
	case "profiles":
		return ui.TabProfiles
	case "server":
		return ui.TabServer
	case "models":
		return ui.TabModels
	case "backends":
		return ui.TabBackends
	default:
		return ui.TabLauncher
	}
}
