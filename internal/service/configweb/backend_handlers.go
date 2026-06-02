package configweb

import (
	"context"
	"html"
	"net/http"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
)

type BackendViewModel struct {
	Draft     BackendDraft
	Kinds     []domain.BackendKind
	IsNew     bool
	ReadOnlyKind bool
}

func (s *Session) handleBackendIndex(w http.ResponseWriter, r *http.Request) {
	vm := BackendViewModel{
		Draft: s.deps.InitialBackendDraft,
		Kinds: []domain.BackendKind{
			domain.BackendKindLlamaServer,
			domain.BackendKindVLLM,
			domain.BackendKindSGLang,
			domain.BackendKindDFlash,
			domain.BackendKindBuunLlamaCpp,
			domain.BackendKindBeeLlamaCpp,
		},
		IsNew:        s.deps.InitialBackendDraft.IsNew,
		ReadOnlyKind: !s.deps.InitialBackendDraft.IsNew,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "backend", vm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Session) handleBackendSave(w http.ResponseWriter, r *http.Request) {
	d := backendDraftFromForm(r)
	if d.ID == "" {
		d.ID = domain.Slugify(d.Name)
	}

	var berr error
	var b domain.Backend
	if d.IsNew {
		if s.deps.Manager == nil {
			http.Error(w, "backend manager not available", http.StatusInternalServerError)
			return
		}
		b, berr = s.deps.Manager.AddBackend(context.Background(), d.Name, d.Executable, d.Kind)
		if berr == nil && (d.Description != "" || len(d.Tags) > 0) {
			b.Description = d.Description
			b.Tags = d.Tags
			b, berr = s.deps.Manager.UpdateBackend(b.ID, b)
		}
	} else {
		if s.deps.Manager == nil {
			http.Error(w, "backend manager not available", http.StatusInternalServerError)
			return
		}
		lookup := d.OrigID
		if lookup == "" {
			lookup = d.ID
		}
		existing, gerr := s.deps.Manager.GetBackend(lookup)
		if gerr != nil {
			berr = gerr
		} else {
			b, berr = s.deps.Manager.UpdateBackend(lookup, d.ApplyTo(existing))
		}
	}
	if berr != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<div class="issue error">` + html.EscapeString(berr.Error()) + `</div>`))
		return
	}
	w.Header().Set("HX-Redirect", "/backend/closed")
	w.WriteHeader(http.StatusOK)
	s.complete(Result{Saved: true, BackendID: b.ID})
}

func (s *Session) handleBackendCancel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("HX-Redirect", "/backend/closed")
	w.WriteHeader(http.StatusOK)
	s.complete(Result{Saved: false})
}

func (s *Session) handleBackendClosed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(donePageHTML))
	s.requestShutdown()
}

func (s *Session) handleBackendValidate(w http.ResponseWriter, r *http.Request) {
	d := backendDraftFromForm(r)
	var issues []string
	if strings.TrimSpace(d.Name) == "" {
		issues = append(issues, "Name is required")
	}
	if strings.TrimSpace(d.Executable) == "" {
		issues = append(issues, "Executable is required")
	}
	if d.Kind == "" {
		issues = append(issues, "Kind is required")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b strings.Builder
	b.WriteString(`<div id="issues" hx-swap-oob="true">`)
	if len(issues) == 0 {
		b.WriteString(`<span class="ok">✓ valid</span>`)
	}
	for _, e := range issues {
		b.WriteString(`<div class="issue error">` + html.EscapeString(e) + `</div>`)
	}
	b.WriteString(`</div>`)
	_, _ = w.Write([]byte(b.String()))
}
