package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
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

	mgr.Register(domain.BackendKindLlamaServer, backendschema.NewLlamaServerGenerator(schemaStore))
	mgr.Register(domain.BackendKindSGLang, backendschema.NewSGLangGenerator(schemaStore))
	mgr.Register(domain.BackendKindVLLM, backendschema.NewVLLMGenerator(schemaStore))
	mgr.Register(domain.BackendKindDFlash, backendschema.NewDFlashGenerator(schemaStore))
	mgr.Register(domain.BackendKindBuunLlamaCpp, backendschema.NewBuunServerGenerator(schemaStore))

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

	fmt.Printf("\nDone: %d OK, %d FAILED out of %d backends\n", ok, fail, len(catalog.Backends))
	if fail > 0 {
		os.Exit(1)
	}
}
