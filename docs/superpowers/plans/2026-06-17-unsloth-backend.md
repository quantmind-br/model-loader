# Unsloth Backend Support — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a first-class `unsloth` backend kind that launches `unsloth studio run` headless, captures its per-boot `sk-unsloth-…` key from the launch log, and injects it as an outbound Bearer token in the httpproxy.

**Architecture:** Mirror the embedded-schema backends (vllm/sglang) for the kind/schema/args/launcher wiring. The one novel piece is a kind-aware readiness step: for unsloth, processmgr scans the instance log for the printed key (which appears only once the model is loaded), returning it as an auth token that the proxy injects on every upstream request to Studio.

**Tech Stack:** Go 1.26, Charmbracelet bubbletea (unaffected), standard library `net/http/httputil` reverse proxy, bash launcher script. unsloth 2026.6.7 CLI (`~/.local/bin/unsloth`), Studio at `~/.unsloth/studio`.

## Global Constraints

- All UI/UX text and all config schema field names MUST be in **English** (CLAUDE.md LANGUAGE RULES).
- **NEVER** set/keep `port` in user args — the process manager owns it (`launch.go` injects `args["port"]`). The schema may list `port` with `IsPort:true` (display/validation only), like sglang.
- **NEVER** hand-edit schema JSON; schemas are generated. Curated flag lists live in Go (`unslothhelp`), never in JSON.
- Service conventions: `var ErrX = errors.New(...)` sentinels (no dynamic `fmt.Errorf` for sentinels); `log.Nop()` fallback; never pass `nil` logger.
- Module path: `github.com/quantmind-br/model-loader`.
- Tests: table-driven; `make tests` (golden + unit) must pass; golden fixtures via `go test ./... -update` (none expected to change here).
- Build/test commands: `make build`, `make tests`, `go test ./internal/service/<pkg>/...`.
- Branch: `feat/unsloth-backend` (already created; spec committed there).

## Verified facts the code depends on

- `unsloth studio run` (alias `unsloth run`) blocks foreground, re-execs via `os.execvp` (PID preserved), serves OpenAI/Anthropic on `--port`, defaults `-H 127.0.0.1`, GGUF-only, accepts local GGUF path or HF repo for `--model`.
- The key prints to stdout (even with `--silent`) as `API Key:      sk-unsloth-<32hex>`, **after** the model loads. Format of the token: `sk-unsloth-` + 32 lowercase hex chars.
- Studio-managed (do NOT expose as schema flags or pass as extras): `--host/--port/--path`, `--api-key*`, `--model/-hf/-m`, `-np/--parallel` (raw), `--ui/--webui`, `--embedding/--rerank`, `--tools`.
- model-loader: `prepareLaunch` resolves `resolvedKind`; `Launch`/`launchForeground` build `RunningInstance` (has `LogPath`); httpproxy `launchNewBackend` calls `WaitHealthy` then builds `newReverseProxy(port)`.

## File structure

| File | Create/Modify | Responsibility |
|---|---|---|
| `internal/domain/backend.go` | Modify | add `BackendKindUnsloth` |
| `internal/domain/instance.go` | Modify | add `RunningInstance.Kind` |
| `internal/service/unslothhelp/unslothhelp.go` | Create | curated `EmbeddedSchema() domain.FlagSchema` |
| `internal/service/unslothhelp/unslothhelp_test.go` | Create | schema sanity test |
| `internal/service/backendschema/unsloth_generator.go` | Create | `UnslothGenerator` (mirrors `VLLMGenerator`) |
| `internal/service/backendschema/unsloth_generator_test.go` | Create | wrong-kind + skip-when-editable |
| `internal/service/backendschema/register.go` | Modify | register the generator |
| `internal/service/backendschema/register_test.go` | Modify | expect the new kind |
| `internal/service/backendschema/presentation.go` | Modify | `essentialSeed` entry |
| `internal/service/processmgr/args.go` | Modify | `buildUnslothArgs` + switch case |
| `internal/service/processmgr/args_test.go` | Modify | unsloth arg test |
| `internal/service/processmgr/launch.go` | Modify | thread kind into `launchPlan`, set `inst.Kind` |
| `internal/service/processmgr/processmgr.go` | Modify | add `WaitReady` to interface + `ErrReadyTimeout` |
| `internal/service/processmgr/readiness.go` | Create | `WaitReady` + `waitForLogToken` |
| `internal/service/processmgr/readiness_test.go` | Create | log-token scan tests |
| `internal/service/httpproxy/proxy.go` | Modify | `newReverseProxy(port, authToken)` + Director |
| `internal/service/httpproxy/proxy_test.go` | Create | upstream Authorization injection test |
| `internal/service/httpproxy/server.go` | Modify | `loadedBackend.authToken` |
| `internal/service/httpproxy/handler.go` | Modify | use `WaitReady`, pass token |
| `internal/service/httpproxy/mocks_test.go` | Modify | stub `WaitReady` + `readyToken` |
| `internal/service/validator/rules.go` | Modify | `supportsHFRepo` += unsloth |
| `internal/service/validator/rules_test.go` | Modify (or add) | unsloth HF-repo accepted |
| `backends/unsloth/unsloth-serve.sh` | Create | headless launcher |
| `.agents/skills/rtx3090-inference-profiles/SKILL.md` | Modify | dispatch row for unsloth |
| `.agents/skills/rtx3090-inference-profiles/references/llama-family.md` | Modify | Unsloth subsection |

---

### Task 1: `unsloth` kind, curated schema, generator, registration, essentials

