package backendcatalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
)

const catalogSchemaVersion = 1

// fsStore persists catalog.json under backendsDir.
type fsStore struct {
	backendsDir string
}

// NewFSStore returns a filesystem-backed backend catalog store.
func NewFSStore(backendsDir string) *fsStore {
	return &fsStore{backendsDir: backendsDir}
}

func (s *fsStore) catalogPath() string {
	return filepath.Join(s.backendsDir, "catalog.json")
}

// Load reads catalog.json. Missing file returns empty schema-versioned catalog.
func (s *fsStore) Load() (domain.BackendCatalog, error) {
	data, err := os.ReadFile(s.catalogPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return domain.BackendCatalog{SchemaVersion: catalogSchemaVersion}, nil
		}
		return domain.BackendCatalog{}, fmt.Errorf("read backend catalog: %w", err)
	}

	var catalog domain.BackendCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return domain.BackendCatalog{}, fmt.Errorf("unmarshal backend catalog: %w", err)
	}
	if catalog.SchemaVersion == 0 {
		catalog.SchemaVersion = catalogSchemaVersion
	}
	return catalog, nil
}

// Save writes catalog.json atomically.
func (s *fsStore) Save(catalog domain.BackendCatalog) error {
	if catalog.SchemaVersion == 0 {
		catalog.SchemaVersion = catalogSchemaVersion
	}
	if catalog.Backends == nil {
		catalog.Backends = []domain.Backend{}
	}
	return writeJSONAtomic(s.catalogPath(), catalog)
}

// fsSchemaStore persists schemas under backendsDir/schemas.
type fsSchemaStore struct {
	backendsDir string
}

// NewFSSchemaStore returns a filesystem-backed backend schema store.
func NewFSSchemaStore(backendsDir string) *fsSchemaStore {
	return &fsSchemaStore{backendsDir: backendsDir}
}

func (s *fsSchemaStore) schemasDir() string {
	return filepath.Join(s.backendsDir, "schemas")
}

func (s *fsSchemaStore) schemaPath(ref string) (string, error) {
	clean := filepath.Clean(ref)
	if ref == "" || filepath.IsAbs(ref) || clean == "." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", ErrInvalidSchemaRef
	}
	path := filepath.Join(s.schemasDir(), clean)
	rel, err := filepath.Rel(s.schemasDir(), path)
	if err != nil {
		return "", fmt.Errorf("resolve schema ref: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", ErrInvalidSchemaRef
	}
	return path, nil
}

// Load reads a backend validation schema by relative ref.
func (s *fsSchemaStore) Load(ref string) (domain.BackendValidationSchema, error) {
	path, err := s.schemaPath(ref)
	if err != nil {
		return domain.BackendValidationSchema{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return domain.BackendValidationSchema{}, ErrSchemaNotFound
		}
		return domain.BackendValidationSchema{}, fmt.Errorf("read backend schema: %w", err)
	}
	var schema domain.BackendValidationSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("unmarshal backend schema: %w", err)
	}
	return schema, nil
}

// Save writes a backend validation schema atomically by relative ref.
func (s *fsSchemaStore) Save(ref string, schema domain.BackendValidationSchema) error {
	path, err := s.schemaPath(ref)
	if err != nil {
		return err
	}
	if schema.SchemaVersion == 0 {
		schema.SchemaVersion = catalogSchemaVersion
	}
	return writeJSONAtomic(path, schema)
}

func writeJSONAtomic(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir backend catalog dir: %w", err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal backend catalog json: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write backend catalog tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename backend catalog tmp: %w", err)
	}
	return nil
}
