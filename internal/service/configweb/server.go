package configweb

import (
	"net/http"

	"github.com/quantmind-br/model-loader/internal/service/configweb/assets"
)

func (s *Session) routes(mux *http.ServeMux) {
	mux.HandleFunc("/", s.handleIndex)
	mux.Handle("/static/", http.FileServer(http.FS(assets.FS)))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/validate", s.handleValidate)
	mux.HandleFunc("/save", s.handleSave)
	mux.HandleFunc("/cancel", s.handleCancel)
	mux.HandleFunc("/closed", s.handleClosed)
	mux.HandleFunc("/customize/flag", s.handleCustomizeFlag)
	mux.HandleFunc("/customize/flag/add", s.handleCustomizeAddFlag)
	mux.HandleFunc("/customize/flag/remove", s.handleCustomizeRemoveFlag)
	mux.HandleFunc("/customize/presentation", s.handleCustomizePresentation)
	mux.HandleFunc("/customize/rules", s.handleCustomizeRules)
}
