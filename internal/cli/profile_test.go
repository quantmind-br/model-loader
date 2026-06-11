package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

// newTempStore returns an FSStore rooted at a temp dir. Shared across profile tests.
func newTempStore(t *testing.T) *profilestore.FSStore {
	t.Helper()
	s, err := profilestore.NewFSStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFSStore: %v", err)
	}
	return s
}

func seed(t *testing.T, s profilestore.Store, id, name string) domain.Profile {
	t.Helper()
	p := domain.Profile{ID: id, Name: name, Model: "m.gguf", Args: map[string]any{}}
	if err := s.Create(p); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	got, _ := s.Get(id)
	return got
}

func TestListProfiles_Table(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "alpha", "Alpha")
	seed(t, s, "beta", "Beta")
	var buf bytes.Buffer
	if err := listProfiles(&buf, s, false); err != nil {
		t.Fatalf("listProfiles: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Fatalf("table missing ids: %q", out)
	}
}

func TestListProfiles_JSON(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "alpha", "Alpha")
	var buf bytes.Buffer
	if err := listProfiles(&buf, s, true); err != nil {
		t.Fatalf("listProfiles json: %v", err)
	}
	var got []domain.Profile
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON array: %v (%q)", err, buf.String())
	}
	if len(got) != 1 || got[0].ID != "alpha" {
		t.Fatalf("unexpected json: %+v", got)
	}
}

func TestResolveProfileRef_ByIDAndName(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "alpha", "Alpha One")
	// by id
	if p, err := resolveProfileRef(s, "alpha"); err != nil || p.ID != "alpha" {
		t.Fatalf("by id: %+v %v", p, err)
	}
	// by exact name (case-insensitive)
	if p, err := resolveProfileRef(s, "alpha one"); err != nil || p.ID != "alpha" {
		t.Fatalf("by name: %+v %v", p, err)
	}
	// not found
	if _, err := resolveProfileRef(s, "nope"); err == nil {
		t.Fatalf("expected not-found error")
	}
}

func TestResolveProfileRef_AmbiguousName(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "alpha-1", "Duplicate")
	seed(t, s, "alpha-2", "Duplicate")
	_, err := resolveProfileRef(s, "duplicate")
	if err == nil {
		t.Fatalf("expected ambiguous error")
	}
	if !strings.Contains(err.Error(), "alpha-1") || !strings.Contains(err.Error(), "alpha-2") {
		t.Fatalf("ambiguous error should list candidate IDs, got: %v", err)
	}
	if !strings.Contains(err.Error(), "use the id") {
		t.Fatalf("ambiguous error should hint about using id, got: %v", err)
	}
}

func TestShowProfile_JSON(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "alpha", "Alpha")
	var buf bytes.Buffer
	if err := showProfile(&buf, s, "alpha", true); err != nil {
		t.Fatalf("showProfile: %v", err)
	}
	var got domain.Profile
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON object: %v", err)
	}
	if got.ID != "alpha" {
		t.Fatalf("wrong profile: %+v", got)
	}
}
