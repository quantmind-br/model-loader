// Package app wires every service the model-loader needs and is the single
// bootstrap reused by the TUI, the CLI, and the headless serve/benchmark paths.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/service/metricsstore"
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

// BootstrapOption tunes Bootstrap.
type BootstrapOption func(*bootstrapOpts)

type bootstrapOpts struct{ stateOwner bool }

// AsStateOwner marks this process as an owner of the shared mutable state:
// it reconciles instances.json (a write), rotates the app log, and prunes
// backend logs/metrics at boot. Owners: the TUI and `serve`. Every other
// entrypoint (one-shot CLI commands) is an observer and MUST NOT pass this —
// a `profile list` racing the TUI/serve must never rewrite the registry
// (audit A2) nor rotate the log file the live session is writing (audit C6).
func AsStateOwner() BootstrapOption { return func(o *bootstrapOpts) { o.stateOwner = true } }

// Bootstrap loads config, builds the logger, wires every service and performs
// initial migrations + reconcile. On error the message is routed to stderr and
// any partially-built logger is closed before returning.
func Bootstrap(cliLevel string, opts ...BootstrapOption) (*Services, error) {
	var o bootstrapOpts
	for _, opt := range opts {
		opt(&o)
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return nil, err
	}

	level := log.ResolveLevel(cliLevel, os.Getenv("MODEL_LOADER_LOG_LEVEL"), cfg.Logging.Level)
	logger, closeLog, err := log.New(log.Config{Dir: cfg.Paths.LogDir, Level: level, Rotate: o.stateOwner})
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
	// Only the state owner (TUI / serve) reconciles — a write. Observers
	// (one-shot CLI) leave the registry to the owner and read via List, which
	// already identity-filters disk entries (audit A2).
	if o.stateOwner {
		if rErr := mgr.Reconcile(); rErr != nil {
			logger.Error("reconcile_failed", "err", rErr)
			fmt.Fprintf(os.Stderr, "instance recovery: %v\n", rErr)
		}
	}

	helper.store = store
	helper.mgr = mgr

	// Owner-only boot hygiene (audit C1/C2): prune stale backend logs and
	// compact per-profile metrics off the boot path so it never blocks startup.
	if o.stateOwner {
		go func() {
			exempt := map[string]struct{}{}
			for _, ri := range mgr.List() {
				exempt[filepath.Base(ri.LogPath)] = struct{}{}
			}
			for _, ei := range mgr.History() {
				exempt[filepath.Base(ei.LogPath)] = struct{}{}
			}
			n := processmgr.PruneBackendLogs(cfg.Paths.LogDir, exempt, 10, logger)
			logger.Info("backend_logs_pruned", "removed", n)

			metricsDir := filepath.Join(cfg.Paths.StateDir, "metrics")
			entries, _ := os.ReadDir(metricsDir)
			for _, e := range entries {
				if id, ok := strings.CutSuffix(e.Name(), ".jsonl"); ok {
					if err := metricsstore.Compact(metricsDir, id, 7*24*time.Hour, 4<<20); err != nil {
						logger.Warn("metrics_compact_failed", "profile_id", id, "err", err)
					}
				}
			}

			harnessDir := filepath.Join(cfg.Paths.StateDir, "benchmark", "harness")
			if hn := benchmark.PruneHarnessLogs(harnessDir, 10, logger); hn > 0 {
				logger.Info("harness_logs_pruned", "removed", hn)
			}
		}()
	}

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

// ensureDefaultCatalog ensures a default backend catalog exists and returns the
// default FlagSchema.
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
		if schema, _, err := resolver.ResolveSchema(domain.Profile{}); err == nil {
			return schema.ToFlagSchema()
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
	if schema, _, err := resolver.ResolveSchema(domain.Profile{}); err == nil {
		return schema.ToFlagSchema()
	}
	return domain.BackendValidationSchema{}.ToFlagSchema()
}

// buildResolver returns a resolver that maps a profile to its binary path and
// backend kind.
func buildResolver(resolver backendcatalog.Resolver) func(domain.Profile) (string, domain.BackendKind, error) {
	return func(p domain.Profile) (string, domain.BackendKind, error) {
		rb, err := resolver.Resolve(p)
		if err != nil {
			return "", "", err
		}
		return rb.ExecutablePath, rb.Backend.Kind, nil
	}
}
