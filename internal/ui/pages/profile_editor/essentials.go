package profile_editor

import "github.com/quantmind-br/model-loader/internal/domain"

// EssentialField promotes a schema flag to a first-class field in the
// Essentials group. Real type/enum/help/default come from schema.Lookup(Flag);
// the fields below are UX and validation hints only.
type EssentialField struct {
	Flag        string           // name for schema.Lookup (long, short, or alias)
	Label       string           // UI title (fallback: Flag)
	Description string           // UI description (fallback: schema HelpText)
	Min, Max    *int             // optional bounds for FlagTypeInt
	IsPort      bool             // uses portValidator()
	AllowEmpty  bool             // optional int may be left blank
	Default     string           // default string value when absent from Args
	Coerce      func(any) string // custom hydration (default: ArgString)
}

func iptr(v int) *int { return &v }

// essentialFields lists, per backend, the flags promoted to Essentials.
// Order defines display order. Flags absent from the schema are omitted.
var essentialFields = map[domain.BackendKind][]EssentialField{
	domain.BackendKindLlamaServer: {
		{Flag: "n-gpu-layers", Label: "ngl (gpu layers)", Description: "Number of GPU layers to offload", Min: iptr(-1), Max: iptr(9999), Default: "99"},
		{Flag: "ctx-size", Label: "ctx-size", Description: "Context window size in tokens", Min: iptr(0), Max: iptr(1024 * 1024), Default: "8192"},
		{Flag: "batch-size", Label: "batch-size", Description: "Prompt processing batch size", Min: iptr(0), Max: iptr(1024 * 1024), AllowEmpty: true, Default: "2048"},
		{Flag: "ubatch-size", Label: "ubatch-size", Description: "Physical batch size", Min: iptr(0), Max: iptr(1024 * 1024), AllowEmpty: true, Default: "512"},
		{Flag: "port", Label: "port", Description: "Port to bind the inference server", IsPort: true, Default: "4321"},
		{Flag: "flash-attn", Label: "flash-attn", Description: "Flash Attention mode", Default: "auto", Coerce: FlashAttnToString},
		{Flag: "cache-type-k", Label: "cache-type-k", Description: "Key cache quantization", Default: "q8_0"},
		{Flag: "cache-type-v", Label: "cache-type-v", Description: "Value cache quantization", Default: "q8_0"},
	},
	domain.BackendKindVLLM: {
		{Flag: "tensor-parallel-size", Label: "tensor-parallel-size", Description: "GPUs for tensor parallelism", Min: iptr(1), Max: iptr(64), Default: "1"},
		{Flag: "gpu-memory-utilization", Label: "gpu-memory-utilization", Description: "Fraction of GPU memory (0.0–1.0)", Default: "0.9"},
		{Flag: "max-model-len", Label: "max-model-len", Description: "Max context length (0 = auto)", Min: iptr(0), Max: iptr(1024 * 1024), AllowEmpty: true},
		{Flag: "dtype", Label: "dtype", Description: "Weights/activations dtype", Default: "auto"},
		{Flag: "quantization", Label: "quantization", Description: "Quantization method", Default: "None"},
		{Flag: "port", Label: "port", Description: "Port to listen on", IsPort: true, Default: "8000"},
		{Flag: "served-model-name", Label: "served-model-name", Description: "Name advertised in /v1/models"},
	},
	domain.BackendKindSGLang: {
		{Flag: "tp-size", Label: "tp-size", Description: "Tensor parallelism size", Min: iptr(1), Max: iptr(64), Default: "1"},
		{Flag: "dp-size", Label: "dp-size", Description: "Data parallelism size", Min: iptr(1), Max: iptr(64), Default: "1"},
		{Flag: "mem-fraction-static", Label: "mem-fraction-static", Description: "GPU memory reserved for KV cache", Default: "0.9"},
		{Flag: "dtype", Label: "dtype", Description: "Weights dtype", Default: "auto"},
		{Flag: "quantization", Label: "quantization", Description: "Quantization method"},
		{Flag: "context-length", Label: "context-length", Description: "Max context length (0 = model default)", Min: iptr(0), Max: iptr(1024 * 1024), AllowEmpty: true},
		{Flag: "port", Label: "port", Description: "Server port", IsPort: true, Default: "30000"},
		{Flag: "served-model-name", Label: "served-model-name", Description: "Name exposed in the API"},
	},
	domain.BackendKindDFlash: {
		{Flag: "draft", Label: "draft (model)", Description: "Path to the DFlash draft model"},
		{Flag: "max-ctx", Label: "max-ctx", Description: "Max context length (>16k can slow attention 20x+)", Min: iptr(0), Max: iptr(1024 * 1024), Default: "16384"},
		{Flag: "budget", Label: "budget", Description: "Speculative decode token budget per step", Min: iptr(1), Max: iptr(512), Default: "22"},
		{Flag: "verify-mode", Label: "verify-mode", Description: "Daemon verify mode (ddtree/fast/seq/replay)", Default: "ddtree"},
		{Flag: "cache-type-k", Label: "cache-type-k", Description: "KV cache type for keys", Default: "q8_0"},
		{Flag: "cache-type-v", Label: "cache-type-v", Description: "KV cache type for values", Default: "q8_0"},
		{Flag: "fa-window", Label: "fa-window", Description: "Sliding-window flash attention (0 = full)", Min: iptr(0), Max: iptr(1024 * 1024), Default: "2048"},
		{Flag: "port", Label: "port", Description: "Server port", IsPort: true, Default: "8080"},
	},
	domain.BackendKindBuunLlamaCpp: {
		{Flag: "n-gpu-layers", Label: "ngl (gpu layers)", Description: "Number of GPU layers to offload", Min: iptr(-1), Max: iptr(9999), Default: "99"},
		{Flag: "ctx-size", Label: "ctx-size", Description: "Context window size in tokens", Min: iptr(0), Max: iptr(1024 * 1024), Default: "8192"},
		{Flag: "flash-attn", Label: "flash-attn", Description: "Flash Attention mode", Default: "auto", Coerce: FlashAttnToString},
		{Flag: "port", Label: "port", Description: "Port to bind the inference server", IsPort: true, Default: "8080"},
		{Flag: "cache-type-k", Label: "cache-type-k", Description: "K cache type (turbo2/3/4, turbo*_tcq, or standard)", Default: "f16"},
		{Flag: "cache-type-v", Label: "cache-type-v", Description: "V cache type (turbo2/3/4, turbo*_tcq, or standard)", Default: "f16"},
		{Flag: "spec-type", Label: "spec-type", Description: "Speculative decoding strategy (dflash/copyspec/ngram-*/...)", Default: "none"},
		{Flag: "spec-draft-model", Label: "spec-draft-model (-md)", Description: "Path to the speculative draft model"},
		{Flag: "spec-dflash-default", Label: "spec-dflash-default", Description: "Enable default DFlash config (requires draft model)"},
		{Flag: "dflash-max-slots", Label: "dflash-max-slots", Description: "Max concurrent server slots with DFlash state", Min: iptr(1), Max: iptr(1024), Default: "1"},
		{Flag: "draft-max", Label: "draft-max", Description: "Max draft tokens per step", Min: iptr(0), Max: iptr(512), Default: "16"},
		{Flag: "draft-min", Label: "draft-min", Description: "Min draft tokens per step", Min: iptr(0), Max: iptr(512), Default: "0"},
	},
}

// essentialsFor returns the essential fields for a kind that exist in the schema.
func essentialsFor(kind domain.BackendKind, schema domain.FlagSchema) []EssentialField {
	fields := essentialFields[kind]
	if len(fields) == 0 {
		// fallback: unknown kind → treat as llama to avoid regression
		fields = essentialFields[domain.BackendKindLlamaServer]
	}
	out := make([]EssentialField, 0, len(fields))
	for _, f := range fields {
		if _, ok := schema.Lookup(f.Flag); ok {
			out = append(out, f)
		}
	}
	return out
}

// coerce converts a stored Args value to the editor's string form,
// honouring the field's custom hook (e.g. flash-attn bool→"on").
func (f EssentialField) coerce(v any) string {
	if f.Coerce != nil {
		return f.Coerce(v)
	}
	return ArgString(v)
}