**Files:**
- Modify: `internal/domain/backend.go`
- Create: `internal/service/unslothhelp/unslothhelp.go`
- Create: `internal/service/unslothhelp/unslothhelp_test.go`
- Create: `internal/service/backendschema/unsloth_generator.go`
- Create: `internal/service/backendschema/unsloth_generator_test.go`
- Modify: `internal/service/backendschema/register.go`
- Modify: `internal/service/backendschema/register_test.go`
- Modify: `internal/service/backendschema/presentation.go`

**Interfaces:**
- Produces: `domain.BackendKindUnsloth domain.BackendKind = "unsloth"`; `unslothhelp.EmbeddedSchema() domain.FlagSchema`; `backendschema.NewUnslothGenerator(schemaStore backendcatalog.SchemaStore) *UnslothGenerator` implementing `Generate(domain.Backend) (domain.BackendValidationSchema, error)`.
- Consumes: `domain.BuildFlagSchema`, `domain.FlagSchemaToBackend`, `domain.SchemaSource`, `schemaStoreRef`, `ptrutil.Ptr`.

- [ ] **Step 1: Add the kind constant.**

In `internal/domain/backend.go`, add to the `const (...)` block (after `BackendKindBeeLlamaCpp`):

```go
	BackendKindUnsloth      BackendKind = "unsloth"
```

- [ ] **Step 2: Write the unslothhelp schema package.**

Create `internal/service/unslothhelp/unslothhelp.go`:

```go
// Package unslothhelp provides a static, curated schema for the unsloth
// backend. `unsloth studio run` is a Python wrapper around llama-server whose
// --help is not stable enough to parse, so we ship a hand-curated schema of
// the unsloth orchestration flags plus the high-value llama-server pass-through
// flags. Studio-managed flags (host, api-key, model identity, raw -np, UI,
// embedding/rerank, tools) are intentionally absent.
package unslothhelp

import (
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/ptrutil"
)

// EmbeddedSchema returns the curated unsloth schema. `model` is supplied from
// the profile's Model field (emitted as --model), so it is not a row here.
// `port` carries IsPort so the validator knows the manager owns it.
func EmbeddedSchema() domain.FlagSchema {
	return domain.BuildFlagSchema("embedded-unsloth-v1", unslothRows)
}

var unslothRows = []domain.FlagSpecRow{
	// Networking (manager-owned; host is forced to loopback by the wrapper).
	{Long: "port", Type: domain.FlagTypeInt, Default: float64(8888), IsPort: true, HelpText: "Server port (assigned by the process manager)", Group: "common"},

	// Unsloth orchestration flags.
	{Long: "gguf-variant", Type: domain.FlagTypeString, Default: "", HelpText: "GGUF quant variant to fetch (e.g. UD-Q4_K_XL); ignored for local GGUF paths", Group: "common"},
	{Long: "max-seq-length", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1048576), HelpText: "Max sequence length unsloth advertises (0 = model default)", Group: "common"},
	{Long: "parallel", Type: domain.FlagTypeInt, Default: float64(1), Min: ptrutil.Ptr(1), Max: ptrutil.Ptr(256), HelpText: "Decode slots; N requests share one model, each gets ctx/N KV", Group: "common"},
	{Long: "tensor-parallel", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable unsloth tensor parallelism", Group: "common"},
	{Long: "enable-tools", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable Studio tool calling (web search, code, bash)", Group: "common"},
	{Long: "load-in-4bit", Type: domain.FlagTypeBool, Default: true, HelpText: "Load weights in 4-bit (unsloth)", Group: "common"},

	// llama-server pass-through (last-wins parser; unsloth appends these).
	{Long: "ctx-size", Short: "c", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1048576), HelpText: "Context window size (0 = model default)", Group: "common"},
	{Long: "n-gpu-layers", Short: "ngl", Type: domain.FlagTypeInt, Default: float64(0), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1000), HelpText: "Layers to offload to GPU (999 = all)", Group: "common"},
	{Long: "flash-attn", Type: domain.FlagTypeBool, Default: false, HelpText: "Enable flash attention (bare flag; use extraArgs if the bundled build needs on/off/auto)", Group: "common"},
	{Long: "cache-type-k", Type: domain.FlagTypeEnum, EnumValues: []string{"f16", "bf16", "q8_0", "q5_1", "q5_0", "q4_1", "q4_0"}, Default: "", HelpText: "KV cache K type", Group: "common"},
	{Long: "cache-type-v", Type: domain.FlagTypeEnum, EnumValues: []string{"f16", "bf16", "q8_0", "q5_1", "q5_0", "q4_1", "q4_0"}, Default: "", HelpText: "KV cache V type", Group: "common"},
	{Long: "jinja", Type: domain.FlagTypeBool, Default: false, HelpText: "Use the model's Jinja chat template", Group: "common"},
	{Long: "chat-template-file", Type: domain.FlagTypeString, Default: "", HelpText: "Path to a chat template (.jinja) file", Group: "common"},

	// Sampling pass-through.
	{Long: "temp", Type: domain.FlagTypeFloat, Default: float64(0.8), FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(2.0), HelpText: "Sampling temperature", Group: "sampling"},
	{Long: "top-p", Type: domain.FlagTypeFloat, Default: float64(0.95), FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(1.0), HelpText: "Top-p (nucleus) sampling", Group: "sampling"},
	{Long: "top-k", Type: domain.FlagTypeInt, Default: float64(40), Min: ptrutil.Ptr(0), Max: ptrutil.Ptr(1000), HelpText: "Top-k sampling", Group: "sampling"},
	{Long: "min-p", Type: domain.FlagTypeFloat, Default: float64(0.05), FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(1.0), HelpText: "Min-p sampling", Group: "sampling"},
	{Long: "repeat-penalty", Type: domain.FlagTypeFloat, Default: float64(1.0), FloatMin: ptrutil.Ptr(0.0), FloatMax: ptrutil.Ptr(2.0), HelpText: "Repetition penalty", Group: "sampling"},
	{Long: "seed", Type: domain.FlagTypeInt, Default: float64(-1), HelpText: "RNG seed (-1 = random)", Group: "sampling"},
}
```

