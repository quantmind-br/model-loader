// Command model-loader launches the TUI for managing LLM server profiles.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/quantmind-br/model-loader/internal/service/migration"
	"github.com/quantmind-br/model-loader/internal/service/modelscanner"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/service/validator"
	"github.com/quantmind-br/model-loader/internal/ui"
	"github.com/quantmind-br/model-loader/internal/ui/pages"
)

func main() {
	cliLevel := flag.String("log-level", "", "override log level (debug|info|warn|error); also reads $MODEL_LOADER_LOG_LEVEL and config logging.level")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		// Chicken-and-egg: logger not yet built, so the config-load error
		// can only go to stderr. This is the documented exception.
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	// Build the file-sink logger. Precedence (highest first):
	//   1. --log-level CLI flag
	//   2. $MODEL_LOADER_LOG_LEVEL env var
	//   3. cfg.Logging.Level (config TOML)
	//   4. "info" default
	level := log.ResolveLevel(*cliLevel, os.Getenv("MODEL_LOADER_LOG_LEVEL"), cfg.Logging.Level)
	logger, closeLog, err := log.New(log.Config{
		Dir:   cfg.Paths.LogDir,
		Level: level,
	})
	if err != nil {
		// Hard-fail per plan §6 — the debug logging system MUST be available
		// once boot has progressed past config.Load. Silent fallback to Nop()
		// would defeat the whole point of the feature.
		fmt.Fprintf(os.Stderr, "log init error: %v\n", err)
		os.Exit(1)
	}
	defer closeLog()
	logger.Info("app_start",
		"log_dir", cfg.Paths.LogDir,
		"state_dir", cfg.Paths.StateDir,
		"level", level.String())

	store, err := profilestore.NewFSStore(cfg.Paths.ProfilesDir)
	if err != nil {
		logger.Error("boot_failed", "step", "profile_store", "err", err)
		fmt.Fprintf(os.Stderr, "profile store: %v\n", err)
		os.Exit(1)
	}

	catalogStore := backendcatalog.NewFSStore(cfg.Paths.BackendsDir)
	schemaStore := backendcatalog.NewFSSchemaStore(cfg.Paths.BackendsDir)

	schemaManager := backendschema.NewManager(catalogStore, schemaStore)
	schemaManager.Register(domain.BackendKindLlamaServer, backendschema.NewLlamaServerGenerator(schemaStore))
	schemaManager.Register(domain.BackendKindSGLang, backendschema.NewSGLangGenerator(schemaStore))
	schemaManager.Register(domain.BackendKindVLLM, backendschema.NewVLLMGenerator(schemaStore))

	migrator := migration.NewService(cfg, store, catalogStore, schemaStore, schemaManager)
	migReport, err := migrator.Run(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "migration: %v\n", err)
		logger.Error("migration_failed", "err", err)
	}
	for _, w := range migReport.Warnings {
		fmt.Fprintf(os.Stderr, "migration warning: %s\n", w)
		logger.Warn("migration_warning", "msg", w)
	}

	resolver := backendcatalog.NewResolver(catalogStore, schemaStore, logger)

	defaultSchema := ensureDefaultCatalog(catalogStore, schemaStore, schemaManager, cfg.Paths.LlamaServerBinaryPath, logger)

	mgr := processmgr.New(processmgr.Config{
		Resolver:     buildResolver(resolver),
		LogDir:       cfg.Paths.LogDir,
		RegistryPath: filepath.Join(cfg.Paths.StateDir, "instances.json"),
		LastUsedSink: store,
		Logger:       logger,
	})
	defer mgr.Close()
	if err := mgr.Reconcile(); err != nil {
		logger.Error("reconcile_failed", "err", err)
		fmt.Fprintf(os.Stderr, "instance recovery: %v\n", err)
	}

	scanner := modelscanner.New()
	val := validator.New(logger)

	profilesPage := pages.NewProfilesPage(store, defaultSchema).
		WithModelScanner(scanner, cfg.Models.SearchPaths).
		WithBackendCatalog(catalogStore, schemaStore)
	modelsPage := pages.NewModelsPage(scanner, cfg.Models.SearchPaths).WithProfileStore(store)
	launcherPage := pages.NewLauncherPage(store, mgr, val).
		SetBackendResolver(resolver).
		WithLogger(logger)

	mon := monitor.New(monitor.Config{NvidiaSMIPath: "nvidia-smi"})
	monitorPage := pages.NewMonitorPage(mgr, mon, store).
		SetBackendResolver(resolver)
	backendsPage := pages.NewBackendsPage(schemaManager)

	root := ui.NewRoot(parseTab(cfg.UI.DefaultTab)).
		WithProfilesPage(profilesPage).
		WithModelsPage(modelsPage).
		WithLauncherPage(launcherPage).
		WithMonitorPage(monitorPage).
		WithBackendsPage(backendsPage)

	prog := tea.NewProgram(root, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		logger.Error("tui_error", "err", err)
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
	logger.Info("app_exit", "residual_instances", len(mgr.List()))
	if running := mgr.List(); len(running) > 0 {
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
	case "monitor":
		return ui.TabMonitor
	case "models":
		return ui.TabModels
	case "backends":
		return ui.TabBackends
	default:
		return ui.TabLauncher
	}
}
