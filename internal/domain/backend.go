package domain

import "time"

// BackendKind identifies the family of LLM server implementation.
type BackendKind string

const (
	BackendKindLlamaServer BackendKind = "llama-server"
	BackendKindVLLM        BackendKind = "vllm"
	BackendKindTabbyAPI    BackendKind = "tabbyapi"
	BackendKindSGLang      BackendKind = "sglang"
	BackendKindDFlash      BackendKind = "dflash"
)

// BackendMeta holds timestamps and bookkeeping for a backend entry.
type BackendMeta struct {
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	GeneratedAt   *time.Time `json:"generatedAt,omitempty"`
	SourceVersion string     `json:"sourceVersion,omitempty"`
}

// Backend represents a single LLM server backend in the catalog.
type Backend struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Kind        BackendKind       `json:"kind"`
	Executable  string            `json:"executable"`
	SchemaRef   string            `json:"schemaRef"`
	Description string            `json:"description,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Meta        BackendMeta       `json:"meta"`
	Properties  map[string]string `json:"properties,omitempty"`
}

// BackendCatalog is the persisted catalog of backends.
type BackendCatalog struct {
	SchemaVersion    int       `json:"schemaVersion"`
	DefaultBackendID string    `json:"defaultBackendId,omitempty"`
	Backends         []Backend `json:"backends"`
}
