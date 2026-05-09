// Command llama-cpp-loader launches the TUI for managing llama.cpp profiles.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/llama-cpp-loader/internal/config"
	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/llamabin"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/llamahelp"
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

	// Resolve and validate the global binary (config > PATH default).
	globalBinary := cfg.Paths.LlamaServerBinaryPath
	if globalBinary == "" {
		globalBinary = llamabin.DefaultName
	}
	if _, err := llamabin.Resolve(globalBinary); err != nil {
		fmt.Fprintf(os.Stderr, "config error: invalid llama-server binary %q: %v\n", globalBinary, err)
		os.Exit(1)
	}

	schemaCache := llamahelp.NewSchemaCache(5 * time.Second)
	schema, schemaWarn := loadSchema(globalBinary, schemaCache)
	scanner := modelscanner.New()

	mgr, err := processmgr.NewWithCheck(processmgr.Config{
		Binary:       globalBinary,
		LogDir:       cfg.Paths.LogDir,
		RegistryPath: filepath.Join(cfg.Paths.StateDir, "instances.json"),
		LastUsedSink: store,
	})
	if err != nil {
		if errors.Is(err, processmgr.ErrBinaryNotFound) {
			root := ui.NewRoot(ui.TabProfiles).WithBootBlocker(
				"llama-server not found in PATH",
				"Install llama.cpp first:\n  Arch: pacman -S llama.cpp-cuda\n  Other distros: build from https://github.com/ggml-org/llama.cpp",
			)
			if _, runErr := tea.NewProgram(root, tea.WithAltScreen()).Run(); runErr != nil {
				fmt.Fprintf(os.Stderr, "tui error: %v\n", runErr)
			}
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "process manager: %v\n", err)
		os.Exit(1)
	}
	defer mgr.Close()
	if err := mgr.Reconcile(); err != nil {
		fmt.Fprintf(os.Stderr, "instance recovery: %v\n", err)
	}

	val := validator.New()

	profilesPage := pages.NewProfilesPage(store, schema).
		WithModelScanner(scanner, cfg.Models.SearchPaths)
	modelsPage := pages.NewModelsPage(scanner, cfg.Models.SearchPaths).WithProfileStore(store)
	launcherPage := pages.NewLauncherPage(store, mgr, val).SetSchema(schema).SetBinaryResolver(globalBinary, schemaCache)

	mon := monitor.New(monitor.Config{NvidiaSMIPath: "nvidia-smi"})
	monitorPage := pages.NewMonitorPage(mgr, mon, store)

	root := ui.NewRoot(parseTab(cfg.UI.DefaultTab)).
		WithProfilesPage(profilesPage).
		WithModelsPage(modelsPage).
		WithLauncherPage(launcherPage).
		WithMonitorPage(monitorPage)
	if schemaWarn != "" {
		root = root.WithStatusWarn(schemaWarn)
	}

	// Background llama-server processes intentionally survive TUI exit;
	// processmgr.Reconcile restores them at next boot from instances.json.
	prog := tea.NewProgram(root, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
	if running := mgr.List(); len(running) > 0 {
		fmt.Fprintf(os.Stderr, "%d background llama-server instance(s) still running:\n", len(running))
		for _, ri := range running {
			fmt.Fprintf(os.Stderr, "  PID %d (port %d)\n", ri.PID, ri.Port)
		}
		fmt.Fprintln(os.Stderr, "Restart the TUI to manage them.")
	}
}

// loadSchema attempts to parse the binary's --help via the schema cache.
// On failure (binary absent, timeout, parse error) it returns the embedded
// fallback and a warning string suitable for the status bar.
func loadSchema(binary string, cache *llamahelp.SchemaCache) (domain.FlagSchema, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	schema, err := cache.Get(ctx, binary)
	if err != nil {
		return llamahelp.EmbeddedSchema(), fmt.Sprintf("schema fallback: %v", err)
	}
	return schema, ""
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
