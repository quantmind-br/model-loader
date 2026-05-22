package cli

import (
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

func TestDeleteProfile(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "p1", "P1")
	var out strings.Builder
	if err := deleteProfile(&out, s, "p1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get("p1"); err == nil {
		t.Fatalf("profile still present")
	}
}

func TestDuplicateProfile_DefaultID(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "p1", "P1")
	var out strings.Builder
	if err := duplicateProfile(&out, s, "p1", ""); err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := s.Get("p1-copy"); err != nil {
		t.Fatalf("expected p1-copy: %v", err)
	}
}

func TestRenameProfile(t *testing.T) {
	dir := t.TempDir()
	s, _ := profilestore.NewFSStore(dir)
	seed(t, s, "p1", "Old Name")
	var out strings.Builder
	if err := renameProfile(&out, s, dir, "p1", "Brand New"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, err := s.Get("p1")
	if err != nil || got.Name != "Brand New" {
		t.Fatalf("rename did not apply: %+v %v", got, err)
	}
}

func TestSetPinned(t *testing.T) {
	dir := t.TempDir()
	s, _ := profilestore.NewFSStore(dir)
	seed(t, s, "p1", "P1")
	var out strings.Builder
	if err := setPinned(&out, s, dir, "p1", true); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if got, _ := s.Get("p1"); !got.Pinned {
		t.Fatalf("not pinned")
	}
	if err := setPinned(&out, s, dir, "p1", false); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if got, _ := s.Get("p1"); got.Pinned {
		t.Fatalf("still pinned")
	}
}