- [ ] **Step 3: Write the unslothhelp test.**

Create `internal/service/unslothhelp/unslothhelp_test.go`:

```go
package unslothhelp

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestEmbeddedSchema_HasKeyFlagsAndNoManagedFlags(t *testing.T) {
	fs := EmbeddedSchema()
	if fs.Version != "embedded-unsloth-v1" {
		t.Fatalf("version = %q", fs.Version)
	}
	for _, want := range []string{"gguf-variant", "ctx-size", "n-gpu-layers", "parallel", "port"} {
		if _, ok := fs.Flags[want]; !ok {
			t.Errorf("missing expected flag %q", want)
		}
	}
	if spec, ok := fs.Flags["port"]; !ok || !spec.IsPort {
		t.Errorf("port must exist with IsPort=true")
	}
	// Studio-managed flags must NOT be user-editable schema rows.
	for _, banned := range []string{"host", "model", "api-key", "hf-repo"} {
		if _, ok := fs.Flags[banned]; ok {
			t.Errorf("Studio-managed flag %q must not be in the schema", banned)
		}
	}
	_ = domain.FlagTypeString
}
```

- [ ] **Step 4: Run the test to verify it fails (package/flags not yet present).**

Run: `go test ./internal/service/unslothhelp/...`
Expected: FAIL to compile or PASS once Step 2 file exists — if Step 2 is in place it PASSES. (Acceptable; the meaningful red→green is the generator + register tests below.)

- [ ] **Step 5: Write the generator.**

Create `internal/service/backendschema/unsloth_generator.go`:

```go
package backendschema

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/unslothhelp"
)

// UnslothGenerator generates schemas from the hand-curated unsloth flag catalog.
type UnslothGenerator struct {
	schemaStore backendcatalog.SchemaStore
}

// NewUnslothGenerator returns a generator that writes schemas to store.
func NewUnslothGenerator(schemaStore backendcatalog.SchemaStore) *UnslothGenerator {
	return &UnslothGenerator{schemaStore: schemaStore}
}

// Generate returns the curated unsloth schema without runtime --help parsing.
// If the existing schema has source.editable=true, generation is skipped to
// preserve manual edits (web Customize mode).
func (g *UnslothGenerator) Generate(backend domain.Backend) (domain.BackendValidationSchema, error) {
	if backend.Kind != domain.BackendKindUnsloth {
		return domain.BackendValidationSchema{}, fmt.Errorf("unsupported backend kind: %s", backend.Kind)
	}

	ref := schemaStoreRef(backend.SchemaRef)
	existing, err := g.schemaStore.Load(ref)
	if err == nil && existing.Source.Editable {
		return existing, nil
	}

	schema := domain.FlagSchemaToBackend(
		unslothhelp.EmbeddedSchema(),
		domain.BackendKindUnsloth,
		backend.ID,
		domain.SchemaSource{GeneratedFrom: backend.Executable},
	)
	if err := g.schemaStore.Save(ref, schema); err != nil {
		return domain.BackendValidationSchema{}, fmt.Errorf("save schema: %w", err)
	}
	return schema, nil
}
```

- [ ] **Step 6: Write the generator test.**

Create `internal/service/backendschema/unsloth_generator_test.go`:

```go
package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func TestUnslothGenerator_WrongKind(t *testing.T) {
	g := NewUnslothGenerator(nil)
	_, err := g.Generate(domain.Backend{Kind: domain.BackendKindLlamaServer})
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestUnslothGenerator_GeneratesAndSkipsWhenEditable(t *testing.T) {
	dir := t.TempDir()
	store := backendcatalog.NewFSSchemaStore(dir)
	g := NewUnslothGenerator(store)
	backend := domain.Backend{
		ID:         "unsloth-test",
		Kind:       domain.BackendKindUnsloth,
		Executable: "echo",
		SchemaRef:  "schemas/unsloth-test.json",
	}
	schema, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if schema.BackendKind != domain.BackendKindUnsloth {
		t.Fatalf("backendKind = %q", schema.BackendKind)
	}
	if _, ok := schema.Flags["gguf-variant"]; !ok {
		t.Fatal("expected gguf-variant flag in generated schema")
	}
	schema.Source.Editable = true
	_ = store.Save("unsloth-test.json", schema)
	schema2, err := g.Generate(backend)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if !schema2.Source.Editable {
		t.Fatal("expected editable schema to be preserved")
	}
}
```

- [ ] **Step 7: Register the generator.**

In `internal/service/backendschema/register.go`, add after the `BeeLlamaCpp` line inside `RegisterDefaults`:

```go
	m.Register(domain.BackendKindUnsloth, NewUnslothGenerator(schemaStore))
```

- [ ] **Step 8: Update the register test.**

In `internal/service/backendschema/register_test.go`, add to the `want` slice:

```go
		domain.BackendKindUnsloth,
```

- [ ] **Step 9: Add the presentation essentials.**

In `internal/service/backendschema/presentation.go`, add to the `essentialSeed` map:

```go
	domain.BackendKindUnsloth:      {"gguf-variant", "ctx-size", "n-gpu-layers", "parallel", "flash-attn", "cache-type-k", "cache-type-v"},
```

- [ ] **Step 10: Run the package tests to verify green.**

Run: `go test ./internal/service/unslothhelp/... ./internal/service/backendschema/...`
Expected: PASS (register test now counts 7 kinds; generator tests pass).

- [ ] **Step 11: Commit.**

