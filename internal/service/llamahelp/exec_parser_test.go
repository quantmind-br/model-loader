package llamahelp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecParser_ParseUsesPATH(t *testing.T) {
	// Symlink fake script as `llama-server` in a temp dir, prepend to PATH.
	tmp := t.TempDir()
	src, err := filepath.Abs("../../../testdata/fake-llama-help.sh")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	dst := filepath.Join(tmp, "llama-server")
	if err := os.Symlink(src, dst); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Sanity: exec.LookPath resolves to our fake.
	if got, err := exec.LookPath("llama-server"); err != nil || !strings.HasPrefix(got, tmp) {
		t.Fatalf("LookPath = %q, err=%v; want path under %q", got, err, tmp)
	}

	parser := NewExecParser()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	schema, err := parser.Parse(ctx)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := schema.Flags["ctx-size"]; !ok {
		t.Errorf("missing ctx-size in schema parsed via fake binary")
	}
	if v, err := parser.DetectVersion(ctx); err != nil || !strings.Contains(v, "10686") {
		t.Errorf("DetectVersion = %q, err=%v; want substring 10686", v, err)
	}
}
