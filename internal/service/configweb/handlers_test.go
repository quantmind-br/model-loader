package configweb

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// --- test doubles ---

type stubSchemaStore struct {
	schema domain.BackendValidationSchema
}

func (s stubSchemaStore) Load(ref string) (domain.BackendValidationSchema, error) {
	return s.schema, nil
}
func (s stubSchemaStore) Save(ref string, sch domain.BackendValidationSchema) error { return nil }
func (s stubSchemaStore) Delete(ref string) error                                   { return nil }

type stubCatalog struct {
	id  string
	ref string
}

func (c stubCatalog) Load() (domain.BackendCatalog, error) {
	return domain.BackendCatalog{
		SchemaVersion:    1,
		DefaultBackendID: c.id,
		Backends:         []domain.Backend{{ID: c.id, Kind: domain.BackendKindLlamaServer, SchemaRef: "schemas/" + c.ref}},
	}, nil
}
func (c stubCatalog) Save(domain.BackendCatalog) error { return nil }

// --- test ---

func TestValidateHandler_ReportsUnknownFlag(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer,
		BackendID:   "llama",
		Flags:       map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	s := &Session{deps: Deps{Schemas: stubSchemaStore{schema: schema}, Catalog: stubCatalog{id: "llama", ref: "llama.json"}}}
	form := url.Values{"backendId": {"llama"}, "arg.bogus": {"1"}}
	req := httptest.NewRequest("POST", "/validate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleValidate(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unknown flag") {
		t.Fatalf("expected unknown-flag issue, got: %s", rec.Body.String())
	}
}