```bash
git add internal/domain/backend.go internal/service/unslothhelp/ internal/service/backendschema/unsloth_generator.go internal/service/backendschema/unsloth_generator_test.go internal/service/backendschema/register.go internal/service/backendschema/register_test.go internal/service/backendschema/presentation.go
git commit -m "feat(unsloth): add unsloth kind, curated schema, generator, registration"
```

---

### Task 2: Launch argument builder for the unsloth kind

**Files:**
- Modify: `internal/service/processmgr/args.go`
- Modify: `internal/service/processmgr/args_test.go`

**Interfaces:**
- Consumes: `buildArgs(p, argBuildOpts)`, `domain.BackendKindUnsloth`.
- Produces: `BuildArgsForBackend(p, domain.BackendKindUnsloth, exe)` emits `--model <p.Model>` then sorted `--<key> <value>` flags.

- [ ] **Step 1: Write the failing test.**

In `internal/service/processmgr/args_test.go`, add:

```go
func TestBuildArgsForBackend_Unsloth(t *testing.T) {
	p := domain.Profile{
		Model: "unsloth/Qwen3-1.7B-GGUF",
		Args: map[string]any{
			"gguf-variant": "UD-Q4_K_XL",
			"ctx-size":     float64(8192),
			"port":         8123,
		},
		ExtraArgs: []string{"--jinja"},
	}
	got, err := BuildArgsForBackend(p, domain.BackendKindUnsloth, "")
	if err != nil {
		t.Fatalf("BuildArgsForBackend(unsloth): %v", err)
	}
	want := []string{
		"--model", "unsloth/Qwen3-1.7B-GGUF",
		"--ctx-size", "8192",
		"--gguf-variant", "UD-Q4_K_XL",
		"--port", "8123",
		"--jinja",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgsForBackend(unsloth):\n got = %v\nwant = %v", got, want)
	}
}
```

- [ ] **Step 2: Run it to verify it fails.**

Run: `go test ./internal/service/processmgr/ -run TestBuildArgsForBackend_Unsloth`
Expected: FAIL with `unsupported backend kind for arg building: unsloth`.

- [ ] **Step 3: Implement the builder + switch case.**

In `internal/service/processmgr/args.go`, add a case in `BuildArgsForBackend` (before `default:`):

```go
	case domain.BackendKindUnsloth:
		return buildUnslothArgs(p), nil
```

And add the builder near `buildSGLangArgs`:

```go
// buildUnslothArgs builds args for `unsloth studio run`. The model (HF repo or
// local GGUF path) is emitted under --model; every other flag in p.Args is
// emitted verbatim as --<key> <value>. --port is injected by prepareLaunch.
// The wrapper script forces the headless/loopback flags.
func buildUnslothArgs(p domain.Profile) []string {
	return buildArgs(p, argBuildOpts{skipKeys: []string{"model"}, modelFlag: "--model"})
}
```

- [ ] **Step 4: Run it to verify it passes.**

Run: `go test ./internal/service/processmgr/ -run TestBuildArgsForBackend_Unsloth`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/service/processmgr/args.go internal/service/processmgr/args_test.go
git commit -m "feat(unsloth): build launch args for the unsloth kind"
```

---

### Task 3: Headless launcher script

**Files:**
- Create: `backends/unsloth/unsloth-serve.sh`

**Interfaces:**
- Produces: an executable the catalog `executable` points at. Receives `--model … --port … [flags]` from model-loader; forces `studio run --silent --yes --no-cloudflare -H 127.0.0.1`.

- [ ] **Step 1: Create the script.**

Create `backends/unsloth/unsloth-serve.sh`:

```sh
#!/usr/bin/env bash
set -euo pipefail

# `unsloth` re-execs into its own Studio venv (~/.unsloth/studio), so unlike
# vllm/sglang there is no local .venv to source here — just require the
# console-script on PATH. Forced flags keep the managed process headless and
# loopback-only; model-loader appends --model / --port / curated flags.
if ! command -v unsloth >/dev/null 2>&1; then
    echo "Error: 'unsloth' not found on PATH. Install Unsloth Studio (install.sh / 'unsloth studio setup')." >&2
    exit 1
fi

exec unsloth studio run --silent --yes --no-cloudflare -H 127.0.0.1 "$@"
```

- [ ] **Step 2: Make it executable and syntax-check.**

```bash
chmod +x backends/unsloth/unsloth-serve.sh
bash -n backends/unsloth/unsloth-serve.sh && echo "syntax ok"
```
Expected: `syntax ok`.

- [ ] **Step 3: Commit.**

```bash
git add backends/unsloth/unsloth-serve.sh
git commit -m "feat(unsloth): headless launcher script"
```

---

### Task 4: Carry backend kind on RunningInstance

**Files:**
- Modify: `internal/domain/instance.go`
- Modify: `internal/service/processmgr/launch.go`

**Interfaces:**
- Produces: `domain.RunningInstance.Kind domain.BackendKind` (json `kind,omitempty`), set at launch from the resolved kind. `launchPlan.kind domain.BackendKind`.
- Consumes: `prepareLaunch` `resolvedKind`.

- [ ] **Step 1: Write the failing test.**

In `internal/service/processmgr/launch_test.go` (add a new test; reuse the package's existing `newTestManager`/`fakeBinary` helpers — see other tests in that file for the exact helper names and adapt):

```go
func TestLaunch_SetsInstanceKind(t *testing.T) {
	m := newTestManager(t) // constructs fsManager with temp dir + fake binary
	p := testProfile(t)    // a profile whose resolver yields a known kind
	inst, err := m.Launch(p, LaunchBackground, "test-kind")
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	t.Cleanup(func() { _ = m.Kill(inst.PID) })
	if inst.Kind == "" {
		t.Fatalf("RunningInstance.Kind not set (got empty)")
	}
}
```

> Note: if `newTestManager`/`testProfile` differ in this package, mirror the setup used by the nearest existing `Launch` test (e.g. `launch_test.go`, `manager_test.go`). The assertion (`inst.Kind != ""`) is the contract.

- [ ] **Step 2: Run it to verify it fails.**

Run: `go test ./internal/service/processmgr/ -run TestLaunch_SetsInstanceKind`
Expected: FAIL (`inst.Kind` field does not exist → compile error, or empty).

- [ ] **Step 3: Add the field.**

In `internal/domain/instance.go`, inside `RunningInstance`, after `BinaryPath`:

```go
	Kind       BackendKind `json:"kind,omitempty"`
