package backendschema

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

// RegisterDefaults registers the built-in schema generators for every backend
// kind that ships with one. Both the app bootstrap and the CLI call this so the
// set of generators lives in exactly one place and cannot drift between them.
func RegisterDefaults(m *Manager, schemaStore backendcatalog.SchemaStore) {
	m.Register(domain.BackendKindLlamaServer, NewLlamaServerGenerator(schemaStore))
	m.Register(domain.BackendKindSGLang, NewSGLangGenerator(schemaStore))
	m.Register(domain.BackendKindVLLM, NewVLLMGenerator(schemaStore))
	m.Register(domain.BackendKindDFlash, NewDFlashGenerator(schemaStore))
	m.Register(domain.BackendKindBuunLlamaCpp, NewBuunServerGenerator(schemaStore))
}
