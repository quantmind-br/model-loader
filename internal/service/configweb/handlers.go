package configweb

import (
	"encoding/json"
	"html"
	"net/http"
	"strconv"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/validator"
)

// draftFromForm parses an editor form POST into a Draft.
// Arg fields are named "arg.<flag>"; meta fields use their plain names.
func draftFromForm(r *http.Request) Draft {
	_ = r.ParseForm()
	d := Draft{
		ID:          r.FormValue("id"),
		OrigID:      r.FormValue("origId"),
		IsNew:       r.FormValue("isNew") == "true",
		Name:        r.FormValue("name"),
		Description: r.FormValue("description"),
		Model:       r.FormValue("model"),
		BackendID:   r.FormValue("backendId"),
		Args:        map[string]string{},
	}
	if tags := strings.TrimSpace(r.FormValue("tags")); tags != "" {
		for _, t := range strings.Split(tags, ",") {
			if t = strings.TrimSpace(t); t != "" {
				d.Tags = append(d.Tags, t)
			}
		}
	}
	for k, vs := range r.Form {
		if strings.HasPrefix(k, "arg.") && len(vs) > 0 {
			d.Args[strings.TrimPrefix(k, "arg.")] = vs[0]
		}
	}
	return d
}

func (s *Session) loadSchema(backendID string) (domain.BackendValidationSchema, error) {
	catalog, err := s.deps.Catalog.Load()
	if err != nil {
		return domain.BackendValidationSchema{}, err
	}
	if backendID == "" {
		backendID = catalog.DefaultBackendID
	}
	for _, b := range catalog.Backends {
		if b.ID == backendID {
			return s.deps.Schemas.Load(backendcatalog.SchemaStoreRef(b.SchemaRef))
		}
	}
	return domain.BackendValidationSchema{}, backendcatalog.ErrBackendNotFound
}

func (s *Session) handleValidate(w http.ResponseWriter, r *http.Request) {
	d := draftFromForm(r)
	schema, err := s.loadSchema(d.BackendID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fs := schema.ToFlagSchema()
	p := d.ToProfile(fs)
	rep := validator.New(nil).Validate(p, fs, schema.BackendKind)
	renderIssues(w, rep)
}

// renderIssues writes an HTMX partial listing errors and warnings.
func renderIssues(w http.ResponseWriter, rep validator.Report) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b strings.Builder
	b.WriteString(`<div id="issues" hx-swap-oob="true">`)
	if len(rep.Errors) == 0 && len(rep.Warnings) == 0 {
		b.WriteString(`<span class="ok">✓ válido</span>`)
	}
	for _, e := range rep.Errors {
		b.WriteString(`<div class="issue error" data-field="` + htmlEscape(e.Field) + `">` + htmlEscape(e.Field) + `: ` + htmlEscape(e.Message) + `</div>`)
	}
	for _, wn := range rep.Warnings {
		b.WriteString(`<div class="issue warn" data-field="` + htmlEscape(wn.Field) + `">` + htmlEscape(wn.Field) + `: ` + htmlEscape(wn.Message) + `</div>`)
	}
	b.WriteString(`</div>`)
	_, _ = w.Write([]byte(b.String()))
}

func htmlEscape(s string) string {
	return html.EscapeString(s)
}

func (s *Session) handleSave(w http.ResponseWriter, r *http.Request) {
	d := draftFromForm(r)
	if d.ID == "" {
		d.ID = domain.Slugify(d.Name)
	}
	schema, err := s.loadSchema(d.BackendID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fs := schema.ToFlagSchema()

	var perr error
	if d.IsNew {
		perr = s.deps.Profiles.Create(d.ToProfile(fs))
	} else {
		lookup := d.OrigID
		if lookup == "" {
			lookup = d.ID
		}
		existing, gerr := s.deps.Profiles.Get(lookup)
		if gerr != nil {
			perr = s.deps.Profiles.Create(d.ToProfile(fs))
		} else {
			perr = s.deps.Profiles.Save(d.ApplyTo(existing, fs))
		}
	}
	if perr != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<div class="issue error">` + htmlEscape(perr.Error()) + `</div>`))
		return
	}
	w.Header().Set("HX-Redirect", "/closed")
	w.WriteHeader(http.StatusOK)
	s.complete(Result{Saved: true, ProfileID: d.ID})
}

func (s *Session) handleCancel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("HX-Redirect", "/closed")
	w.WriteHeader(http.StatusOK)
	s.complete(Result{Saved: false})
}

func (s *Session) handleClosed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>Pronto</title><body style="font-family:sans-serif;padding:3rem;text-align:center"><h2>Pronto — pode fechar esta aba.</h2></body>`))
}