```

- [ ] **Step 4: Thread the kind through launchPlan.**

In `internal/service/processmgr/launch.go`:

Add to the `launchPlan` struct:

```go
	kind   domain.BackendKind
```

Set it in `prepareLaunch`'s return (the struct literal that returns `binary/args/env/port`):

```go
	return launchPlan{
		binary: resolvedBinary,
		args:   profileArgs,
		env:    applyProfileEnv(p.Launch.Env),
		port:   port,
		kind:   resolvedKind,
	}, nil
```

In `Launch` (background), set `Kind` in the `inst := domain.RunningInstance{...}` literal:

```go
		Kind:           plan.kind,
```

In `launchForeground`, set `Kind` in its `inst := domain.RunningInstance{...}` literal:

```go
		Kind:       plan.kind,
```

- [ ] **Step 5: Run it to verify it passes.**

Run: `go test ./internal/service/processmgr/ -run TestLaunch_SetsInstanceKind`
Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add internal/domain/instance.go internal/service/processmgr/launch.go internal/service/processmgr/launch_test.go
git commit -m "feat(unsloth): record backend kind on RunningInstance"
```

---

### Task 5: Kind-aware readiness + key capture (`WaitReady`)

**Files:**
- Modify: `internal/service/processmgr/processmgr.go`
- Create: `internal/service/processmgr/readiness.go`
- Create: `internal/service/processmgr/readiness_test.go`

**Interfaces:**
- Produces (interface method on `processmgr.Manager`):
  `WaitReady(inst domain.RunningInstance, timeout time.Duration, attemptID string) (authToken string, err error)`.
  For `domain.BackendKindUnsloth`: scans `inst.LogPath` for `sk-unsloth-[0-9a-f]{32}`, returns it (empty + error on timeout). For all other kinds: returns `"", WaitHealthy(inst.PID, inst.Port, timeout, attemptID)`.
- Produces: `var ErrReadyTimeout = errors.New("backend did not become ready within timeout")`; helper `waitForLogToken(logPath string, re *regexp.Regexp, timeout time.Duration) (string, error)`.
- Consumes: `RunningInstance.Kind`, `RunningInstance.LogPath`, existing `WaitHealthy`.

- [ ] **Step 1: Write failing tests for the log scanner.**

Create `internal/service/processmgr/readiness_test.go`:

```go
package processmgr

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

var testKeyRe = regexp.MustCompile(`sk-unsloth-[0-9a-f]{32}`)

func TestWaitForLogToken_FindsKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	if err := os.WriteFile(p, []byte("starting...\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
		_, _ = f.WriteString("API Key:      sk-unsloth-0123456789abcdef0123456789abcdef\n")
		_ = f.Close()
	}()
	got, err := waitForLogToken(p, testKeyRe, 3*time.Second)
	if err != nil {
		t.Fatalf("waitForLogToken: %v", err)
	}
	if got != "sk-unsloth-0123456789abcdef0123456789abcdef" {
		t.Fatalf("token = %q", got)
	}
}

func TestWaitForLogToken_Timeout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	if err := os.WriteFile(p, []byte("no key here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := waitForLogToken(p, testKeyRe, 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
```

- [ ] **Step 2: Run to verify failure.**

Run: `go test ./internal/service/processmgr/ -run TestWaitForLogToken`
Expected: FAIL (`waitForLogToken` undefined).

- [ ] **Step 3: Implement readiness.**

Create `internal/service/processmgr/readiness.go`:

```go
package processmgr

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// ErrReadyTimeout is returned when a backend does not signal readiness in time.
var ErrReadyTimeout = errors.New("backend did not become ready within timeout")

// unslothKeyRe matches the per-boot API key unsloth prints once the model is
// loaded: "API Key:      sk-unsloth-<32 hex>".
var unslothKeyRe = regexp.MustCompile(`sk-unsloth-[0-9a-f]{32}`)

// WaitReady blocks until the backend is ready to serve and returns any upstream
// auth token the proxy must inject. For unsloth, the printed key is both the
// readiness signal (it appears only after the model loads) and the token. For
// every other kind it polls /health and returns an empty token.
func (m *fsManager) WaitReady(inst domain.RunningInstance, timeout time.Duration, attemptID string) (string, error) {
	if inst.Kind == domain.BackendKindUnsloth {
		lg := m.logger.With("pid", inst.PID, "port", inst.Port, "attempt_id", attemptID)
		lg.Info("readiness_start", "mode", "unsloth_log_token", "timeout", timeout)
		token, err := waitForLogToken(inst.LogPath, unslothKeyRe, timeout)
		if err != nil {
			lg.Warn("readiness_timeout", "err", err)
			return "", err
		}
		lg.Info("readiness_ok", "mode", "unsloth_log_token")
		return token, nil
	}
	return "", m.WaitHealthy(inst.PID, inst.Port, timeout, attemptID)
}

// waitForLogToken polls logPath until re matches (returning the first match) or
// timeout elapses. Uses the same capped backoff as WaitHealthy. A missing file
// is treated as "not ready yet", not an error.
func waitForLogToken(logPath string, re *regexp.Regexp, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	delay := 100 * time.Millisecond
	const maxDelay = time.Second
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(logPath); err == nil {
			if m := re.Find(data); m != nil {
				return string(m), nil
			}
		}
		time.Sleep(delay)
		if delay < maxDelay {
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		}
	}
	return "", fmt.Errorf("log %q: %w", logPath, ErrReadyTimeout)
}
```

