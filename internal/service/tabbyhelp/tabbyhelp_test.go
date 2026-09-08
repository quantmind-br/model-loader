package tabbyhelp

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestEmbeddedSchema(t *testing.T) {
	fs := EmbeddedSchema()
	if fs.Version != "embedded-tabby-v6" {
		t.Fatalf("version = %q", fs.Version)
	}

	// Spot-check the load-bearing flags exist with the right types.
	want := map[string]domain.FlagType{
		"max-seq-len":     domain.FlagTypeInt,
		"cache-mode":      domain.FlagTypeString,
		"tensor-parallel": domain.FlagTypeBool,
		"gpu-split":       domain.FlagTypeString,
		"backend":         domain.FlagTypeEnum,
		"draft-mode":              domain.FlagTypeEnum,
		"sysmem-multimodal-cache": domain.FlagTypeInt,
		"access-log":              domain.FlagTypeBool,
	}
	for long, typ := range want {
		spec, ok := fs.Flags[long]
		if !ok {
			t.Errorf("missing flag %q", long)
			continue
		}
		if spec.Type != typ {
			t.Errorf("flag %q type = %v, want %v", long, spec.Type, typ)
		}
	}

	// port must be flagged manager-owned.
	port, ok := fs.Flags["port"]
	if !ok || !port.IsPort {
		t.Fatalf("port flag missing or not IsPort: %+v", port)
	}

	// model is emitted from Profile.Model (as --model-dir/--model-name), never a flag row.
	if _, ok := fs.Flags["model"]; ok {
		t.Error("model must not be a schema flag")
	}

	// backend enum: exllamav2 was removed upstream (c8b9a8f) — only exllamav3 remains.
	be := fs.Flags["backend"]
	if len(be.EnumValues) != 1 || be.EnumValues[0] != "exllamav3" {
		t.Fatalf("backend enum = %v", be.EnumValues)
	}

	// draft-cache-mode is typed CACHE_SIZES in config_models.py, not CACHE_TYPE:
	// only the legacy aliases pass validation, a "k,v" bit pair does not.
	dcm := fs.Flags["draft-cache-mode"]
	if dcm.Type != domain.FlagTypeEnum {
		t.Errorf("draft-cache-mode type = %v, want enum", dcm.Type)
	}
	if got, want := len(dcm.EnumValues), 4; got != want {
		t.Errorf("draft-cache-mode enum = %v", dcm.EnumValues)
	}

	// tool-format is a closed registry (ALL_TOOLCALL_FORMATS); an unknown value
	// silently disables tool parsing, so the schema must reject it up front.
	tf := fs.Flags["tool-format"]
	if tf.Type != domain.FlagTypeEnum {
		t.Fatalf("tool-format type = %v, want enum", tf.Type)
	}
	for _, want := range []string{"qwen3_coder", "harmony", "laguna", "hy3", "deepseek_v4"} {
		found := false
		for _, v := range tf.EnumValues {
			if v == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tool-format enum missing %q: %v", want, tf.EnumValues)
		}
	}

	// Flags whose backend default is "unset" (the engine picks) must carry no
	// schema default, so the editor never materializes a wrong explicit value.
	for _, long := range []string{"max-seq-len", "cache-size", "max-batch-size", "draft-num-tokens", "harmony"} {
		if spec, ok := fs.Flags[long]; !ok {
			t.Errorf("missing flag %q", long)
		} else if spec.Default != nil {
			t.Errorf("flag %q default = %v, want unset", long, spec.Default)
		}
	}
}
