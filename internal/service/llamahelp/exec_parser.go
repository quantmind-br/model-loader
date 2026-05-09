package llamahelp

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/quantmind-br/llama-cpp-loader/internal/domain"
	"github.com/quantmind-br/llama-cpp-loader/internal/service/llamabin"
)

// ExecParser invokes llama-server in PATH to capture --help and --version.
type ExecParser struct {
	binary string
}

// NewExecParser returns an ExecParser that resolves "llama-server" via PATH.
func NewExecParser() *ExecParser {
	return NewExecParserFor(llamabin.DefaultName)

}

// NewExecParserFor returns an ExecParser that invokes binary directly.
func NewExecParserFor(binary string) *ExecParser {
	return &ExecParser{binary: binary}
}

func (p *ExecParser) Parse(ctx context.Context) (domain.FlagSchema, error) {
	cmd := exec.CommandContext(ctx, p.binary, "--help")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return domain.FlagSchema{}, fmt.Errorf("%s --help: %w (stderr: %s)", p.binary, err, stderr.String())
	}
	combined := append(stdout.Bytes(), stderr.Bytes()...)

	schema, err := ParseHelp(combined)
	if err != nil {
		return domain.FlagSchema{}, fmt.Errorf("ParseHelp: %w", err)
	}
	if v, verr := p.DetectVersion(ctx); verr == nil {
		schema.Version = v
	}
	return schema, nil
}

func (p *ExecParser) DetectVersion(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, p.binary, "--version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s --version: %w", p.binary, err)
	}
	return strings.TrimSpace(out.String()), nil
}
