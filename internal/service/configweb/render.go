package configweb

import (
	"encoding/json"
	"html/template"
	"net/http"

	"github.com/quantmind-br/model-loader/internal/service/configweb/assets"
)

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"toJSON": func(v any) (template.JS, error) {
		b, err := json.Marshal(v)
		return template.JS(b), err
	},
}).ParseFS(assets.FS,
	"templates/base.gohtml",
	"templates/configure.gohtml",
	"templates/customize.gohtml",
))

func (s *Session) handleIndex(w http.ResponseWriter, r *http.Request) {
	schema, err := s.loadSchema(s.deps.InitialDraft.BackendID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	vm := BuildViewModel(s.deps.InitialDraft, schema)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", vm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
