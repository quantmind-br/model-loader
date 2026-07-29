package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	backendsDir := cfg.Paths.BackendsDir
	catalogStore := backendcatalog.NewFSStore(backendsDir)
	schemaStore := backendcatalog.NewFSSchemaStore(backendsDir)
	mgr := backendschema.NewManager(catalogStore, schemaStore)

	backendschema.RegisterDefaults(mgr, schemaStore)

	catalog, err := catalogStore.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load catalog: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ok := 0
	fail := 0
	for _, b := range catalog.Backends {
		logger.Info("regenerating", "backend", b.ID, "kind", b.Kind)
		if err := mgr.RefreshSchema(b.ID); err != nil {
			logger.Error("failed", "backend", b.ID, "err", err)
			fail++
			continue
		}
		logger.Info("ok", "backend", b.ID)
		ok++
	}

	// Pattern-C generators emit Presentation: nil, so a regeneration leaves those
	// schemas without a layout until the next app bootstrap. Reseed here, exactly
	// as `backend schema refresh` does.
	if n, err := backendschema.EnsurePresentations(catalogStore, schemaStore); err != nil {
		logger.Warn("ensure_presentations_failed", "err", err)
	} else if n > 0 {
		logger.Info("seeded_presentations", "count", n)
	}

	fmt.Printf("\nDone: %d OK, %d FAILED out of %d backends\n", ok, fail, len(catalog.Backends))
	if fail > 0 {
		os.Exit(1)
	}
}
