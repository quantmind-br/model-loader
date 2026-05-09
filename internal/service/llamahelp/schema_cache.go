package llamahelp

import (
	"context"
	"sync"
	"time"

	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/llamabin"
)

// SchemaCache caches parsed llama-server schemas per resolved binary path.
type SchemaCache struct {
	mu      sync.Mutex
	schemas map[string]domain.FlagSchema
	parsers map[string]*ExecParser
	timeout time.Duration
}

// NewSchemaCache returns an in-memory cache with per-parse timeout.
func NewSchemaCache(timeout time.Duration) *SchemaCache {
	return &SchemaCache{
		schemas: make(map[string]domain.FlagSchema),
		parsers: make(map[string]*ExecParser),
		timeout: timeout,
	}
}

// Get returns the parsed schema for binary, caching successful parses only.
func (c *SchemaCache) Get(ctx context.Context, binary string) (domain.FlagSchema, error) {
	resolved, err := llamabin.Resolve(binary)
	if err != nil {
		return domain.FlagSchema{}, err
	}

	c.mu.Lock()
	if schema, ok := c.schemas[resolved]; ok {
		c.mu.Unlock()
		return schema, nil
	}
	parser, ok := c.parsers[resolved]
	if !ok {
		parser = NewExecParserFor(resolved)
		c.parsers[resolved] = parser
	}
	c.mu.Unlock()

	parseCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	schema, err := parser.Parse(parseCtx)
	if err != nil {
		return domain.FlagSchema{}, err
	}

	c.mu.Lock()
	c.schemas[resolved] = schema
	c.mu.Unlock()
	return schema, nil
}
