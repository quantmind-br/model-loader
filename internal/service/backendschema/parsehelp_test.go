package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// parseHelpSchema must reject a binary that cannot be exec'd as --help.
func TestParseHelpSchema_BadBinary(t *testing.T) {
	backend := domain.Backend{
		ID:         "x",
		Kind:       domain.BackendKindLlamaServer,
		Executable: "/nonexistent/definitely-not-a-binary",
	}
	_, err := parseHelpSchema(backend, "/nonexistent/definitely-not-a-binary")
	if err == nil {
		t.Fatal("expected error parsing --help from a missing binary")
	}
}
