// Package backendcatalog persists backend catalogs and validation schemas.
package backendcatalog

import (
	"errors"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// Store persists BackendCatalog as JSON.
type Store interface {
	Load() (domain.BackendCatalog, error)
	Save(domain.BackendCatalog) error
}

// SchemaStore persists BackendValidationSchema as JSON.
type SchemaStore interface {
	Load(ref string) (domain.BackendValidationSchema, error)
	Save(ref string, schema domain.BackendValidationSchema) error
}

var (
	// ErrBackendNotFound means requested backend ID is absent from catalog.
	ErrBackendNotFound = errors.New("backend not found")
	// ErrSchemaNotFound means requested schema ref is absent from schema store.
	ErrSchemaNotFound = errors.New("backend schema not found")
	// ErrNoBackendSelected means neither profile nor catalog selected backend.
	ErrNoBackendSelected = errors.New("no backend selected")
	// ErrInvalidSchemaRef means schema ref is empty, absolute, or escapes schema dir.
	ErrInvalidSchemaRef = errors.New("backend schema ref is invalid")
)
