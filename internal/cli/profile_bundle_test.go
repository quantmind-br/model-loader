package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

func TestExportProfiles_Subset(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "a", "A")
	seed(t, s, "b", "B")
	var buf bytes.Buffer
	if err := exportProfiles(&buf, s, []string{"a"}, "-"); err != nil {
		t.Fatalf("export: %v", err)
	}
	var bundle profilestore.ExportBundle
	if err := json.Unmarshal(buf.Bytes(), &bundle); err != nil {
		t.Fatalf("bundle not JSON: %v", err)
	}
	if len(bundle.Profiles) != 1 || bundle.Profiles[0].ID != "a" {
		t.Fatalf("subset export wrong: %+v", bundle.Profiles)
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	src := newTempStore(t)
	seed(t, src, "a", "A")
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.json")
	var buf bytes.Buffer
	if err := exportProfiles(&buf, src, nil, path); err != nil {
		t.Fatalf("export: %v", err)
	}
	dst := newTempStore(t)
	var out bytes.Buffer
	if err := importProfiles(&out, dst, path, profilestore.ConflictModeMerge); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := dst.Get("a"); err != nil {
		t.Fatalf("imported profile missing: %v", err)
	}
}

func TestValidateProfile_OK(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "a", "A")
	var out strings.Builder
	code := validateProfile(&out, &out, s, noopValidator{}, nil, "a")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
}
