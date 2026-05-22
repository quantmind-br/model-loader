package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func schemaFixture() (*fakeBackendManager, *fakeSchemaStore) {
	mgr := twoBackends()
	store := newFakeSchemaStore()
	store.schemas["llama-server.json"] = domain.BackendValidationSchema{
		SchemaVersion: 3,
		BackendID:     "llama-server",
		BackendKind:   domain.BackendKindLlamaServer,
		Source:        domain.SchemaSource{SourceVersion: "v7376", Editable: false},
		Flags: map[string]domain.FlagSpec{
			"ctx-size": {Long: "ctx-size"},
			"port":     {Long: "port"},
		},
	}
	return mgr, store
}

func TestShowSchema_TextSummary(t *testing.T) {
	mgr, store := schemaFixture()
	var out bytes.Buffer
	if err := showSchema(&out, mgr, store, "llama-server", false); err != nil {
		t.Fatalf("showSchema: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "llama-server") || !strings.Contains(s, "Flags:     2") {
		t.Fatalf("summary missing fields: %q", s)
	}
}

func TestShowSchema_JSONFull(t *testing.T) {
	mgr, store := schemaFixture()
	var out bytes.Buffer
	if err := showSchema(&out, mgr, store, "llama-server", true); err != nil {
		t.Fatalf("showSchema: %v", err)
	}
	var sc domain.BackendValidationSchema
	if err := json.Unmarshal(out.Bytes(), &sc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if sc.BackendID != "llama-server" || len(sc.Flags) != 2 {
		t.Fatalf("unexpected schema: %+v", sc)
	}
}

func TestShowSchema_NotFound(t *testing.T) {
	mgr, store := schemaFixture()
	var out bytes.Buffer
	// vllm-main resolves as a backend but has no schema stored.
	if err := showSchema(&out, mgr, store, "vllm-main", false); err == nil {
		t.Fatal("expected schema-not-found error")
	}
}

func TestBackendSchemaSubtree_Registered(t *testing.T) {
	bc := childByName(rootCmd, "backend")
	sc := childByName(bc, "schema")
	if sc == nil {
		t.Fatal("backend schema not registered")
	}
	if childByName(sc, "show") == nil {
		t.Fatal("backend schema show not registered")
	}
}
