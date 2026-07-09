package configweb

import (
	"errors"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
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

// failingSchemaStore always fails to load — simulates a corrupt/missing schema.
type failingSchemaStore struct{}

func (failingSchemaStore) Load(ref string) (domain.BackendValidationSchema, error) {
	return domain.BackendValidationSchema{}, errors.New("schema load boom")
}
func (failingSchemaStore) Save(ref string, sch domain.BackendValidationSchema) error { return nil }
func (failingSchemaStore) Delete(ref string) error                                   { return nil }

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

type memProfileStore struct {
	m       map[string]domain.Profile
	renames int
}

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
	s.renames++
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

func TestSaveHandler_RefusesRenameWhenInstanceInUse(t *testing.T) {
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
		deps: Deps{
			Profiles: ps, Schemas: stubSchemaStore{schema: schema},
			Catalog:       stubCatalog{id: "llama", ref: "llama.json"},
			InstanceInUse: func(string) bool { return true },
		},
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

	if ps.renames != 0 {
		t.Fatalf("Rename must not be called when instance is in use, got %d calls", ps.renames)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "" {
		t.Fatalf("refused rename must not redirect, got HX-Redirect=%q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="issue error"`) {
		t.Fatalf("expected issue error in body, got: %s", body)
	}
	if !strings.Contains(body, "old-id") {
		t.Fatalf("issue error should name the offending id, got: %s", body)
	}
	if _, err := ps.Get("old-id"); err != nil {
		t.Fatalf("old-id must remain after refused rename: %v", err)
	}
	if _, err := ps.Get("new-id"); err == nil {
		t.Fatal("new-id must not exist after refused rename")
	}
	select {
	case <-s.Done():
		t.Fatal("session should not complete on a refused rename")
	default:
	}
}

func TestSaveHandler_AllowsRenameWhenNotInUse(t *testing.T) {
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
		deps: Deps{
			Profiles: ps, Schemas: stubSchemaStore{schema: schema},
			Catalog:       stubCatalog{id: "llama", ref: "llama.json"},
			InstanceInUse: func(string) bool { return false },
		},
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

	if ps.renames != 1 {
		t.Fatalf("Rename must be called once when not in use, got %d calls", ps.renames)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "/closed" {
		t.Fatalf("allowed rename must redirect to /closed, got HX-Redirect=%q", got)
	}
	if _, err := ps.Get("new-id"); err != nil {
		t.Fatalf("renamed profile not persisted under new id: %v", err)
	}
	if _, err := ps.Get("old-id"); err == nil {
		t.Fatal("old id should no longer exist after rename")
	}
}

func TestSaveHandler_PreservesDescriptionAndTags(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	ps := newMemProfileStore()
	ps.m["p"] = domain.Profile{
		ID: "p", Name: "P", Model: "/m.gguf",
		Description: "old desc", Tags: []string{"x"},
		Launch: domain.LaunchConfig{BackendID: "llama"},
	}
	s := &Session{
		deps: Deps{Profiles: ps, Schemas: stubSchemaStore{schema: schema}, Catalog: stubCatalog{id: "llama", ref: "llama.json"}},
		done: make(chan Result, 1),
	}
	form := url.Values{
		"isNew": {"false"}, "id": {"p"}, "origId": {"p"},
		"name": {"P"}, "backendId": {"llama"}, "model": {"/m.gguf"},
		"description": {"new description"}, "tags": {"red, green, blue"},
	}
	req := httptest.NewRequest("POST", "/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.handleSave(httptest.NewRecorder(), req)
	<-s.Done()

	got, err := ps.Get("p")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Description != "new description" {
		t.Fatalf("description not saved: %q", got.Description)
	}
	if strings.Join(got.Tags, ",") != "red,green,blue" {
		t.Fatalf("tags not saved: %v", got.Tags)
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

func TestSaveButtonIsNotNativeSubmit(t *testing.T) {
	// The Save button must NOT be a native form-submit button: a submit button
	// associated with #profile-form fires a native GET navigation that reloads
	// the editor (discarding the edit) before HTMX can POST /save. It must be
	// type="button" and pull the form values via hx-include instead.
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

	i := strings.Index(body, `hx-post="/save"`)
	if i < 0 {
		t.Fatalf("save button not found")
	}
	// Inspect the button tag around the hx-post="/save" attribute.
	start := strings.LastIndex(body[:i], "<button")
	end := strings.Index(body[i:], ">")
	if start < 0 || end < 0 {
		t.Fatalf("malformed save button")
	}
	btn := body[start : i+end+1]
	if !strings.Contains(btn, `type="button"`) {
		t.Fatalf("save button must be type=button to avoid native submit: %s", btn)
	}
	if strings.Contains(btn, `form="profile-form"`) {
		t.Fatalf("save button must not be a form-associated submit button: %s", btn)
	}
	if !strings.Contains(btn, `hx-include="#profile-form"`) {
		t.Fatalf("save button must hx-include the form to send its values: %s", btn)
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
	// Must render the issues partial (inner content for the live region).
	body := rec.Body.String()
	if !strings.Contains(body, `class="issue error"`) {
		t.Fatalf("expected issue error in body, got: %s", body)
	}
}

func TestValidateHandler_SchemaLoadFailureRendersIssue(t *testing.T) {
	// htmx ignores non-2xx swap bodies, so a schema-load failure must come back
	// as a 200 issue partial — never http.Error — or #issues silently goes stale.
	s := &Session{deps: Deps{Schemas: failingSchemaStore{}, Catalog: stubCatalog{id: "llama", ref: "llama.json"}}}
	form := url.Values{"backendId": {"llama"}}
	req := httptest.NewRequest("POST", "/validate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleValidate(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200 so htmx swaps the issue into #issues, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="issue error"`) || !strings.Contains(body, "schema load boom") {
		t.Fatalf("expected issue div with the load error, got: %s", body)
	}
}

func TestSaveHandler_SchemaLoadFailureRendersIssue(t *testing.T) {
	s := &Session{
		deps: Deps{Profiles: newMemProfileStore(), Schemas: failingSchemaStore{}, Catalog: stubCatalog{id: "llama", ref: "llama.json"}},
		done: make(chan Result, 1),
	}
	form := url.Values{
		"isNew": {"true"}, "id": {"p"}, "name": {"P"},
		"backendId": {"llama"}, "model": {"/m.gguf"},
	}
	req := httptest.NewRequest("POST", "/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSave(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200 so htmx swaps the issue into #issues, got %d", rec.Code)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "" {
		t.Fatalf("expected no HX-Redirect on failed save, got %q", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="issue error"`) || !strings.Contains(body, "schema load boom") {
		t.Fatalf("expected issue div with the load error, got: %s", body)
	}
}

func TestValidateHandler_PartialOmitsIssuesContainer(t *testing.T) {
	// The /validate partial must be inner content only: the persistent #issues
	// div in the page carries aria-live, and replacing it (oob outerHTML swap)
	// would kill screen-reader announcements.
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	s := &Session{deps: Deps{Schemas: stubSchemaStore{schema: schema}, Catalog: stubCatalog{id: "llama", ref: "llama.json"}}}
	form := url.Values{"backendId": {"llama"}, "model": {"/m.gguf"}, "name": {"P"}, "id": {"p"}}
	req := httptest.NewRequest("POST", "/validate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleValidate(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, `id="issues"`) {
		t.Fatalf("validate partial must not contain the #issues container: %s", body)
	}
	if strings.Contains(body, "hx-swap-oob") {
		t.Fatalf("validate partial must not use an oob swap: %s", body)
	}
}

func TestBackendValidateHandler_PartialOmitsIssuesContainer(t *testing.T) {
	s := &Session{deps: Deps{}}
	form := url.Values{"name": {""}, "executable": {""}}
	req := httptest.NewRequest("POST", "/backend/validate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleBackendValidate(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, `id="issues"`) || strings.Contains(body, "hx-swap-oob") {
		t.Fatalf("backend validate partial must be inner content only: %s", body)
	}
	if !strings.Contains(body, `class="issue error"`) {
		t.Fatalf("expected validation issues for empty backend form: %s", body)
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

// --- backend-switch test doubles ---

// multiBackendCatalog serves a catalog with several backends, each with its
// own schema ref.
type multiBackendCatalog struct{ backends []domain.Backend }

func (c multiBackendCatalog) Load() (domain.BackendCatalog, error) {
	return domain.BackendCatalog{
		SchemaVersion:    1,
		DefaultBackendID: c.backends[0].ID,
		Backends:         c.backends,
	}, nil
}
func (c multiBackendCatalog) Save(domain.BackendCatalog) error { return nil }

// refSchemaStore returns a different schema per store ref.
type refSchemaStore struct {
	schemas map[string]domain.BackendValidationSchema
}

func (s refSchemaStore) Load(ref string) (domain.BackendValidationSchema, error) {
	if sch, ok := s.schemas[ref]; ok {
		return sch, nil
	}
	return domain.BackendValidationSchema{}, errors.New("no schema for ref " + ref)
}
func (s refSchemaStore) Save(string, domain.BackendValidationSchema) error { return nil }
func (s refSchemaStore) Delete(string) error                               { return nil }

// switchTestSession wires two backends: "old" knows ctx-size and old-only,
// "new" knows only ctx-size.
func switchTestSession() *Session {
	oldSchema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "old",
		Flags: map[string]domain.FlagSpec{
			"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt},
			"old-only": {Long: "old-only", Type: domain.FlagTypeString},
		},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{
			{Name: "Essentials", Highlighted: true, Flags: []string{"ctx-size", "old-only"}},
		}},
	}
	newSchema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "new",
		Flags: map[string]domain.FlagSpec{
			"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt},
		},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{
			{Name: "Core", Highlighted: true, Flags: []string{"ctx-size"}},
		}},
	}
	return &Session{deps: Deps{
		Schemas: refSchemaStore{schemas: map[string]domain.BackendValidationSchema{
			"old.json": oldSchema,
			"new.json": newSchema,
		}},
		Catalog: multiBackendCatalog{backends: []domain.Backend{
			{ID: "old", Name: "Old Backend", Kind: domain.BackendKindLlamaServer, SchemaRef: "schemas/old.json"},
			{ID: "new", Name: "New Backend", Kind: domain.BackendKindLlamaServer, SchemaRef: "schemas/new.json"},
		}},
		InitialDraft: Draft{IsNew: true, Name: "P", BackendID: "old"},
	}}
}

func postSwitchBackend(t *testing.T, s *Session, form url.Values) string {
	t.Helper()
	req := httptest.NewRequest("POST", "/switch-backend", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSwitchBackend(rec, req)
	if rec.Code != 200 {
		t.Fatalf("switch-backend status %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func assertInitialDraftUntouched(t *testing.T, s *Session) {
	t.Helper()
	if s.deps.InitialDraft.BackendID != "old" {
		t.Fatalf("handleSwitchBackend must not mutate shared InitialDraft state, got BackendID=%q",
			s.deps.InitialDraft.BackendID)
	}
}

func TestSwitchBackend_CarriesSharedArgsAndRefreshesSidebar(t *testing.T) {
	s := switchTestSession()
	body := postSwitchBackend(t, s, url.Values{
		"isNew": {"true"}, "name": {"P"},
		"backendId": {"new"}, "prevBackendId": {"old"},
		"arg.ctx-size": {"4096"},
	})
	if !strings.Contains(body, `value="4096"`) {
		t.Fatalf("shared arg value must carry over to the new form: %s", body)
	}
	if !strings.Contains(body, `name="prevBackendId" value="new"`) {
		t.Fatalf("new form must record the new backend as prevBackendId: %s", body)
	}
	if strings.Contains(body, "switch-confirm") {
		t.Fatalf("no confirmation expected when nothing is dropped: %s", body)
	}
	if !strings.Contains(body, `id="sidebar-groups" hx-swap-oob="innerHTML"`) {
		t.Fatalf("response must oob-refresh the sidebar tablist: %s", body)
	}
	if !strings.Contains(body, `x-init="activeGroup=&#39;Core&#39;"`) {
		t.Fatalf("oob fragment must reset activeGroup to the new schema's first group: %s", body)
	}
	assertInitialDraftUntouched(t, s)
}

func TestSwitchBackend_DroppedArgsAskConfirmation(t *testing.T) {
	s := switchTestSession()
	body := postSwitchBackend(t, s, url.Values{
		"isNew": {"true"}, "name": {"P"},
		"backendId": {"new"}, "prevBackendId": {"old"},
		"arg.ctx-size": {"4096"}, "arg.old-only": {"xyz"},
	})
	if !strings.Contains(body, "switch-confirm") {
		t.Fatalf("expected confirmation banner when args would be dropped: %s", body)
	}
	if !strings.Contains(body, "old-only") {
		t.Fatalf("banner must name the dropped flag: %s", body)
	}
	if !strings.Contains(body, "confirmSwitch") {
		t.Fatalf("banner must re-post with confirmSwitch: %s", body)
	}
	// The OLD form is rendered back: the at-risk arg keeps its input + value...
	if !strings.Contains(body, `name="arg.old-only"`) || !strings.Contains(body, `value="xyz"`) {
		t.Fatalf("old form must still carry the dropped arg input with its value: %s", body)
	}
	// ...and the select shows the old backend selected again.
	if !strings.Contains(body, `value="old" selected`) {
		t.Fatalf("select must render reverted to the old backend: %s", body)
	}
	assertInitialDraftUntouched(t, s)
}

// extractFormFields pulls name/value pairs from rendered inputs and selects
// (selected option) so a test can re-submit a form exactly as the browser
// would. Alpine-bound inputs (":name=") have no static name and
// are skipped, matching what a real form submission includes.
func extractFormFields(t *testing.T, body string) url.Values {
	t.Helper()
	fields := url.Values{}
	attr := func(tag, name string) (string, bool) {
		m := regexp.MustCompile(name + `="([^"]*)"`).FindStringSubmatch(tag)
		if m == nil {
			return "", false
		}
		return m[1], true
	}
	for _, tag := range regexp.MustCompile(`<input[^>]*>`).FindAllString(body, -1) {
		name, ok := attr(tag, ` name`)
		if !ok || name == "" {
			continue
		}
		val, _ := attr(tag, `value`)
		fields.Set(name, val)
	}
	for _, sel := range regexp.MustCompile(`(?s)<select[^>]*>.*?</select>`).FindAllString(body, -1) {
		openTag := sel[:strings.Index(sel, ">")+1]
		name, ok := attr(openTag, ` name`)
		if !ok || name == "" {
			continue
		}
		for _, opt := range regexp.MustCompile(`<option[^>]*>`).FindAllString(sel, -1) {
			if strings.Contains(opt, " selected") {
				val, _ := attr(opt, `value`)
				fields.Set(name, val)
			}
		}
	}
	return fields
}

// TestSwitchBackend_ConfirmRoundTrip drives the full two-POST contract through
// the HTML the handler actually renders: the confirmed re-POST is built from
// the unconfirmed response's form fields (exactly what hx-include="#profile-form"
// submits) plus the banner button's confirmSwitch value, so a field-name drift
// between template and handler fails here instead of in the browser.
func TestSwitchBackend_ConfirmRoundTrip(t *testing.T) {
	s := switchTestSession()
	first := postSwitchBackend(t, s, url.Values{
		"isNew": {"true"}, "name": {"P"},
		"backendId": {"new"}, "prevBackendId": {"old"},
		"arg.ctx-size": {"4096"}, "arg.old-only": {"xyz"},
	})
	if !strings.Contains(first, "switch-confirm") {
		t.Fatalf("expected confirmation banner: %s", first)
	}
	form := extractFormFields(t, first)
	if got := form.Get("arg.old-only"); got != "xyz" {
		t.Fatalf("reverted form must carry the at-risk arg, got %q", got)
	}
	if got := form.Get("prevBackendId"); got != "old" {
		t.Fatalf("reverted form must carry prevBackendId=old, got %q", got)
	}
	if got := form.Get("backendId"); got != "old" {
		t.Fatalf("reverted select must post the old backend, got %q", got)
	}
	// The banner button adds confirmSwitch via hx-vals on top of the form.
	if !strings.Contains(first, `"confirmSwitch":"new"`) {
		t.Fatalf("banner must carry confirmSwitch=new in hx-vals: %s", first)
	}
	form.Set("confirmSwitch", "new")

	second := postSwitchBackend(t, s, form)
	if !strings.Contains(second, `value="4096"`) {
		t.Fatalf("kept arg must survive the round trip: %s", second)
	}
	if strings.Contains(second, `name="arg.old-only"`) {
		t.Fatalf("dropped arg must be gone after the round trip: %s", second)
	}
	if strings.Contains(second, "switch-confirm") {
		t.Fatalf("no banner expected after confirming: %s", second)
	}
	if !strings.Contains(second, `name="prevBackendId" value="new"`) {
		t.Fatalf("round trip must land on the new backend's form: %s", second)
	}
	assertInitialDraftUntouched(t, s)
}

func TestSwitchBackend_ConfirmedDropsUnsupportedArgs(t *testing.T) {
	s := switchTestSession()
	// The confirm button re-posts the reverted old form plus confirmSwitch=new.
	body := postSwitchBackend(t, s, url.Values{
		"isNew": {"true"}, "name": {"P"},
		"backendId": {"old"}, "prevBackendId": {"old"}, "confirmSwitch": {"new"},
		"arg.ctx-size": {"4096"}, "arg.old-only": {"xyz"},
	})
	if !strings.Contains(body, `value="4096"`) {
		t.Fatalf("kept arg must survive the confirmed switch: %s", body)
	}
	if strings.Contains(body, `name="arg.old-only"`) {
		t.Fatalf("dropped arg must be gone after the confirmed switch: %s", body)
	}
	if strings.Contains(body, "switch-confirm") {
		t.Fatalf("no banner expected on the confirmed switch: %s", body)
	}
	assertInitialDraftUntouched(t, s)
}

func TestDraftFromForm_ParsesEnvVars(t *testing.T) {
	form := url.Values{
		"id":         []string{"test"},
		"name":       []string{"Test"},
		"isNew":      []string{"true"},
		"envKey_0":   []string{"FOO"},
		"envValue_0": []string{"bar"},
		"envKey_1":   []string{"BAZ"},
		"envValue_1": []string{"qux"},
		"envKey_2":   []string{""},
		"envValue_2": []string{"skip"},
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

// TestBackendValidateHandler_TagsInvalidFields verifies UIUX-001: an invalid
// backend draft renders each error as a class="issue error" div carrying the
// offending field's data-field attribute so editor.js decorates the right input.
func TestBackendValidateHandler_TagsInvalidFields(t *testing.T) {
	s := &Session{deps: Deps{}}
	form := url.Values{"name": {""}, "executable": {""}}
	req := httptest.NewRequest("POST", "/backend/validate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleBackendValidate(rec, req)
	body := rec.Body.String()
	for _, want := range []string{
		`class="issue error" data-field="name"`,
		`class="issue error" data-field="executable"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %s in validate output, got: %s", want, body)
		}
	}
}

// TestBackendSaveHandler_BlocksOnInvalidDraft verifies UIUX-001: saving an
// invalid backend draft (empty executable) renders the field-tagged issues,
// emits no HX-Redirect, and never completes the session — the validation guard
// returns before touching the nil Manager.
func TestBackendSaveHandler_BlocksOnInvalidDraft(t *testing.T) {
	s := &Session{deps: Deps{}, done: make(chan Result, 1)}
	form := url.Values{
		"isNew": {"true"}, "name": {"X"}, "kind": {"llama-server"}, "executable": {""},
	}
	req := httptest.NewRequest("POST", "/backend/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleBackendSave(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `data-field="executable"`) {
		t.Fatalf("expected executable issue in save output, got: %s", body)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "" {
		t.Fatalf("invalid save must not redirect, got HX-Redirect=%q", got)
	}
	select {
	case <-s.Done():
		t.Fatal("save must not complete on invalid draft")
	default:
	}
}

// TestCustomizeAddFlag_ReturnsLiveFlagsPartial verifies UIUX-002: adding a flag
// saves it to the schema (marked editable) and returns the re-rendered
// customize-flags partial containing the new flag, so the grid updates live.
func TestCustomizeAddFlag_ReturnsLiveFlagsPartial(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	store := &captureSchemaStore{schema: schema}
	s := &Session{deps: Deps{Schemas: store, Catalog: stubCatalog{id: "llama", ref: "llama.json"}}, done: make(chan Result, 1)}
	form := url.Values{"backendId": {"llama"}, "flag": {"max-tokens"}, "type": {"int"}}
	req := httptest.NewRequest("POST", "/customize/flag/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleCustomizeAddFlag(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `id="customize-flags"`) {
		t.Fatalf("expected re-rendered flags partial, got: %s", body)
	}
	if !strings.Contains(body, `class="flag-name">max-tokens`) {
		t.Fatalf("expected new flag in rendered partial, got: %s", body)
	}
	if _, ok := store.saved.Flags["max-tokens"]; !ok {
		t.Fatalf("added flag not persisted: %+v", store.saved.Flags)
	}
	if !store.saved.Source.Editable {
		t.Fatalf("schema must be marked Editable after adding a flag")
	}
}
