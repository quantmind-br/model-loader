package configweb

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/configweb/assets"
)

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"toJSON": func(v any) (template.JS, error) {
		b, err := json.Marshal(v)
		return template.JS(b), err
	},
	"joinCSV": func(v []string) string {
		return strings.Join(v, ", ")
	},
}).ParseFS(assets.FS,
	"templates/base.gohtml",
	"templates/configure.gohtml",
	"templates/customize.gohtml",
	"templates/backend.gohtml",
))

func (s *Session) handleIndex(w http.ResponseWriter, r *http.Request) {
	schema, err := s.loadSchema(s.deps.InitialDraft.BackendID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	backends, _ := s.loadBackends()
	vm := BuildViewModel(s.deps.InitialDraft, schema, backends)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", vm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Session) loadBackends() ([]domain.Backend, error) {
	catalog, err := s.deps.Catalog.Load()
	if err != nil {
		return nil, err
	}
	return catalog.Backends, nil
}