- [ ] **Step 4: Add `WaitReady` to the Manager interface.**

In `internal/service/processmgr/processmgr.go`, inside the `Manager` interface, after the `WaitHealthy` line:

```go
	// WaitReady blocks until the backend is ready to serve and returns any
	// upstream auth token the proxy must inject (empty for kinds that need
	// none). For unsloth it captures the printed sk-unsloth key from the log,
	// which also signals the model has finished loading.
	WaitReady(inst domain.RunningInstance, timeout time.Duration, attemptID string) (authToken string, err error)
```

- [ ] **Step 5: Run readiness tests.**

Run: `go test ./internal/service/processmgr/ -run 'TestWaitForLogToken|TestLaunch_SetsInstanceKind'`
Expected: PASS.

- [ ] **Step 6: Build the package to surface any interface-impl gaps.**

Run: `go build ./...`
Expected: success (fsManager now satisfies the extended interface; httpproxy still compiles because it calls WaitHealthy — changed in Task 6).

- [ ] **Step 7: Commit.**

```bash
git add internal/service/processmgr/processmgr.go internal/service/processmgr/readiness.go internal/service/processmgr/readiness_test.go
git commit -m "feat(unsloth): kind-aware WaitReady captures sk-unsloth key from log"
```

---

### Task 6: Proxy injects the captured Bearer token

**Files:**
- Modify: `internal/service/httpproxy/proxy.go`
- Create: `internal/service/httpproxy/proxy_test.go`
- Modify: `internal/service/httpproxy/server.go`
- Modify: `internal/service/httpproxy/handler.go`
- Modify: `internal/service/httpproxy/mocks_test.go`

**Interfaces:**
- Consumes: `processmgr.Manager.WaitReady(inst, timeout, attemptID) (string, error)`.
- Produces: `newReverseProxy(port int, authToken string) *httputil.ReverseProxy`; `loadedBackend.authToken string`.

- [ ] **Step 1: Write the failing upstream-auth test.**

Create `internal/service/httpproxy/proxy_test.go`:

```go
package httpproxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func backendPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	u, _ := url.Parse(srv.URL)
	p, _ := strconv.Atoi(u.Port())
	return p
}

func TestNewReverseProxy_InjectsAuthorization(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	rp := newReverseProxy(backendPort(t, upstream), "sk-unsloth-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	if gotAuth != "Bearer sk-unsloth-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("upstream Authorization = %q", gotAuth)
	}
}

func TestNewReverseProxy_NoTokenNoHeader(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	rp := newReverseProxy(backendPort(t, upstream), "")
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

	if gotAuth != "" {
		t.Fatalf("expected no Authorization header, got %q", gotAuth)
	}
}
```

- [ ] **Step 2: Run to verify failure.**

Run: `go test ./internal/service/httpproxy/ -run TestNewReverseProxy`
Expected: FAIL (`newReverseProxy` takes 1 arg → compile error).

- [ ] **Step 3: Update `newReverseProxy` to inject outbound auth.**

In `internal/service/httpproxy/proxy.go`, replace the function signature and add the Director wrap:

```go
// newReverseProxy builds an httputil.ReverseProxy pointed at a backend
// listening on 127.0.0.1:<port>. When authToken is non-empty it injects
// "Authorization: Bearer <authToken>" on every UPSTREAM request — this is
// outbound auth (proxy → backend, e.g. Unsloth Studio), NOT inbound client
// auth; the proxy itself remains unauthenticated on the client side.
func newReverseProxy(port int, authToken string) *httputil.ReverseProxy {
	target := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%d", port),
	}
	rp := httputil.NewSingleHostReverseProxy(target)
	if authToken != "" {
		orig := rp.Director
		rp.Director = func(req *http.Request) {
			orig(req)
			req.Header.Set("Authorization", "Bearer "+authToken)
		}
	}
	rp.FlushInterval = -1
	rp.Transport = &http.Transport{
		DisableCompression:    true,
		ResponseHeaderTimeout: 0,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   16,
	}
	rp.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		writeOpenAIError(w, http.StatusBadGateway, "backend_error", "upstream_unavailable", err.Error())
	}
	return rp
}
```

- [ ] **Step 4: Add `authToken` to loadedBackend.**

In `internal/service/httpproxy/server.go`, add to the `loadedBackend` struct (after `logPath`):

```go
	authToken string
```

- [ ] **Step 5: Use WaitReady + pass the token in handler.go.**

In `internal/service/httpproxy/handler.go` `launchNewBackend`, replace the `WaitHealthy` block and the `loaded` literal:

```go
	token, rErr := s.deps.ProcessMgr.WaitReady(inst, s.cfg.HealthCheckTimeout, attemptID)
	if rErr != nil {
		s.logger.Error("proxy_swap_unhealthy",
			"profile_id", profileID, "pid", inst.PID, "port", inst.Port,
			"attempt_id", attemptID, "err", rErr)
		_ = s.deps.ProcessMgr.Kill(inst.PID)
		s.recordError(fmt.Sprintf("readiness %s: %v", profileID, rErr))
		return nil, &SwapError{http.StatusGatewayTimeout, "backend_error", "backend_unhealthy",
			fmt.Sprintf("backend %s not ready: %v", profileID, rErr)}
	}

	loaded := &loadedBackend{
		profileID: profileID,
		pid:       inst.PID,
		port:      inst.Port,
		logPath:   inst.LogPath,
		authToken: token,
		proxy:     newReverseProxy(inst.Port, token),
	}
```

- [ ] **Step 6: Update the stub to implement WaitReady.**

