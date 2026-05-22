package assets

import "testing"

func TestEmbed_HasTemplatesAndStatic(t *testing.T) {
	if _, err := FS.ReadFile("templates/base.gohtml"); err != nil {
		t.Fatalf("base template missing: %v", err)
	}
	if b, err := FS.ReadFile("static/htmx.min.js"); err != nil || len(b) < 1000 {
		t.Fatalf("htmx not embedded: %v len=%d", err, len(b))
	}
	if b, err := FS.ReadFile("static/app.css"); err != nil || len(b) < 100 {
		t.Fatalf("app.css not embedded: %v len=%d", err, len(b))
	}
}
