package configweb

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestBackendDraftFromForm_ParsesAllFields(t *testing.T) {
	form := url.Values{
		"id":          []string{"test"},
		"name":        []string{"Test Backend"},
		"isNew":       []string{"true"},
		"kind":        []string{"llama-server"},
		"executable":  []string{"/bin/llama-server"},
		"description": []string{"desc"},
		"tags":        []string{"a, b, c"},
	}
	req := httptest.NewRequest("POST", "/backend/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	d := backendDraftFromForm(req)
	if d.ID != "test" {
		t.Fatalf("id = %q", d.ID)
	}
	if d.Name != "Test Backend" {
		t.Fatalf("name = %q", d.Name)
	}
	if !d.IsNew {
		t.Fatal("expected IsNew=true")
	}
	if d.Kind != domain.BackendKindLlamaServer {
		t.Fatalf("kind = %q", d.Kind)
	}
	if d.Executable != "/bin/llama-server" {
		t.Fatalf("executable = %q", d.Executable)
	}
	if d.Description != "desc" {
		t.Fatalf("description = %q", d.Description)
	}
	if len(d.Tags) != 3 || d.Tags[0] != "a" || d.Tags[1] != "b" || d.Tags[2] != "c" {
		t.Fatalf("tags = %v", d.Tags)
	}
}

func TestBackendDraftFromForm_EmptyTags(t *testing.T) {
	form := url.Values{"id": []string{"test"}, "name": []string{"Test"}}
	req := httptest.NewRequest("POST", "/backend/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	d := backendDraftFromForm(req)
	if len(d.Tags) != 0 {
		t.Fatalf("expected no tags, got %v", d.Tags)
	}
}

func TestBackendDraft_ToBackend(t *testing.T) {
	d := BackendDraft{
		ID: "my-backend", Name: "My Backend", Kind: domain.BackendKindLlamaServer,
		Executable: "/bin/llama-server", Description: "desc", Tags: []string{"cuda"},
	}
	b := d.ToBackend()
	if b.ID != "my-backend" || b.Name != "My Backend" || b.Kind != domain.BackendKindLlamaServer {
		t.Fatalf("backend mismatch: %+v", b)
	}
	if b.Executable != "/bin/llama-server" || b.Description != "desc" || len(b.Tags) != 1 {
		t.Fatalf("backend fields mismatch: %+v", b)
	}
}

func TestBackendDraft_ApplyTo(t *testing.T) {
	existing := domain.Backend{
		ID: "my-backend", Name: "Old", Kind: domain.BackendKindLlamaServer,
		Executable: "/bin/old", Description: "old desc", Tags: []string{"old"},
	}
	d := BackendDraft{Name: "New", Executable: "/bin/new", Description: "new desc", Tags: []string{"new"}}
	b := d.ApplyTo(existing)
	if b.Name != "New" || b.Executable != "/bin/new" || b.Description != "new desc" {
		t.Fatalf("fields not updated: %+v", b)
	}
	if b.ID != "my-backend" || b.Kind != domain.BackendKindLlamaServer {
		t.Fatalf("immutable fields changed: %+v", b)
	}
}
