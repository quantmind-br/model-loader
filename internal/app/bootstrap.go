// Package app wires every service the model-loader needs and is the single
// bootstrap reused by the TUI, the CLI, and the headless serve/benchmark paths.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/quantmind-br/model-loader/internal/service/migration"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/service/validator"
)

// Services is the dependency container shared by the TUI, CLI, and headless
// entrypoints. Callers MUST defer Close() to flush the log file and stop the
// process manager.
type Services struct {
	Cfg           config.AppConfig
	Logger        *slog.Logger
	Store         profilestore.Store
	CatalogStore  backendcatalog.Store
	SchemaStore   backendcatalog.SchemaStore
	SchemaManager *backendschema.Manager
	Resolver      backendcatalog.Resolver
	DefaultSchema domain.FlagSchema
	Mgr           processmgr.Manager
	Val           validator.Validator

	closeLog func()
}

// Close stops the process manager and flushes the logger, in that order.
func (s *Services) Close() {
	if s == nil {
		return
	}
	if s.Mgr != nil {
		s.Mgr.Close()
	}
	if s.closeLog != nil {
		s.closeLog()
	}
}

type restartHelper struct {
	store profilestore.Store
	mgr   processmgr.Manager
}

func (r *restartHelper) restart(profileID string) {
	if r.store == nil || r.mgr == nil {
		return
	}
	p, err := r.store.Get(profileID)
	if err != nil {
		return
	}
	_, _ = r.mgr.Launch(p, processmgr.LaunchBackground, "watchdog")
}

// Bootstrap loads config, builds the logger, wires every service and performs
// initial migrations + reconcile. On error the message is routed to stderr and
// any partially-built logger is closed before returning.
func Bootstrap(cliLevel string) (*Services, error) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return nil, err
	}

	level := log.ResolveLevel(cliLevel, os.Getenv("MODEL_LOADER_LOG_LEVEL"), cfg.Logging.Level)
	logger, closeLog, err := log.New(log.Config{Dir: cfg.Paths.LogDir, Level: level})
	if err != nil {
		fmt.Fprintf(os.Stderr, "log init error: %v\n", err)
		return nil, err
	}
	logger.Info("app_start",
		"log_dir", cfg.Paths.LogDir,
		"state_dir", cfg.Paths.StateDir,
		"level", level.String())

	store, err := profilestore.NewFSStore(cfg.Paths.ProfilesDir)
	if err != nil {
		logger.Error("boot_failed", "step", "profile_store", "err", err)
		fmt.Fprintf(os.Stderr, "profile store: %v\n", err)
		closeLog()
		return nil, err
	}

	catalogStore := backendcatalog.NewFSStore(cfg.Paths.BackendsDir)
	schemaStore := backendcatalog.NewFSSchemaStore(cfg.Paths.BackendsDir)

	schemaManager := backendschema.NewManager(catalogStore, schemaStore)
	backendschema.RegisterDefaults(schemaManager, schemaStore)

	migrator := migration.NewService(cfg, store, catalogStore, schemaStore, schemaManager)
	migReport, mErr := migrator.Run(context.Background())
	if mErr != nil {
		fmt.Fprintf(os.Stderr, "migration: %v\n", mErr)
		logger.Error("migration_failed", "err", mErr)
	}
	for _, w := range migReport.Warnings {
		fmt.Fprintf(os.Stderr, "migration warning: %s\n", w)
		logger.Warn("migration_warning", "msg", w)
	}

	resolver := backendcatalog.NewResolver(catalogStore, schemaStore, logger)
	defaultSchema := ensureDefaultCatalog(catalogStore, schemaStore, schemaManager, cfg.Paths.LlamaServerBinaryPath, logger)
	if _, perr := backendschema.EnsurePresentations(catalogStore, schemaStore); perr != nil {
		logger.Warn("ensure_presentations_failed", "err", perr)
	}

	helper := &restartHelper{}
	mgr := processmgr.New(processmgr.Config{
		Resolver:     buildResolver(resolver),
		LogDir:       cfg.Paths.LogDir,
		RegistryPath: filepath.Join(cfg.Paths.StateDir, "instances.json"),
		HistoryPath:  filepath.Join(cfg.Paths.StateDir, "instances-history.json"),
		LastUsedSink: store,
		Logger:       logger,
		RestartFunc:  helper.restart,
	})
	if rErr := mgr.Reconcile(); rErr != nil {
		logger.Error("reconcile_failed", "err", rErr)
		fmt.Fprintf(os.Stderr, "instance recovery: %v\n", rErr)
	}

	helper.store = store
	helper.mgr = mgr

	return &Services{
		Cfg:           cfg,
		Logger:        logger,
		Store:         store,
		CatalogStore:  catalogStore,
		SchemaStore:   schemaStore,
		SchemaManager: schemaManager,
		Resolver:      resolver,
		DefaultSchema: defaultSchema,
		Mgr:           mgr,
		Val:           validator.New(logger),
		closeLog:      closeLog,
	}, nil
}

// ensureDefaultCatalog is copied verbatim from cmd/model-loader/main.go.
// It ensures a default backend catalog exists and returns the default FlagSchema.
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
	schema := backendschema.CuratedLlamaSchema()
	schema.BackendID = backend.ID
	ref := backendcatalog.SchemaStoreRef(backend.SchemaRef)
	_ = schemaStore.Save(ref, schema)
	resolver := backendcatalog.NewResolver(catalogStore, schemaStore, logger)
	if rb, err := resolver.Resolve(domain.Profile{}); err == nil {
		return rb.Schema.ToFlagSchema()
	}
	return domain.BackendValidationSchema{}.ToFlagSchema()
}

// buildResolver is copied verbatim from cmd/model-loader/main.go.
func buildResolver(resolver backendcatalog.Resolver) func(domain.Profile) (string, domain.BackendKind, error) {
	return func(p domain.Profile) (string, domain.BackendKind, error) {
		rb, err := resolver.Resolve(p)
		if err != nil {
			return "", "", err
		}
		return rb.ExecutablePath, rb.Backend.Kind, nil
	}
}
