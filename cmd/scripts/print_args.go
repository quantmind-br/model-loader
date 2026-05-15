package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

func main() {
	cfg, _ := config.Load()
	store, _ := profilestore.NewFSStore(cfg.Paths.ProfilesDir)

	id := "qwen3.6-27b"
	if len(os.Args) > 1 {
		id = os.Args[1]
	}
	p, err := store.Get(id)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	catalogStore := backendcatalog.NewFSStore(cfg.Paths.BackendsDir)
	schemaStore := backendcatalog.NewFSSchemaStore(cfg.Paths.BackendsDir)
	resolver := backendcatalog.NewResolver(catalogStore, schemaStore, log.Nop())
	rb, err := resolver.Resolve(p)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	args, err := processmgr.BuildArgsForBackend(p, rb.Backend.Kind, rb.ExecutablePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println(rb.ExecutablePath)
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			fmt.Println()
		}
		fmt.Print(a, " ")
	}
	fmt.Println()
}
