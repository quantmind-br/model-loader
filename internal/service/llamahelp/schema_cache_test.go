package llamahelp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSchemaCache_ReturnsCachedSchemaOnSecondCall(t *testing.T) {
	tmp := t.TempDir()
	countPath := filepath.Join(tmp, "count")
	binary := writeFakeLlamaServer(t, tmp, "llama-server", countPath, "ctx-size", false)
	cache := NewSchemaCache(5 * time.Second)

	ctx := context.Background()
	first, err := cache.Get(ctx, binary)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	second, err := cache.Get(ctx, binary)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}

	if _, ok := first.Flags["ctx-size"]; !ok {
		t.Fatal("first schema missing ctx-size")
	}
	if _, ok := second.Flags["ctx-size"]; !ok {
		t.Fatal("second schema missing ctx-size")
	}
	assertHelpRuns(t, countPath, 1)
}

func TestSchemaCache_DifferentBinaryTriggersNewParse(t *testing.T) {
	tmp := t.TempDir()
	firstCount := filepath.Join(tmp, "first-count")
	secondCount := filepath.Join(tmp, "second-count")
	firstBinary := writeFakeLlamaServer(t, tmp, "llama-server-a", firstCount, "ctx-size", false)
	secondBinary := writeFakeLlamaServer(t, tmp, "llama-server-b", secondCount, "batch-size", false)
	cache := NewSchemaCache(5 * time.Second)

	ctx := context.Background()
	first, err := cache.Get(ctx, firstBinary)
	if err != nil {
		t.Fatalf("first binary Get: %v", err)
	}
	second, err := cache.Get(ctx, secondBinary)
	if err != nil {
		t.Fatalf("second binary Get: %v", err)
	}

	if _, ok := first.Flags["ctx-size"]; !ok {
		t.Fatal("first schema missing ctx-size")
	}
	if _, ok := second.Flags["batch-size"]; !ok {
		t.Fatal("second schema missing batch-size")
	}
	assertHelpRuns(t, firstCount, 1)
	assertHelpRuns(t, secondCount, 1)
}

func TestSchemaCache_ParseFailureDoesNotPoisonCache(t *testing.T) {
	tmp := t.TempDir()
	countPath := filepath.Join(tmp, "count")
	binary := writeFakeLlamaServer(t, tmp, "llama-server", countPath, "ctx-size", true)
	cache := NewSchemaCache(5 * time.Second)

	ctx := context.Background()
	if _, err := cache.Get(ctx, binary); err == nil {
		t.Fatal("first Get expected parse failure, got nil")
	}
	schema, err := cache.Get(ctx, binary)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if _, ok := schema.Flags["ctx-size"]; !ok {
		t.Fatal("schema missing ctx-size after retry")
	}
	assertHelpRuns(t, countPath, 2)
}

func writeFakeLlamaServer(t *testing.T, dir, name, countPath, flagName string, failFirstHelp bool) string {
	t.Helper()
	binary := filepath.Join(dir, name)
	failScript := ""
	if failFirstHelp {
		failScript = "if [ \"$n\" -eq 1 ]; then echo fail >&2; exit 42; fi\n"
	}
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "fake llama.cpp v7376"
  exit 0
fi
if [ "$1" = "--help" ]; then
  n=0
  if [ -f %[1]s ]; then n=$(cat %[1]s); fi
  n=$((n + 1))
  printf "%%s" "$n" > %[1]s
  %[2]s
  cat <<'EOF'
----- common params -----
--%[3]s N                              fake flag (default: 1)
EOF
  exit 0
fi
exit 2
`, strconv.Quote(countPath), failScript, flagName)
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	return binary
}

func assertHelpRuns(t *testing.T, countPath string, want int) {
	t.Helper()
	data, err := os.ReadFile(countPath)
	if err != nil {
		t.Fatalf("read count: %v", err)
	}
	got, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse count %q: %v", data, err)
	}
	if got != want {
		t.Fatalf("help runs = %d, want %d", got, want)
	}
}