In `internal/service/httpproxy/mocks_test.go`, add a field to `stubManager`:

```go
	readyToken string // token WaitReady returns when healthFn passes
```

And add the method (next to `WaitHealthy`):

```go
func (m *stubManager) WaitReady(inst domain.RunningInstance, timeout time.Duration, attemptID string) (string, error) {
	if err := m.healthFn(inst.PID, inst.Port); err != nil {
		return "", err
	}
	return m.readyToken, nil
}
```

- [ ] **Step 7: Fix any other callers of `newReverseProxy`.**

Run: `grep -rn "newReverseProxy(" internal/service/httpproxy`
For every call other than the one in Step 5, add the second arg `""` (no token). Expected: only `handler.go` and possibly test files; update them to `newReverseProxy(port, "")`.

- [ ] **Step 8: Run the httpproxy + processmgr tests.**

Run: `go test ./internal/service/httpproxy/... ./internal/service/processmgr/...`
Expected: PASS.

- [ ] **Step 9: Commit.**

```bash
git add internal/service/httpproxy/
git commit -m "feat(unsloth): proxy injects captured Bearer token via WaitReady"
```

---

### Task 7: Validator accepts HF repos for the unsloth kind

**Files:**
- Modify: `internal/service/validator/rules.go`
- Modify: `internal/service/validator/rules_test.go`

**Interfaces:**
- Consumes: `supportsHFRepo(kind)`.

- [ ] **Step 1: Write the failing test.**

In `internal/service/validator/rules_test.go`, add (adapt to the file's existing test style if a `supportsHFRepo` test already exists — extend its table instead):

```go
func TestSupportsHFRepo_Unsloth(t *testing.T) {
	if !supportsHFRepo(domain.BackendKindUnsloth) {
		t.Fatal("unsloth must support HF repo model refs")
	}
}
```

- [ ] **Step 2: Run to verify failure.**

Run: `go test ./internal/service/validator/ -run TestSupportsHFRepo_Unsloth`
Expected: FAIL.

- [ ] **Step 3: Add the kind.**

In `internal/service/validator/rules.go`, extend `supportsHFRepo`:

```go
func supportsHFRepo(kind domain.BackendKind) bool {
	switch kind {
	case domain.BackendKindVLLM, domain.BackendKindSGLang, domain.BackendKindUnsloth:
		return true
	}
	return false
}
```

- [ ] **Step 4: Run to verify pass.**

Run: `go test ./internal/service/validator/ -run TestSupportsHFRepo_Unsloth`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/service/validator/rules.go internal/service/validator/rules_test.go
git commit -m "feat(unsloth): accept HF repo model refs for unsloth profiles"
```

---

### Task 8: Full build + test gate

**Files:** none (verification).

- [ ] **Step 1: Build.**

Run: `make build`
Expected: builds `bin/model-loader` with no errors.

- [ ] **Step 2: Full test suite.**

Run: `make tests`
Expected: PASS (no golden fixture changes; if any golden test fails unexpectedly, investigate — do NOT blindly `-update`).

- [ ] **Step 3: Commit (only if any incidental fixups were needed).**

```bash
git add -A && git commit -m "test(unsloth): full suite green" || echo "nothing to commit"
```

---

### Task 9: Update the rtx3090-inference-profiles skill

**Files:**
- Modify: `.agents/skills/rtx3090-inference-profiles/SKILL.md`
- Modify: `.agents/skills/rtx3090-inference-profiles/references/llama-family.md`

**Interfaces:** documentation only.

- [ ] **Step 1: Add the dispatch-table row.**

In `SKILL.md`, in the "Backend dispatch" table, add after the `dflash` row:

```
| unsloth | none — must `backend add` first | GGUF (HF repo `org/repo-GGUF` or local path) via `unsloth studio run` | references/llama-family.md |
```

- [ ] **Step 2: Add dispatch guidance.**

In `SKILL.md`, in the "Backend dispatch" paragraph, append a sentence:

```
unsloth is opt-in: pick it when the user wants Unsloth's managed `unsloth run`
server for a GGUF (HF auto-download, Studio tool-calling, or the Anthropic
endpoint). Default GGUF routing is unchanged. unsloth requires Unsloth Studio
installed; the proxy auto-captures and injects Studio's Bearer token, so the
profile author sets neither auth nor port.
```

- [ ] **Step 3: Add an "Unsloth" subsection to llama-family.md.**

Append to `.agents/skills/rtx3090-inference-profiles/references/llama-family.md`:

```markdown
## Unsloth (`unsloth` kind)

`unsloth studio run` wraps the same llama-server under the hood, so all
llama-family VRAM/KV/ctx math applies. Differences:

- **Registration:** `model-loader backend add "Unsloth RTX3090" --kind unsloth
  --executable /home/diogo/dev/model-loader/backends/unsloth/unsloth-serve.sh`,
  then the schema is generated automatically.
- **Model field:** an HF repo (`unsloth/<Repo>-GGUF`, optionally with a
  separate `"gguf-variant"` arg) OR a local `.gguf` path. HF repos are legal
  for this kind (unlike plain llama-server).
- **Managed flags:** never set `port`/`host`/auth — the wrapper forces
  loopback + headless and the process manager assigns the port; the proxy
  captures the printed `sk-unsloth-…` key and injects it on every request.
- **Schema flags:** `gguf-variant`, `ctx-size`, `n-gpu-layers`, `parallel`,
  `flash-attn`, `cache-type-k/v`, `jinja`, `chat-template-file`, sampling.
  Real-but-absent flags go in `extraArgs` (warning only) — never raw `-np`,
  `--host`, `--port`, `--api-key`, `--model`/`-hf` (Studio-managed).
- **served-model-name:** the proxy forwards `model: <profile-id>`. If Studio
  rejects an unknown model id, set unsloth's alias to the profile id (verify
  per the run tutorial); otherwise it serves the single loaded model.
