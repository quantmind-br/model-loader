package domain

import (
	"path/filepath"
	"strings"
)

// knownModelExtensions are file suffixes that indicate a local model file.
var knownModelExtensions = map[string]bool{
	".gguf":        true,
	".bin":         true,
	".safetensors": true,
	".pt":          true,
	".pth":         true,
	".onnx":        true,
	".ckpt":        true,
	".ggml":        true,
}

// LooksLikeHFRepo reports whether a model path looks like a HuggingFace
// repository ID (e.g. "org/model-name") rather than a local filesystem path.
// It rejects absolute paths, home-relative paths, and paths with known model
// file extensions, but accepts dotted repo IDs like "Qwen/Qwen2.5-7B".
func LooksLikeHFRepo(path string) bool {
	if filepath.IsAbs(path) {
		return false
	}
	if strings.HasPrefix(path, ".") || strings.HasPrefix(path, "~/") {
		return false
	}
	if knownModelExtensions[filepath.Ext(path)] {
		return false // "models/foo.gguf" is a file, not a HF repo
	}
	parts := strings.Split(path, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}
