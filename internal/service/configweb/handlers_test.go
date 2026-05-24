package configweb

import (
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
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

// --- memProfileStore ---

type memProfileStore struct{ m map[string]domain.Profile }

func newMemProfileStore() *memProfileStore { return &memProfileStore{m: map[string]domain.Profile{}} }

func (s *memProfileStore) List() ([]domain.Profile, error) {
	out := make([]domain.Profile, 0, len(s.m))
	for _, p := range s.m {
		out = append(out, p)
	}
	return out, nil
}
func (s *memProfileStore) ListWithDiagnostics() ([]domain.Profile, []profilestore.ListDiagnostic, error) {
	ps, _ := s.List()
	return ps, nil, nil
}
func (s *memProfileStore) Get(id string) (domain.Profile, error) {
	if p, ok := s.m[id]; ok {
		return p, nil
	}
	return domain.Profile{}, profilestore.ErrNotFound
}
func (s *memProfileStore) Create(p domain.Profile) error {
	if _, ok := s.m[p.ID]; ok {
		return profilestore.ErrDuplicateID
	}
	s.m[p.ID] = p
	return nil
}
func (s *memProfileStore) Save(p domain.Profile) error { s.m[p.ID] = p; return nil }
func (s *memProfileStore) Delete(id string) error      { delete(s.m, id); return nil }
func (s *memProfileStore) Duplicate(srcID, newID string) (domain.Profile, error) {
	return domain.Profile{}, nil
}
func (s *memProfileStore) Rename(oldID string, p domain.Profile) error {
	if oldID != p.ID {
		if _, ok := s.m[p.ID]; ok {
			return profilestore.ErrDuplicateID
		}
		if _, ok := s.m[oldID]; !ok {
			return profilestore.ErrNotFound
		}
		delete(s.m, oldID)
	}
	s.m[p.ID] = p
	return nil
}

func TestSaveHandler_PersistsAndCompletes(t *testing.T) {
	// Create a real model file so the existence validator passes.
	modelPath := t.TempDir() + "/m.gguf"
	if err := os.WriteFile(modelPath, []byte("gguf"), 0o644); err != nil {
		t.Fatal(err)
	}
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	ps := newMemProfileStore()
	s := &Session{
		deps: Deps{Profiles: ps, Schemas: stubSchemaStore{schema: schema}, Catalog: stubCatalog{id: "llama", ref: "llama.json"}},
		done: make(chan Result, 1),
	}
	form := url.Values{
		"isNew": {"true"}, "id": {"qwen"}, "name": {"Qwen"},
		"backendId": {"llama"}, "model": {modelPath}, "arg.ctx-size": {"8192"},
	}
	req := httptest.NewRequest("POST", "/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSave(rec, req)
	res := <-s.Done()
	if !res.Saved || res.ProfileID != "qwen" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if _, err := ps.Get("qwen"); err != nil {
		t.Fatalf("profile not persisted: %v", err)
	}
}

func TestSaveHandler_RenamesProfileOnIDChange(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	ps := newMemProfileStore()
	ps.m["old-id"] = domain.Profile{
		ID: "old-id", Name: "Old", Model: "/m.gguf",
		Launch: domain.LaunchConfig{BackendID: "llama"},
	}
	s := &Session{
		deps: Deps{Profiles: ps, Schemas: stubSchemaStore{schema: schema}, Catalog: stubCatalog{id: "llama", ref: "llama.json"}},
		done: make(chan Result, 1),
	}
	form := url.Values{
		"isNew": {"false"}, "id": {"new-id"}, "origId": {"old-id"},
		"name": {"Old"}, "backendId": {"llama"}, "model": {"/m.gguf"},
	}
	req := httptest.NewRequest("POST", "/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSave(rec, req)
	res := <-s.Done()

	if !res.Saved || res.ProfileID != "new-id" {
		t.Fatalf("expected save under new-id, got: %+v", res)
	}
	if _, err := ps.Get("new-id"); err != nil {
		t.Fatalf("renamed profile not persisted under new id: %v", err)
	}
	if _, err := ps.Get("old-id"); err == nil {
		t.Fatal("old id should no longer exist after rename")
	}
}

func TestConfigureRendersSingleIDInput(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	s := &Session{deps: Deps{
		Schemas:      stubSchemaStore{schema: schema},
		Catalog:      stubCatalog{id: "llama", ref: "llama.json"},
		InitialDraft: Draft{ID: "old-id", OrigID: "old-id", Name: "Old", BackendID: "llama"},
	}}
	rec := httptest.NewRecorder()
	s.handleIndex(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	if n := strings.Count(body, `name="id"`); n != 1 {
		t.Fatalf("expected exactly one id input, got %d:\n%s", n, body)
	}
}

// --- captureSchemaStore ---

type captureSchemaStore struct {
	schema domain.BackendValidationSchema
	saved  domain.BackendValidationSchema
}

func (c *captureSchemaStore) Load(ref string) (domain.BackendValidationSchema, error) {
	return c.schema, nil
}
func (c *captureSchemaStore) Save(ref string, sch domain.BackendValidationSchema) error {
	c.saved = sch
	return nil
}
func (c *captureSchemaStore) Delete(ref string) error { return nil }

func TestCustomize_SaveFlagConstraintMarksEditable(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	store := &captureSchemaStore{schema: schema}
	s := &Session{deps: Deps{Schemas: store, Catalog: stubCatalog{id: "llama", ref: "llama.json"}}, done: make(chan Result, 1)}
	form := url.Values{
		"backendId": {"llama"}, "flag": {"ctx-size"},
		"min": {"512"}, "max": {"131072"}, "default": {"8192"}, "required": {"on"},
	}
	req := httptest.NewRequest("POST", "/customize/flag", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.handleCustomizeFlag(httptest.NewRecorder(), req)

	saved := store.saved
	if saved.Flags["ctx-size"].Min == nil || *saved.Flags["ctx-size"].Min != 512 {
		t.Fatalf("min not saved: %+v", saved.Flags["ctx-size"])
	}
	if !saved.Flags["ctx-size"].Required {
		t.Fatalf("required not saved")
	}
	if !saved.Source.Editable {
		t.Fatalf("schema must be marked Editable after manual edit")
	}
}

func TestCustomize_RulesRejectsUnknownFlag(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	store := &captureSchemaStore{schema: schema}
	s := &Session{deps: Deps{Schemas: store, Catalog: stubCatalog{id: "llama", ref: "llama.json"}}, done: make(chan Result, 1)}
	body := `[{"id":"r1","when":{"flag":"nonexistent","op":"eq","value":"x"},"then":{"kind":"message","message":"hi"},"severity":"warning"}]`
	req := httptest.NewRequest("POST", "/customize/rules?backendId=llama", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleCustomizeRules(rec, req)
	if rec.Code != 400 {
		t.Fatalf("expected 400 for unknown-flag rule, got %d", rec.Code)
	}
}

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

func TestSaveHandler_BlocksOnValidationError(t *testing.T) {
	// Schema has a required flag "port"; form omits it → validator fires SeverityError.
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{
			"port": {Long: "port", Type: domain.FlagTypeInt, Required: true},
		},
	}
	ps := newMemProfileStore()
	s := &Session{
		deps: Deps{Profiles: ps, Schemas: stubSchemaStore{schema: schema}, Catalog: stubCatalog{id: "llama", ref: "llama.json"}},
		done: make(chan Result, 1),
	}
	// "port" is intentionally absent — required flag missing triggers an error.
	form := url.Values{
		"isNew": {"true"}, "id": {"blocked-profile"}, "name": {"Blocked"},
		"backendId": {"llama"}, "model": {"/m.gguf"},
	}
	req := httptest.NewRequest("POST", "/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSave(rec, req)

	// Must NOT redirect.
	if got := rec.Header().Get("HX-Redirect"); got != "" {
		t.Fatalf("expected no HX-Redirect on blocked save, got %q", got)
	}
	// Must NOT persist the profile.
	if _, err := ps.Get("blocked-profile"); err == nil {
		t.Fatal("profile should not have been persisted when validation fails")
	}
	// Must render the #issues partial.
	body := rec.Body.String()
	if !strings.Contains(body, `id="issues"`) {
		t.Fatalf("expected issues partial in body, got: %s", body)
	}
}

func TestSaveHandler_AllowsMissingModelFile(t *testing.T) {
	// Model file does NOT exist — configure now, download later flow.
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	ps := newMemProfileStore()
	s := &Session{
		deps: Deps{Profiles: ps, Schemas: stubSchemaStore{schema: schema}, Catalog: stubCatalog{id: "llama", ref: "llama.json"}},
		done: make(chan Result, 1),
	}
	form := url.Values{
		"isNew": {"true"}, "id": {"qwen"}, "name": {"Qwen"},
		"backendId": {"llama"}, "model": {"/nonexistent/model.gguf"}, "arg.ctx-size": {"4096"},
	}
	req := httptest.NewRequest("POST", "/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSave(rec, req)
	res := <-s.Done()
	if !res.Saved || res.ProfileID != "qwen" {
		t.Fatalf("expected save to succeed with missing model file, got: %+v", res)
	}
	if _, err := ps.Get("qwen"); err != nil {
		t.Fatalf("profile not persisted: %v", err)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "/closed" {
		t.Fatalf("expected HX-Redirect=/closed, got %q", got)
	}
}

func TestDraftFromForm_ParsesEnvVars(t *testing.T) {
	form := url.Values{
		"id":          []string{"test"},
		"name":        []string{"Test"},
		"isNew":       []string{"true"},
		"envKey_0":    []string{"FOO"},
		"envValue_0":  []string{"bar"},
		"envKey_1":    []string{"BAZ"},
		"envValue_1":  []string{"qux"},
		"envKey_2":    []string{""},
		"envValue_2":  []string{"skip"},
	}
	req := httptest.NewRequest("POST", "/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()
	d := draftFromForm(req)
	if len(d.Env) != 2 {
		t.Fatalf("expected 2 env vars, got %d: %+v", len(d.Env), d.Env)
	}
	if d.Env[0].Key != "FOO" || d.Env[0].Value != "bar" {
		t.Fatalf("env[0] wrong: %+v", d.Env[0])
	}
	if d.Env[1].Key != "BAZ" || d.Env[1].Value != "qux" {
		t.Fatalf("env[1] wrong: %+v", d.Env[1])
	}
}

func TestDraftFromForm_NoEnvVars(t *testing.T) {
	form := url.Values{"id": []string{"test"}, "name": []string{"Test"}, "isNew": []string{"true"}}
	req := httptest.NewRequest("POST", "/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()
	d := draftFromForm(req)
	if len(d.Env) != 0 {
		t.Fatalf("expected no env vars, got %d", len(d.Env))
	}
}

func TestDraftFromForm_SkipsWhitespaceKeys(t *testing.T) {
	form := url.Values{
		"id":         []string{"test"},
		"name":       []string{"Test"},
		"envKey_0":   []string{"  "},
		"envValue_0": []string{"bar"},
	}
	req := httptest.NewRequest("POST", "/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()
	d := draftFromForm(req)
	if len(d.Env) != 0 {
		t.Fatalf("expected no env vars for whitespace key, got %d", len(d.Env))
	}
}
