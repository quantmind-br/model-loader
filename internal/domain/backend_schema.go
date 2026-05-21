package domain

import "time"

// ValidationSchemaKind identifies the schema format.
type ValidationSchemaKind string

const (
	// ValidationSchemaCLIFlagsV1 is the schema kind for CLI flag-based backends
	// (llama-server, vLLM, etc.). Each flag maps to a FlagSpec.
	ValidationSchemaCLIFlagsV1 ValidationSchemaKind = "cli-flags.v1"
)

// SchemaSource tracks how a schema file was produced.
type SchemaSource struct {
	GeneratedFrom string    `json:"generatedFrom,omitempty"`
	GeneratedAt   time.Time `json:"generatedAt,omitempty"`
	SourceVersion string    `json:"sourceVersion,omitempty"`
	Editable      bool      `json:"editable"`
}

// BackendValidationSchema is the persisted schema for a backend.
// It wraps FlagSchema in a versioned envelope so future backends can
// add new schema kinds without coupling the catalog to llama-server.
type BackendValidationSchema struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Kind          ValidationSchemaKind `json:"kind"`
	BackendKind   BackendKind          `json:"backendKind"`
	BackendID     string               `json:"backendId"`
	Source        SchemaSource         `json:"source"`
	Flags         map[string]FlagSpec  `json:"flags,omitempty"`
}

// ToFlagSchema converts the backend validation schema to the legacy
// FlagSchema used by the validator and UI. The Version field carries
// the backend ID and source version for display.
func (s BackendValidationSchema) ToFlagSchema() FlagSchema {
	version := s.BackendID
	if s.Source.SourceVersion != "" {
		version += " (" + s.Source.SourceVersion + ")"
	}
	return FlagSchema{
		Version:     version,
		BackendKind: s.BackendKind,
		Flags:       s.Flags,
	}
}

// FlagSchemaToBackend converts a FlagSchema into a BackendValidationSchema.
// Used when generating a schema file from --help parsing.
func FlagSchemaToBackend(fs FlagSchema, kind BackendKind, backendID string, src SchemaSource) BackendValidationSchema {
	return BackendValidationSchema{
		SchemaVersion: 1,
		Kind:          ValidationSchemaCLIFlagsV1,
		BackendKind:   kind,
		BackendID:     backendID,
		Source:        src,
		Flags:         fs.Flags,
	}
}
