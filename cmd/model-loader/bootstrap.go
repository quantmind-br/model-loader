package main

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

// bootServices is the dependency container shared by both runTUI and
// runServe entrypoints. Owners of the returned values must call
// closeLog and svc.mgr.Close() in defer order (logger last).
type bootServices struct {
	store         profilestore.Store
	catalogStore  backendcatalog.Store
	schemaStore   backendcatalog.SchemaStore
	schemaManager *backendschema.Manager
	resolver      backendcatalog.Resolver
	defaultSchema domain.FlagSchema
	mgr           processmgr.Manager
	val           validator.Validator
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

// bootstrap loads config, builds the logger, wires every service and
// performs initial migrations + reconcile. Returns the populated container
// or an error message routed to stderr.
//
// closeLog is non-nil only when the logger was successfully constructed —
// the caller MUST defer it to ensure the log file is flushed.
func bootstrap(cliLevel string) (cfg config.AppConfig, logger *slog.Logger, closeLog func(), svc *bootServices, err error) {
	cfg, err = config.Load()
	if err != nil {
		// Chicken-and-egg: logger not yet built, so the config-load error
		// can only go to stderr. This is the documented exception.
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return cfg, nil, nil, nil, err
	}

	level := log.ResolveLevel(cliLevel, os.Getenv("MODEL_LOADER_LOG_LEVEL"), cfg.Logging.Level)
	logger, closeLog, err = log.New(log.Config{
		Dir:   cfg.Paths.LogDir,
		Level: level,
	})
	if err != nil {
		// Hard-fail per plan §6 — the debug logging system MUST be available
		// once boot has progressed past config.Load. Silent fallback to Nop()
		// would defeat the whole point of the feature.
		fmt.Fprintf(os.Stderr, "log init error: %v\n", err)
		return cfg, nil, nil, nil, err
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
		return cfg, nil, nil, nil, err
	}

	catalogStore := backendcatalog.NewFSStore(cfg.Paths.BackendsDir)
	schemaStore := backendcatalog.NewFSSchemaStore(cfg.Paths.BackendsDir)

	schemaManager := backendschema.NewManager(catalogStore, schemaStore)
	schemaManager.Register(domain.BackendKindLlamaServer, backendschema.NewLlamaServerGenerator(schemaStore))
	schemaManager.Register(domain.BackendKindSGLang, backendschema.NewSGLangGenerator(schemaStore))
	schemaManager.Register(domain.BackendKindVLLM, backendschema.NewVLLMGenerator(schemaStore))

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

	val := validator.New(logger)

	svc = &bootServices{
		store:         store,
		catalogStore:  catalogStore,
		schemaStore:   schemaStore,
		schemaManager: schemaManager,
		resolver:      resolver,
		defaultSchema: defaultSchema,
		mgr:           mgr,
		val:           val,
	}
	helper.store = store
	helper.mgr = mgr
	return cfg, logger, closeLog, svc, nil
}
