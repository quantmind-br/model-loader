package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/spf13/cobra"
)

var backendCmd = &cobra.Command{
	Use:   "backend",
	Short: "List backends, inspect them, probe binaries, and manage schemas",
}

func init() {
	rootCmd.AddCommand(backendCmd)
}

// backendManager is the narrow slice of *backendschema.Manager the CLI needs.
type backendManager interface {
	ListBackends() ([]domain.Backend, error)
	DefaultBackendID() (string, error)
	RefreshSchema(backendID string) error
	AddBackend(ctx context.Context, name, executable string, kind domain.BackendKind) (domain.Backend, error)
	DeleteBackend(id string) error
	SetDefaultBackend(id string) error
	Generators() map[domain.BackendKind]backendschema.Generator
}

// backendProber is the narrow slice of *backendcatalog.Prober the CLI needs.
type backendProber interface {
	Probe(ctx context.Context) (<-chan backendcatalog.ProbeEvent, error)
}

// buildSchemaManager constructs a fully-registered schema Manager rooted at the
// configured backends dir, sharing generator registration with the app via
// backendschema.RegisterDefaults.
func buildSchemaManager(cfg config.AppConfig) *backendschema.Manager {
	catalogStore := backendcatalog.NewFSStore(cfg.Paths.BackendsDir)
	schemaStore := backendcatalog.NewFSSchemaStore(cfg.Paths.BackendsDir)
	m := backendschema.NewManager(catalogStore, schemaStore)
	backendschema.RegisterDefaults(m, schemaStore)
	return m
}

// buildSchemaStore returns the filesystem schema store for the configured dir.
func buildSchemaStore(cfg config.AppConfig) backendcatalog.SchemaStore {
	return backendcatalog.NewFSSchemaStore(cfg.Paths.BackendsDir)
}

// buildCatalogStore returns the filesystem catalog store for the configured dir.
func buildCatalogStore(cfg config.AppConfig) backendcatalog.Store {
	return backendcatalog.NewFSStore(cfg.Paths.BackendsDir)
}

// buildProber returns a Prober over the configured catalog with a 5s timeout.
func buildProber(cfg config.AppConfig) backendProber {
	store := backendcatalog.NewFSStore(cfg.Paths.BackendsDir)
	return backendcatalog.NewProber(store, backendcatalog.ProbeConfig{Timeout: 5 * time.Second})
}

// resolveBackend matches idOrPrefix against catalog backend IDs: an exact match
// wins; otherwise a unique prefix match is accepted. Mirrors resolveInstance.
func resolveBackend(mgr backendManager, idOrPrefix string) (domain.Backend, error) {
	backends, err := mgr.ListBackends()
	if err != nil {
		return domain.Backend{}, fmt.Errorf("list backends: %w", err)
	}
	for _, b := range backends {
		if b.ID == idOrPrefix {
			return b, nil
		}
	}
	var match domain.Backend
	n := 0
	for _, b := range backends {
		if strings.HasPrefix(b.ID, idOrPrefix) {
			match = b
			n++
		}
	}
	switch n {
	case 0:
		return domain.Backend{}, fmt.Errorf("backend not found: %s", idOrPrefix)
	case 1:
		return match, nil
	default:
		return domain.Backend{}, fmt.Errorf("ambiguous backend id: %s", idOrPrefix)
	}
}
