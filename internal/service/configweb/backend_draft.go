package configweb

import (
	"net/http"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
)

type BackendDraft struct {
	ID          string
	OrigID      string
	IsNew       bool
	Name        string
	Kind        domain.BackendKind
	Executable  string
	Description string
	Tags        []string
}

func backendDraftFromForm(r *http.Request) BackendDraft {
	_ = r.ParseForm()
	d := BackendDraft{
		ID:          r.FormValue("id"),
		OrigID:      r.FormValue("origId"),
		IsNew:       r.FormValue("isNew") == "true",
		Name:        r.FormValue("name"),
		Executable:  r.FormValue("executable"),
		Description: r.FormValue("description"),
	}
	if k := r.FormValue("kind"); k != "" {
		d.Kind = domain.BackendKind(k)
	}
	if tags := strings.TrimSpace(r.FormValue("tags")); tags != "" {
		for _, t := range strings.Split(tags, ",") {
			if t = strings.TrimSpace(t); t != "" {
				d.Tags = append(d.Tags, t)
			}
		}
	}
	return d
}

func (d BackendDraft) ToBackend() domain.Backend {
	return domain.Backend{
		ID:          d.ID,
		Name:        d.Name,
		Kind:        d.Kind,
		Executable:  d.Executable,
		Description: d.Description,
		Tags:        d.Tags,
	}
}

func (d BackendDraft) ApplyTo(existing domain.Backend) domain.Backend {
	existing.Name = d.Name
	existing.Executable = d.Executable
	existing.Description = d.Description
	existing.Tags = d.Tags
	return existing
}
