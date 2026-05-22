package configweb

import (
	"net/http"
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
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
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
