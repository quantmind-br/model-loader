package backendcatalog

import (
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

const defaultBackendID = "llama-cpp-default"

// DefaultCatalog creates a catalog with one default llama-server backend.
func DefaultCatalog(executable string) domain.BackendCatalog {
	now := time.Now().UTC()
	return domain.BackendCatalog{
		SchemaVersion:    catalogSchemaVersion,
		DefaultBackendID: defaultBackendID,
		Backends: []domain.Backend{
			{
				ID:         defaultBackendID,
				Name:       "llama.cpp default",
				Kind:       domain.BackendKindLlamaServer,
				Executable: executable,
				SchemaRef:  "schemas/llama-cpp-default.json",
				Meta: domain.BackendMeta{
					CreatedAt: now,
					UpdatedAt: now,
				},
			},
		},
	}
}
