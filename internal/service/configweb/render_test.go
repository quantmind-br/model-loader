package configweb

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestIndexRendersGroupedFields(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt, Default: 8192}},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{
			{Name: "Essenciais", Highlighted: true, Flags: []string{"ctx-size"}},
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
	if !strings.Contains(body, "Essenciais") {
		t.Fatalf("group title missing")
	}
}
