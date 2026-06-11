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

func TestConfigureRendersDescriptionAndTags(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	s := &Session{deps: Deps{
		Schemas:      stubSchemaStore{schema: schema},
		Catalog:      stubCatalog{id: "llama", ref: "llama.json"},
		InitialDraft: Draft{ID: "p", OrigID: "p", Name: "P", BackendID: "llama", Description: "long desc here", Tags: []string{"alpha", "beta"}},
	}}
	rec := httptest.NewRecorder()
	s.handleIndex(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `name="description"`) {
		t.Fatalf("description field missing")
	}
	if !strings.Contains(body, "long desc here") {
		t.Fatalf("description value not rendered: %s", body)
	}
	if !strings.Contains(body, `name="tags"`) {
		t.Fatalf("tags field missing")
	}
	if !strings.Contains(body, `value="alpha, beta"`) {
		t.Fatalf("tags value not rendered joined: %s", body)
	}
}

func TestConfigureRendersAccessibleLabelsAndTabs(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{
			"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt, Required: true, HelpText: "context window size"},
		},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{
			{Name: "Essentials", Highlighted: true, Flags: []string{"ctx-size"}},
		}},
	}
	s := &Session{deps: Deps{
		Schemas:      stubSchemaStore{schema: schema},
		Catalog:      stubCatalog{id: "llama", ref: "llama.json"},
		InitialDraft: Draft{ID: "p", Name: "P", BackendID: "llama"},
	}}
	rec := httptest.NewRecorder()
	s.handleIndex(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	for _, want := range []string{
		// Top-field label/input wiring.
		`<label for="profile-model">`, `id="profile-model"`,
		// Schema-driven arg field wiring + help description.
		`<label for="arg-ctx-size">`, `id="arg-ctx-size"`,
		`aria-describedby="arg-ctx-size-help"`, `id="arg-ctx-size-help"`,
		// Required semantics.
		`aria-required="true"`,
		// Tablist structure (sidebar groups + mode switcher).
		`role="tablist"`, `role="tab"`, `role="tabpanel"`,
		`aria-orientation="vertical"`,
		`id="group-tab-essentials"`, `id="group-panel-essentials"`,
		`aria-controls="group-panel-essentials"`,
		`aria-labelledby="group-tab-essentials"`,
		`aria-label="Editor mode"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("configure page missing %s", want)
		}
	}
}

func TestBackendPageRendersAccessibleLabels(t *testing.T) {
	s := &Session{deps: Deps{InitialBackendDraft: BackendDraft{IsNew: true}}}
	rec := httptest.NewRecorder()
	s.handleBackendIndex(rec, httptest.NewRequest("GET", "/backend/", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`<label for="backend-name">`, `id="backend-name"`,
		`<label for="backend-kind">`, `id="backend-kind"`,
		`<label for="backend-executable">`, `id="backend-executable"`,
		`<label for="backend-description">`, `id="backend-description"`,
		`<label for="backend-tags">`, `id="backend-tags"`,
		`aria-required="true"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("backend page missing %s", want)
		}
	}
}

func TestIndexRendersIssuesLiveRegion(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	s := &Session{deps: Deps{
		Schemas:      stubSchemaStore{schema: schema},
		Catalog:      stubCatalog{id: "llama", ref: "llama.json"},
		InitialDraft: Draft{ID: "p", Name: "P", BackendID: "llama"},
	}}
	rec := httptest.NewRecorder()
	s.handleIndex(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	for _, want := range []string{`id="issues"`, `role="status"`, `aria-live="polite"`, `aria-atomic="true"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("index page missing %s on the issues live region", want)
		}
	}
}

func TestBackendPageRendersIssuesLiveRegion(t *testing.T) {
	s := &Session{deps: Deps{InitialBackendDraft: BackendDraft{IsNew: true}}}
	rec := httptest.NewRecorder()
	s.handleBackendIndex(rec, httptest.NewRequest("GET", "/backend/", nil))
	body := rec.Body.String()
	for _, want := range []string{`id="issues"`, `role="status"`, `aria-live="polite"`, `aria-atomic="true"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("backend page missing %s on the issues live region", want)
		}
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
