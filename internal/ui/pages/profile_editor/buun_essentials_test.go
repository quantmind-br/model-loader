package profile_editor

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/buunhelp"
)

func TestEssentials_BuunAgainstEmbeddedSchema(t *testing.T) {
	schema := buunhelp.EmbeddedSchema()
	got := essentialsFor(domain.BackendKindBuunLlamaCpp, schema)

	// Every curated buun essential must resolve against the embedded schema,
	// otherwise it would be silently dropped from the editor.
	want := []string{
		"n-gpu-layers", "ctx-size", "flash-attn", "port",
		"cache-type-k", "cache-type-v",
		"spec-draft-model", "spec-dflash-default", "dflash-max-slots",
		"spec-type", "draft-max", "draft-min",
	}
	have := make(map[string]bool, len(got))
	for _, f := range got {
		have[f.Flag] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("essential %q dropped (not found in embedded schema)", w)
		}
	}
}
