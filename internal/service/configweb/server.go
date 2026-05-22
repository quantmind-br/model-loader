package configweb

import "net/http"

func (s *Session) routes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/validate", s.handleValidate)
	mux.HandleFunc("/save", s.handleSave)
	mux.HandleFunc("/cancel", s.handleCancel)
	mux.HandleFunc("/closed", s.handleClosed)
}
