# buun-llama-cpp Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `buun-llama-cpp` as a first-class backend kind whose flag schema is generated at runtime from the fork's `llama-server --help`, with a curated embedded fallback and curated essentials.

**Architecture:** New `domain.BackendKindBuunLlamaCpp`. A `BuunServerGenerator` reuses the existing `llamahelp` `--help` parser (the fork's `--help` is format-identical to upstream) and persists the schema; on resolve/parse failure it falls back to a curated embedded schema (`buunhelp`). Args reuse `buildLlamaArgs` (CLI identical to upstream). The generator is registered in bootstrap; the Backends-tab UI picks up the new kind automatically because `kindOptions()` iterates registered generators.

**Tech Stack:** Go 1.26, Charmbracelet bubbletea/huh, standard `testing`.

**Reference facts (verified against the built binary):**
- Binary: `backends/buun-llama-cpp/build/bin/llama-server`, version `9561 (e9187d155)`.
- Confirmed fork flags: `--cache-type-k`/`--cache-type-v` (turbo types), `--spec-draft-model` (`-md`, `--model-draft`, `--draft-model`), `--spec-type`, `--draft-max` (`--draft`/`--draft-n`), `--draft-min` (`--draft-n-min`), `--dflash-max-slots`, `--spec-dflash-default`.
- **NOT present in this build** (do not reference): `--tree-budget`, `--draft-topk`.
- Base llama flags confirmed present: `--n-gpu-layers` (`-ngl`/`--gpu-layers`), `--ctx-size` (`-c`), `--flash-attn` (`-fa`), `--host`, `--port`.

---

### Task 1: Add the BackendKind constant

**Files:**
- Modify: `internal/domain/backend.go:8-14`
- Test: `internal/domain/backend_kind_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `internal/domain/backend_kind_test.go`:

```go
package domain

import "testing"

func TestBackendKindBuunLlamaCppValue(t *testing.T) {
	if BackendKindBuunLlamaCpp != "buun-llama-cpp" {
		t.Fatalf("got %q, want %q", BackendKindBuunLlamaCpp, "buun-llama-cpp")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run TestBackendKindBuunLlamaCppValue`
Expected: FAIL — `undefined: BackendKindBuunLlamaCpp`.

- [ ] **Step 3: Add the constant**

In `internal/domain/backend.go`, add the line inside the existing `const` block (after `BackendKindDFlash`):

```go
const (
	BackendKindLlamaServer BackendKind = "llama-server"
	BackendKindVLLM        BackendKind = "vllm"
	BackendKindTabbyAPI    BackendKind = "tabbyapi"
	BackendKindSGLang      BackendKind = "sglang"
	BackendKindDFlash      BackendKind = "dflash"
	BackendKindBuunLlamaCpp BackendKind = "buun-llama-cpp"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/ -run TestBackendKindBuunLlamaCppValue`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/backend.go internal/domain/backend_kind_test.go
git commit -m "feat(domain): add BackendKindBuunLlamaCpp constant"
```

---

### Task 2: Create the `buunhelp` embedded fallback schema

The fallback = upstream llama embedded base (`llamahelp.EmbeddedSchema()`) merged with the curated fork-specific rows, so curated essentials still validate when the binary is absent.

**Files:**
- Create: `internal/service/buunhelp/buunhelp.go`
- Test: `internal/service/buunhelp/buunhelp_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/service/buunhelp/buunhelp_test.go`:

```go
package buunhelp

import "testing"

func TestEmbeddedSchema_HasForkFlags(t *testing.T) {
	s := EmbeddedSchema()
	for _, name := range []string{
		"cache-type-k", "cache-type-v", "spec-draft-model",
		"spec-dflash-default", "dflash-max-slots", "spec-type",
		"draft-max", "draft-min",
	} {
		if _, ok := s.Lookup(name); !ok {
			t.Errorf("fork flag %q missing from embedded schema", name)
		}
	}
}

func TestEmbeddedSchema_HasBaseLlamaFlags(t *testing.T) {
	s := EmbeddedSchema()
	for _, name := range []string{"n-gpu-layers", "ctx-size", "flash-attn", "port"} {
		if _, ok := s.Lookup(name); !ok {
			t.Errorf("base llama flag %q missing from embedded schema", name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/buunhelp/`
Expected: FAIL — package `buunhelp` does not exist.

- [ ] **Step 3: Write the implementation**

Create `internal/service/buunhelp/buunhelp.go`:

```go
// Package buunhelp provides the embedded fallback schema for the buun-llama-cpp
// backend (a llama.cpp fork). The fork's `--help` is format-identical to
// upstream, so the live schema is normally parsed at runtime by llamahelp; this
// embedded schema is only the degraded fallback used when the binary cannot be
// resolved or parsed. It merges the upstream llama embedded base with the
// curated fork-specific flags (KV-cache turbo types, DFlash, generic spec-type).
package buunhelp

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/llamahelp"
)

const embeddedVersion = "embedded-buun-9561"

// EmbeddedSchema returns the upstream llama embedded base merged with the
// curated buun fork rows. Fork rows override base entries on key collision.
func EmbeddedSchema() domain.FlagSchema {
	base := llamahelp.EmbeddedSchema()
	extra := domain.BuildFlagSchema(embeddedVersion, buunRows)
	for k, v := range extra.Flags {
		base.Flags[k] = v
	}
	base.Version = embeddedVersion
	return base
}

// kvTurboTypes lists the standard llama KV types plus the fork's turbo types.
var kvTurboTypes = []string{
	"f16", "bf16", "q8_0", "q4_0", "q4_1", "q5_0", "q5_1",
	"turbo2", "turbo3", "turbo4", "turbo2_tcq", "turbo3_tcq",
}

var specTypes = []string{
	"none", "draft-simple", "draft-eagle3", "draft-mtp",
	"ngram-simple", "ngram-map-k", "ngram-map-k4v", "ngram-mod",
	"ngram-cache", "suffix", "copyspec", "recycle", "dflash",
}

var buunRows = []domain.FlagSpecRow{
	{Long: "cache-type-k", Short: "ctk", Type: domain.FlagTypeEnum, EnumValues: kvTurboTypes, HelpText: "KV cache data type for K (incl. turbo* fork types)", Group: "embedded"},
	{Long: "cache-type-v", Short: "ctv", Type: domain.FlagTypeEnum, EnumValues: kvTurboTypes, HelpText: "KV cache data type for V (incl. turbo* fork types)", Group: "embedded"},
	{Long: "spec-draft-model", Short: "md", Aliases: []string{"model-draft", "draft-model"}, Type: domain.FlagTypeString, HelpText: "Path to the speculative draft model", Group: "embedded"},
	{Long: "spec-dflash-default", Type: domain.FlagTypeBool, HelpText: "Enable default DFlash speculative decoding config (requires -md)", Group: "embedded"},
	{Long: "dflash-max-slots", Type: domain.FlagTypeInt, Default: 1, Min: iptr(1), Max: iptr(1024), HelpText: "Max concurrent server slots with DFlash state", Group: "embedded"},
	{Long: "spec-type", Type: domain.FlagTypeEnum, EnumValues: specTypes, Default: "none", HelpText: "Speculative decoding strategy", Group: "embedded"},
	{Long: "draft-max", Aliases: []string{"draft", "draft-n"}, Type: domain.FlagTypeInt, Default: 16, Min: iptr(0), Max: iptr(512), HelpText: "Max draft tokens for speculative decoding", Group: "embedded"},
	{Long: "draft-min", Aliases: []string{"draft-n-min"}, Type: domain.FlagTypeInt, Default: 0, Min: iptr(0), Max: iptr(512), HelpText: "Min draft tokens for speculative decoding", Group: "embedded"},
}

func iptr(v int) *int { return &v }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/buunhelp/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/buunhelp/
git commit -m "feat(buunhelp): embedded fallback schema for buun-llama-cpp"
```

---

### Task 3: Extract the shared `--help` parse helper

Pull the parse-and-persist body out of `LlamaServerGenerator.Generate` into a reusable helper so the buun generator does not duplicate it. `LlamaServerGenerator` keeps identical behavior.

**Files:**
- Modify: `internal/service/backendschema/generator.go:55-75`
- Test: `internal/service/backendschema/generator_test.go` (create if absent; otherwise append)

- [ ] **Step 1: Write the failing test**

Create `internal/service/backendschema/parsehelp_test.go`:

```go
package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// parseHelpSchema must reject a binary that cannot be exec'd as --help.
func TestParseHelpSchema_BadBinary(t *testing.T) {
	backend := domain.Backend{
		ID:         "x",
		Kind:       domain.BackendKindLlamaServer,
		Executable: "/nonexistent/definitely-not-a-binary",
	}
	_, err := parseHelpSchema(backend, "/nonexistent/definitely-not-a-binary")
	if err == nil {
		t.Fatal("expected error parsing --help from a missing binary")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/backendschema/ -run TestParseHelpSchema_BadBinary`
Expected: FAIL — `undefined: parseHelpSchema`.

- [ ] **Step 3: Extract the helper and rewire LlamaServerGenerator**

In `internal/service/backendschema/generator.go`, add the helper (place it after the `Generate` method):

```go
// parseHelpSchema runs --help on a resolved binary and converts the parsed
// flags into a backend validation schema. Shared by the llama-server and
// buun-llama-cpp generators (the fork's --help is format-identical).
func parseHelpSchema(backend domain.Backend, resolved string) (domain.BackendValidationSchema, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	parser := llamahelp.NewExecParserFor(resolved)
	fs, err := parser.Parse(ctx)
	if err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("parse --help: %w", err)
	}

	src := domain.SchemaSource{
		GeneratedFrom: backend.Executable,
		GeneratedAt:   time.Now().UTC(),
		SourceVersion: fs.Version,
		Editable:      true,
	}
	return domain.FlagSchemaToBackend(fs, backend.Kind, backend.ID, src), nil
}
```

Then replace the body of `LlamaServerGenerator.Generate` from the `ctx, cancel := ...` line through the `schema := domain.FlagSchemaToBackend(...)` line (current `generator.go:55-71`) with:

```go
	schema, err := parseHelpSchema(backend, resolved)
	if err != nil {
		return domain.BackendValidationSchema{}, err
	}
```

The trailing `g.schemaStore.Save(ref, schema)` block stays unchanged.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/service/backendschema/`
Expected: PASS (the new test plus all existing generator tests, unchanged behavior).

- [ ] **Step 5: Commit**

```bash
git add internal/service/backendschema/generator.go internal/service/backendschema/parsehelp_test.go
git commit -m "refactor(backendschema): extract shared parseHelpSchema helper"
```

---

### Task 4: Create the `BuunServerGenerator`

Runtime `--help` parsing with graceful embedded fallback on resolve/parse failure.

**Files:**
- Create: `internal/service/backendschema/buun_generator.go`
- Test: `internal/service/backendschema/buun_generator_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/service/backendschema/buun_generator_test.go`:

```go
package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestBuunGenerator_WrongKind(t *testing.T) {
	g := NewBuunServerGenerator(nil)
	_, err := g.Generate(domain.Backend{Kind: domain.BackendKindLlamaServer})
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestBuunGenerator_FallsBackWhenBinaryMissing(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewBuunServerGenerator(store)

	backend := domain.Backend{
		ID:         "buun-test",
		Kind:       domain.BackendKindBuunLlamaCpp,
		Executable: "/nonexistent/definitely-not-a-binary",
		SchemaRef:  "schemas/buun-test.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("expected graceful fallback, got error: %v", err)
	}
	if _, ok := schema.ToFlagSchema().Lookup("spec-draft-model"); !ok {
		t.Fatal("fallback schema should contain curated fork flags")
	}
}

func TestBuunGenerator_SkipsWhenEditable(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewBuunServerGenerator(store)
	backend := domain.Backend{
		ID:         "buun-edit",
		Kind:       domain.BackendKindBuunLlamaCpp,
		Executable: "/nonexistent/definitely-not-a-binary",
		SchemaRef:  "schemas/buun-edit.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	schema.Source.Editable = true
	_ = store.Save("buun-edit.json", schema)
	schema2, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if !schema2.Source.Editable {
		t.Fatal("expected editable schema to be preserved")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/backendschema/ -run TestBuunGenerator`
Expected: FAIL — `undefined: NewBuunServerGenerator`.

- [ ] **Step 3: Write the implementation**

Create `internal/service/backendschema/buun_generator.go`:

```go
package backendschema

import (
	"fmt"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/buunhelp"
	"github.com/quantmind-br/model-loader/internal/service/llamabin"
)

// BuunServerGenerator generates schemas for the buun-llama-cpp fork. The fork's
// `--help` is format-identical to upstream llama.cpp, so the live schema is
// parsed at runtime via the shared parseHelpSchema helper. When the binary
// cannot be resolved or parsed, it falls back to the curated embedded schema.
type BuunServerGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

// NewBuunServerGenerator returns a generator that writes schemas to store.
func NewBuunServerGenerator(schemaStore backendcatalog.SchemaStore) *BuunServerGenerator {
	return &BuunServerGenerator{schemaStore: schemaStore}
}

// Generate parses --help when possible, else writes the embedded fallback.
func (g *BuunServerGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindBuunLlamaCpp {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Editable {
		return existing, nil
	}

	schema := g.resolveSchema(backend)

	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}

// resolveSchema tries runtime --help parsing and falls back to the embedded
// curated schema on any resolve/parse failure.
func (g *BuunServerGenerator) resolveSchema(backend domain.Backend) domain.BackendValidationSchema {
	if resolved, err := llamabin.Resolve(backend.Executable); err == nil {
		if parsed, perr := parseHelpSchema(backend, resolved); perr == nil {
			return parsed
		}
	}
	return buunFallbackSchema(backend)
}

func buunFallbackSchema(backend domain.Backend) domain.BackendValidationSchema {
	fs := buunhelp.EmbeddedSchema()
	src := domain.SchemaSource{
		GeneratedFrom: "embedded-fallback",
		GeneratedAt:   time.Now().UTC(),
		SourceVersion: fs.Version,
		Editable:      true,
	}
	return domain.FlagSchemaToBackend(fs, backend.Kind, backend.ID, src)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/service/backendschema/ -run TestBuunGenerator`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/backendschema/buun_generator.go internal/service/backendschema/buun_generator_test.go
git commit -m "feat(backendschema): add BuunServerGenerator with embedded fallback"
```

---

### Task 5: Register the generator in bootstrap

**Files:**
- Modify: `cmd/model-loader/bootstrap.go:98-101`

- [ ] **Step 1: Add the registration line**

In `cmd/model-loader/bootstrap.go`, after the existing `schemaManager.Register(domain.BackendKindDFlash, ...)` line, add:

```go
	schemaManager.Register(domain.BackendKindBuunLlamaCpp, backendschema.NewBuunServerGenerator(schemaStore))
```

- [ ] **Step 2: Build to verify wiring**

Run: `go build ./...`
Expected: builds with no errors.

- [ ] **Step 3: Commit**

```bash
git add cmd/model-loader/bootstrap.go
git commit -m "feat(bootstrap): register buun-llama-cpp schema generator"
```

---

### Task 6: Wire arg construction for the buun kind

The fork's CLI is identical to upstream llama-server, so buun reuses `buildLlamaArgs`.

**Files:**
- Modify: `internal/service/processmgr/args.go:26-39`
- Test: `internal/service/processmgr/buun_kind_resolve_test.go` (create)

- [ ] **Step 1: Write the failing test**

Create `internal/service/processmgr/buun_kind_resolve_test.go`:

```go
package processmgr

import (
	"path/filepath"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// TestPrepareLaunch_BuunEmitsModelFlag mirrors the dflash kind-resolution guard:
// the buun-llama-cpp backend shares llama-server's CLI, so it must emit
// "--model <path>" first, exactly like llama-server.
func TestPrepareLaunch_BuunEmitsModelFlag(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)

	mgr := New(Config{
		Resolver: func(_ domain.Profile) (string, domain.BackendKind, error) {
			return "/bin/echo", domain.BackendKindBuunLlamaCpp, nil
		},
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "i.json"),
	})
	t.Cleanup(func() { _ = mgr.Close() })

	p := domain.Profile{
		ID:    "buun-test",
		Model: "/models/target.gguf",
		Args:  map[string]any{"port": float64(port)},
	}

	plan, err := mgr.prepareLaunch(p)
	if err != nil {
		t.Fatalf("prepareLaunch: %v", err)
	}
	if len(plan.args) < 2 || plan.args[0] != "--model" || plan.args[1] != p.Model {
		t.Fatalf("expected args to begin with --model %s, got %v", p.Model, plan.args)
	}
}

func TestBuildArgsForBackend_Buun(t *testing.T) {
	p := domain.Profile{Model: "/m.gguf", Args: map[string]any{"ctx-size": 8192}}
	args, err := BuildArgsForBackend(p, domain.BackendKindBuunLlamaCpp, "/bin/echo")
	if err != nil {
		t.Fatalf("BuildArgsForBackend: %v", err)
	}
	if len(args) < 2 || args[0] != "--model" || args[1] != "/m.gguf" {
		t.Fatalf("expected --model first, got %v", args)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/processmgr/ -run 'Buun'`
Expected: FAIL — `BuildArgsForBackend` returns "unsupported backend kind: buun-llama-cpp".

- [ ] **Step 3: Add the dispatch case**

In `internal/service/processmgr/args.go`, add a case to `BuildArgsForBackend` immediately after the `case domain.BackendKindDFlash:` case (before `default:`):

```go
	case domain.BackendKindBuunLlamaCpp:
		return buildLlamaArgs(p), nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/service/processmgr/ -run 'Buun'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/processmgr/args.go internal/service/processmgr/buun_kind_resolve_test.go
git commit -m "feat(processmgr): build args for buun-llama-cpp via llama builder"
```

---

### Task 7: Add curated essentials for the buun kind

**Files:**
- Modify: `internal/ui/pages/profile_editor/essentials.go:53-63`
- Test: `internal/ui/pages/profile_editor/essentials_test.go` (create if absent; otherwise append)

- [ ] **Step 1: Write the failing test**

Create `internal/ui/pages/profile_editor/buun_essentials_test.go`:

```go
package profile_editor

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/buunhelp"
)

func TestEssentials_BuunAgainstEmbeddedSchema(t *testing.T) {
	schema := buunhelp.EmbeddedSchema()
	got := essentialsFor(domain.BackendKindBuunLlamaCpp, schema)

	// Every curated buun essential must resolve against the embedded schema,
	// otherwise it would be silently dropped from the editor.
	want := []string{
		"n-gpu-layers", "ctx-size", "flash-attn", "port",
		"cache-type-k", "cache-type-v",
		"spec-draft-model", "spec-dflash-default", "dflash-max-slots",
		"spec-type", "draft-max", "draft-min",
	}
	have := make(map[string]bool, len(got))
	for _, f := range got {
		have[f.Flag] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("essential %q dropped (not found in embedded schema)", w)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/pages/profile_editor/ -run TestEssentials_BuunAgainstEmbeddedSchema`
Expected: FAIL — `essentialsFor` falls back to llama essentials (no buun entry), so fork-specific flags like `spec-draft-model` are missing.

- [ ] **Step 3: Add the essentials entry**

In `internal/ui/pages/profile_editor/essentials.go`, add a new map entry after the `domain.BackendKindDFlash` block (before the closing `}` of `essentialFields`):

```go
	domain.BackendKindBuunLlamaCpp: {
		{Flag: "n-gpu-layers", Label: "ngl (gpu layers)", Description: "Number of GPU layers to offload", Min: iptr(-1), Max: iptr(9999), Default: "99"},
		{Flag: "ctx-size", Label: "ctx-size", Description: "Context window size in tokens", Min: iptr(0), Max: iptr(1024 * 1024), Default: "8192"},
		{Flag: "flash-attn", Label: "flash-attn", Description: "Flash Attention mode", Default: "auto", Coerce: FlashAttnToString},
		{Flag: "port", Label: "port", Description: "Port to bind the inference server", IsPort: true, Default: "8080"},
		{Flag: "cache-type-k", Label: "cache-type-k", Description: "K cache type (turbo2/3/4, turbo*_tcq, or standard)", Default: "f16"},
		{Flag: "cache-type-v", Label: "cache-type-v", Description: "V cache type (turbo2/3/4, turbo*_tcq, or standard)", Default: "f16"},
		{Flag: "spec-type", Label: "spec-type", Description: "Speculative decoding strategy (dflash/copyspec/ngram-*/...)", Default: "none"},
		{Flag: "spec-draft-model", Label: "spec-draft-model (-md)", Description: "Path to the speculative draft model"},
		{Flag: "spec-dflash-default", Label: "spec-dflash-default", Description: "Enable default DFlash config (requires draft model)"},
		{Flag: "dflash-max-slots", Label: "dflash-max-slots", Description: "Max concurrent server slots with DFlash state", Min: iptr(1), Max: iptr(1024), Default: "1"},
		{Flag: "draft-max", Label: "draft-max", Description: "Max draft tokens per step", Min: iptr(0), Max: iptr(512), Default: "16"},
		{Flag: "draft-min", Label: "draft-min", Description: "Min draft tokens per step", Min: iptr(0), Max: iptr(512), Default: "0"},
	},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/pages/profile_editor/ -run TestEssentials_BuunAgainstEmbeddedSchema`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/pages/profile_editor/essentials.go internal/ui/pages/profile_editor/buun_essentials_test.go
git commit -m "feat(profile_editor): curated essentials for buun-llama-cpp"
```

---

### Task 8: Full verification against the live binary

End-to-end check: build, full test suite, gofmt, and a real schema-generation smoke test against the actual fork binary.

**Files:** none (verification only)

- [ ] **Step 1: gofmt the touched packages**

Run: `gofmt -w internal/domain internal/service/buunhelp internal/service/backendschema internal/service/processmgr internal/ui/pages/profile_editor cmd/model-loader`
Expected: no diff beyond intended changes.

- [ ] **Step 2: Build and run the full suite**

Run: `make build && go test ./...`
Expected: build succeeds; all tests pass.

- [ ] **Step 3: Smoke-test live schema generation**

Run:
```bash
go test ./internal/service/backendschema/ -run TestBuunGenerator -v
```
Expected: PASS. Then optionally confirm the live binary still parses (manual, not committed):
```bash
backends/buun-llama-cpp/build/bin/llama-server --help | head -1
```
Expected: prints usage; version `9561 (e9187d155)` family.

- [ ] **Step 4: Confirm profile-schema.json needs no change**

Run: `grep -c '"enum"' docs/profile-schema.json && grep -n 'kind' docs/profile-schema.json`
Expected: backend `kind` is not constrained by an `enum`, so no edit is needed. (If a future enum is added, add `buun-llama-cpp` there.)

- [ ] **Step 5: Final commit (if gofmt produced changes)**

```bash
git add -A
git commit -m "chore(buun): gofmt and final verification"
```

---

## Self-review notes

- **Spec coverage:** §1 domain → Task 1; §2 generator + fallback → Tasks 2-4; §3 bootstrap → Task 5; §4 args → Task 6; §6 essentials → Task 7; §7 UI (no change, auto-picked-up) + validator (no change) → noted, verified in Task 8; §testing → tests embedded in each task + Task 8.
- **Correction vs spec:** `--tree-budget` and `--draft-topk` were dropped — they do not exist in build 9561. Essentials use only verified flags.
- **Refactor scope:** Task 3 keeps `LlamaServerGenerator` behavior identical; only extracts a helper. Buun adds graceful fallback (distinct from llama's error-on-failure) because the spec requires a functional degraded mode.
- **No persisted-shape change:** `domain.Profile` is untouched, so `docs/profile-schema.json` stays in sync (verified in Task 8 Step 4).