// loadSchemaRef returns the schema plus its store ref for a backend.
func (s *Session) loadSchemaRef(backendID string) (domain.BackendValidationSchema, string, error) {
	catalog, err := s.deps.Catalog.Load()
	if err != nil {
		return domain.BackendValidationSchema{}, "", err
	}
	if backendID == "" {
		backendID = catalog.DefaultBackendID
	}
	for _, b := range catalog.Backends {
		if b.ID == backendID {
			ref := backendcatalog.SchemaStoreRef(b.SchemaRef)
			sch, err := s.deps.Schemas.Load(ref)
			return sch, ref, err
		}
	}
	return domain.BackendValidationSchema{}, "", backendcatalog.ErrBackendNotFound
}

func atoiPtr(s string) *int {
	if s == "" {
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		return &n
	}
	return nil
}

// persistEditable marks the schema as a manual edit. RefreshSchema preserves
// schemas with Source.Editable=true.
func persistEditable(schema *domain.BackendValidationSchema) {
	schema.Source.Editable = true
}

// handleCustomizeFlag updates one flag's editable constraints and marks the
// schema as user-edited so RefreshSchema preserves it.
func (s *Session) handleCustomizeFlag(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	backendID := r.FormValue("backendId")
	flag := r.FormValue("flag")
	schema, ref, err := s.loadSchemaRef(backendID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	spec, ok := schema.Flags[flag]
	if !ok {
		http.Error(w, "unknown flag", http.StatusBadRequest)
		return
	}
	spec.Min = atoiPtr(r.FormValue("min"))
	spec.Max = atoiPtr(r.FormValue("max"))
	spec.Required = r.FormValue("required") == "on"
	if dv := r.FormValue("default"); dv != "" {
		spec.Default = dv
	}
	if ev := r.FormValue("enumValues"); ev != "" {
		spec.EnumValues = splitCSV(ev)
	}
	schema.Flags[flag] = spec
	persistEditable(&schema)
	if err := s.deps.Schemas.Save(ref, schema); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// handleCustomizeAddFlag adds a new user-defined flag to the schema.
func (s *Session) handleCustomizeAddFlag(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	schema, ref, err := s.loadSchemaRef(r.FormValue("backendId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	long := r.FormValue("flag")
	if long == "" {
		http.Error(w, "flag name required", http.StatusBadRequest)
		return
	}
	if schema.Flags == nil {
		schema.Flags = map[string]domain.FlagSpec{}
	}
	schema.Flags[long] = domain.FlagSpec{
		Long:     long,
		Type:     parseFlagType(r.FormValue("type")),
		HelpText: r.FormValue("help"),
		Group:    "custom",
	}
	persistEditable(&schema)
	if err := s.deps.Schemas.Save(ref, schema); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleCustomizeRemoveFlag deletes a flag and any presentation reference to it.
func (s *Session) handleCustomizeRemoveFlag(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	schema, ref, err := s.loadSchemaRef(r.FormValue("backendId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	long := r.FormValue("flag")
	delete(schema.Flags, long)
	if schema.Presentation != nil {
		for gi := range schema.Presentation.Groups {
			schema.Presentation.Groups[gi].Flags = removeStr(schema.Presentation.Groups[gi].Flags, long)
		}
	}
	persistEditable(&schema)
	if err := s.deps.Schemas.Save(ref, schema); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleCustomizePresentation overwrites the presentation from a JSON body.
func (s *Session) handleCustomizePresentation(w http.ResponseWriter, r *http.Request) {
	schema, ref, err := s.loadSchemaRef(r.URL.Query().Get("backendId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var pres domain.Presentation
	if err := jsonDecode(r, &pres); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	schema.Presentation = &pres
	persistEditable(&schema)
	if err := s.deps.Schemas.Save(ref, schema); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleCustomizeRules overwrites the cross-field rules from a JSON body.
func (s *Session) handleCustomizeRules(w http.ResponseWriter, r *http.Request) {
	schema, ref, err := s.loadSchemaRef(r.URL.Query().Get("backendId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var rules []domain.CrossFieldRule
	if err := jsonDecode(r, &rules); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if msg := validateRules(rules, schema); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	schema.Rules = rules
	persistEditable(&schema)
	if err := s.deps.Schemas.Save(ref, schema); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func parseFlagType(s string) domain.FlagType {
	switch s {
	case "int":
		return domain.FlagTypeInt
	case "float":
		return domain.FlagTypeFloat
	case "bool":
		return domain.FlagTypeBool
	case "enum":
		return domain.FlagTypeEnum
	default:
		return domain.FlagTypeString
	}
}

func removeStr(xs []string, s string) []string {
	out := xs[:0]
	for _, x := range xs {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

func jsonDecode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func validateRules(rules []domain.CrossFieldRule, schema domain.BackendValidationSchema) string {
	for _, rule := range rules {
		if _, ok := schema.Flags[rule.When.Flag]; !ok {
			return "rule references unknown flag: " + rule.When.Flag
		}
		if rule.Then.Flag != "" {
			if _, ok := schema.Flags[rule.Then.Flag]; !ok {
				return "rule effect references unknown flag: " + rule.Then.Flag
			}
		}
		if rule.Severity != "warning" && rule.Severity != "error" {
			return "rule severity must be warning or error"
		}
	}
	return ""
}