- Record quant/KV/ctx/peak-GiB/tok-s in the description as usual.
```

- [ ] **Step 4: Add a Common-mistakes row in SKILL.md.**

In the "Common mistakes" table, add:

```
| unsloth profile without Studio installed | Wrapper exits "unsloth not found" | Install Unsloth Studio first; only `backend add` after |
```

- [ ] **Step 5: Commit.**

```bash
git add .agents/skills/rtx3090-inference-profiles/
git commit -m "docs(skill): teach rtx3090-inference-profiles the unsloth kind"
```

---

### Task 10: End-to-end acceptance with DiffusionGemma (verification gate)

**Files:** none (live verification; produces a profile + recorded numbers).

**Preconditions:** `unsloth` on PATH; Studio set up; GPU free. Target model present:
`/home/diogo/models/huggingface/unsloth/diffusiongemma-26B-A4B-it-GGUF/diffusiongemma-26B-A4B-it-Q4_K_M.gguf` (17 GB, confirmed).

- [ ] **Step 1: Read the DiffusionGemma run tutorial for required flags.**

Fetch <https://unsloth.ai/docs/models/diffusiongemma#run-diffusiongemma-tutorials>.
Capture any required launch flags (chat template / `--jinja` / sampling / diffusion-specific knobs). Map each to a schema flag or to `extraArgs`.

- [ ] **Step 2: Register the backend.**

```bash
./bin/model-loader backend add "Unsloth RTX3090" --kind unsloth \
  --executable /home/diogo/dev/model-loader/backends/unsloth/unsloth-serve.sh
./bin/model-loader backend list
```
Expected: an `unsloth` backend appears with a generated schema under `~/.config/model-loader/backends/schemas/`.

- [ ] **Step 3: Create the profile (local GGUF).**

```bash
./bin/model-loader profile create --id diffusiongemma-unsloth --backend <unsloth-backend-id> --file - <<'EOF'
{
  "model": "/home/diogo/models/huggingface/unsloth/diffusiongemma-26B-A4B-it-GGUF/diffusiongemma-26B-A4B-it-Q4_K_M.gguf",
  "args": { "ctx-size": 8192, "n-gpu-layers": 999, "jinja": true },
  "extraArgs": []
}
EOF
```
Apply any tutorial-required flags from Step 1 (move binary-real/schema-absent ones to `extraArgs`). Omit `port`.

- [ ] **Step 4: Validate.**

```bash
./bin/model-loader profile validate diffusiongemma-unsloth
```
Expected: passes. Unknown-arg error → fix spelling or move to `extraArgs`.

- [ ] **Step 5: Start + confirm readiness via the key line.**

```bash
./bin/model-loader instance start diffusiongemma-unsloth
# then watch the log (proxy-owned instances: read the file directly)
tail -f ~/.local/state/model-loader/logs/diffusiongemma-unsloth-*.log
```
Expected: the log shows the model loading and an `API Key:      sk-unsloth-…` line (the readiness signal). `nvidia-smi --query-gpu=memory.used --format=csv` for VRAM.

- [ ] **Step 6: Inference through the proxy (resolves the model-field risk).**

```bash
curl -s http://127.0.0.1:4321/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"diffusiongemma-unsloth","messages":[{"role":"user","content":"Say hello in one short sentence."}],"max_tokens":64}' | tee /tmp/unsloth-resp.json
```
Expected: a valid OpenAI completion. 
- If it returns an auth error → the token capture/injection failed (re-check the log scan + Director).
- If it returns a model-not-found error → Studio validates the `model` field; set unsloth's alias to the profile id (per Step 1 tutorial / a passthrough flag) or document the required `model` value, then retry.

- [ ] **Step 7: Record results + report.**

Update the profile description with quant/KV/ctx/peak-GiB/tok-s. Summarize the E2E outcome (and any model-field resolution) to the user. If calibration cannot complete now, record "calibration pending" — never present estimates as measurements.

- [ ] **Step 8: Commit any profile/doc artifacts that belong in the repo.**

(Profiles live under `~/.config`, not the repo — only commit repo files, e.g. if the tutorial revealed a schema flag worth adding to `unslothhelp`.)

```bash
git add -A && git commit -m "test(unsloth): DiffusionGemma end-to-end verified" || echo "nothing to commit"
```

---

## Self-review

- **Spec coverage:** kind+launcher (Tasks 1,3) ✓; embedded schema+generator+register+essentials (Task 1) ✓; args (Task 2) ✓; readiness+auth data flow (Tasks 4,5,6) ✓; supportsHFRepo (Task 7) ✓; catalog entry via `backend add` (Task 10) ✓; rtx3090 skill (Task 9) ✓; DiffusionGemma E2E + model-field risk (Task 10) ✓; tests (every task + Task 8) ✓.
- **Type consistency:** `WaitReady(inst domain.RunningInstance, timeout time.Duration, attemptID string) (string, error)` used identically in the interface (Task 5), fsManager impl (Task 5), stub (Task 6), and caller (Task 6). `newReverseProxy(port int, authToken string)` consistent across proxy.go, handler.go, tests (Task 6). `RunningInstance.Kind` / `launchPlan.kind` consistent (Task 4→5). `unslothhelp.EmbeddedSchema()` consumed only in Task 1's generator.
- **Placeholders:** Task 4's test references package helpers (`newTestManager`/`testProfile`) whose exact names must be matched to the processmgr test suite — flagged inline with the invariant to assert; not a code placeholder. No TBD/TODO.
- **Risk carried forward:** the `model`-field/served-name behavior is resolved empirically in Task 10 Step 6 with a concrete contingency.
