// Package llamahelp parses llama-server --help into a FlagSchema and supplies
// an embedded fallback when the binary is unavailable.
package llamahelp

import (
	"context"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// Parser exposes schema discovery against a real llama-server binary.
type Parser interface {
	Parse(ctx context.Context) (domain.FlagSchema, error)
	DetectVersion(ctx context.Context) (string, error)
}
