package configweb

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestCustomizeModeRendersFlagEditors(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags:        map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{{Name: "Essentials", Highlighted: true, Flags: []string{"ctx-size"}}}},
		Rules:        []domain.CrossFieldRule{{ID: "r1", When: domain.Cond{Flag: "ctx-size", Op: "ge", Value: "1"}, Then: domain.Effect{Kind: "message", Message: "hi"}, Severity: "warning"}},
	}
	s := &Session{deps: Deps{Schemas: stubSchemaStore{schema: schema}, Catalog: stubCatalog{id: "llama", ref: "llama.json"}, InitialDraft: Draft{BackendID: "llama"}}}
	rec := httptest.NewRecorder()
	s.handleIndex(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "/customize/flag") {
		t.Fatalf("flag editor form action missing")
	}
	if !strings.Contains(body, "Cross-field rules") {
		t.Fatalf("rules section missing")
	}
}

func TestIndexRendersGroupedFields(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt, Default: 8192}},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{
			{Name: "Essentials", Highlighted: true, Flags: []string{"ctx-size"}},
		}},
	}
	s := &Session{deps: Deps{
		Schemas:      stubSchemaStore{schema: schema},
		Catalog:      stubCatalog{id: "llama", ref: "llama.json"},
		InitialDraft: Draft{ID: "qwen", Name: "Qwen", BackendID: "llama", Args: map[string]string{"ctx-size": "4096"}},
	}}
	rec := httptest.NewRecorder()
	s.handleIndex(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `name="arg.ctx-size"`) || !strings.Contains(body, `value="4096"`) {
		t.Fatalf("ctx-size field not rendered with value: %s", body)
	}
	if !strings.Contains(body, "Essentials") {
		t.Fatalf("group title missing")
	}
}
