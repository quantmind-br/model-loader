// Package modelscanner walks configured filesystem paths and emits
// ScanEvent values describing GGUF model files found, with best-effort
// metadata (quant from filename, parameter count from GGUF header).
package modelscanner

import (
	"context"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// Scanner discovers GGUF files under the given root paths.
// Scan returns a buffered channel that the caller must drain until it
// closes. The channel closes after a single ScanEventDone is emitted,
// or earlier if ctx is cancelled.
type Scanner interface {
	Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error)
}
