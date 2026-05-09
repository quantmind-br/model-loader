// Command llama-cpp-loader launches the TUI for managing LLM server profiles.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/llama-cpp-loader/internal/config"
	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/backendschema"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/migration"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/modelscanner"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/monitor"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/processmgr"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/profilestore"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/validator"
	"github.com/quantmind-br/llama-cpp-loader/internal/ui"
	"github.com/quantmind-br/llama-cpp-loader/internal/ui/pages"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	store, err := profilestore.NewFSStore(cfg.Paths.ProfilesDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile store: %v\n", err)
		os.Exit(1)
	}

	catalogStore := backendcatalog.NewFSStore(cfg.Paths.BackendsDir)
	schemaStore := backendcatalog.NewFSSchemaStore(cfg.Paths.BackendsDir)

	schemaManager := backendschema.NewManager(catalogStore, schemaStore)
	schemaManager.Register(domain.BackendKindLlamaServer, backendschema.NewLlamaServerGenerator(schemaStore))

	migrator := migration.NewService(cfg, store, catalogStore, schemaStore, schemaManager)
	migReport, err := migrator.Run(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "migration: %v\n", err)
	}
	for _, w := range migReport.Warnings {
		fmt.Fprintf(os.Stderr, "migration warning: %s\n", w)
	}

	resolver := backendcatalog.NewResolver(catalogStore, schemaStore)

	defaultSchema := ensureDefaultCatalog(catalogStore, schemaStore, schemaManager, cfg.Paths.LlamaServerBinaryPath)

	mgr := processmgr.New(processmgr.Config{
		Resolver:     buildResolver(resolver),
		LogDir:       cfg.Paths.LogDir,
		RegistryPath: filepath.Join(cfg.Paths.StateDir, "instances.json"),
		LastUsedSink: store,
	})
	defer mgr.Close()
	if err := mgr.Reconcile(); err != nil {
		fmt.Fprintf(os.Stderr, "instance recovery: %v\n", err)
	}

	scanner := modelscanner.New()
	val := validator.New()

	profilesPage := pages.NewProfilesPage(store, defaultSchema).
		WithModelScanner(scanner, cfg.Models.SearchPaths).
		WithBackendCatalog(catalogStore, schemaStore).
		WithBackendManager(schemaManager)
	modelsPage := pages.NewModelsPage(scanner, cfg.Models.SearchPaths).WithProfileStore(store)
	launcherPage := pages.NewLauncherPage(store, mgr, val).
		SetBackendResolver(resolver)

	mon := monitor.New(monitor.Config{NvidiaSMIPath: "nvidia-smi"})
	monitorPage := pages.NewMonitorPage(mgr, mon, store)

	root := ui.NewRoot(parseTab(cfg.UI.DefaultTab)).
		WithProfilesPage(profilesPage).
		WithModelsPage(modelsPage).
		WithLauncherPage(launcherPage).
		WithMonitorPage(monitorPage)

	prog := tea.NewProgram(root, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
	if running := mgr.List(); len(running) > 0 {
		fmt.Fprintf(os.Stderr, "%d background instance(s) still running:\n", len(running))
		for _, ri := range running {
			fmt.Fprintf(os.Stderr, "  PID %d (port %d)\n", ri.PID, ri.Port)
		}
		fmt.Fprintln(os.Stderr, "Restart the TUI to manage them.")
	}
}

func ensureDefaultCatalog(catalogStore backendcatalog.Store, schemaStore backendcatalog.SchemaStore, _ *backendschema.Manager, fallbackBinary string) domain.FlagSchema {
	catalog, err := catalogStore.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "backend catalog load error: %v\n", err)
		fmt.Fprintln(os.Stderr, "fix catalog.json or remove it to recreate defaults")
		return domain.BackendValidationSchema{}.ToFlagSchema()
	}
	if len(catalog.Backends) > 0 {
		resolver := backendcatalog.NewResolver(catalogStore, schemaStore)
		if rb, err := resolver.Resolve(domain.Profile{}); err == nil {
			return rb.Schema.ToFlagSchema()
		}
		fmt.Fprintf(os.Stderr, "warning: default backend schema missing/invalid, using fallback\n")
		return domain.BackendValidationSchema{}.ToFlagSchema()
	}
	if fallbackBinary == "" {
		fallbackBinary = "llama-server"
	}
	catalog = backendcatalog.DefaultCatalog(fallbackBinary)
	if err := catalogStore.Save(catalog); err != nil {
		fmt.Fprintf(os.Stderr, "save default catalog: %v\n", err)
		return domain.BackendValidationSchema{}.ToFlagSchema()
	}
	backend := catalog.Backends[0]
	_ = backendschema.WriteEmbeddedFallback(schemaStore, backend.ID, backend.SchemaRef)
	resolver := backendcatalog.NewResolver(catalogStore, schemaStore)
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
	case "monitor":
		return ui.TabMonitor
	case "models":
		return ui.TabModels
	default:
		return ui.TabProfiles
	}
}
