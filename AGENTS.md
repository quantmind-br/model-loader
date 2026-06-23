# PROJECT KNOWLEDGE BASE

**Generated:** 2026-06-02
**Commit:** 58b279a
**Branch:** main

> **AGENTS.md is in `.gitignore`** — this file is generated locally and not tracked. The canonical per-directory knowledge bases are the `CLAUDE.md` files throughout the repo.

---

## OVERVIEW

TUI application for managing inference server profiles and processes across multiple backends (llama.cpp, vLLM, SGLang, DFlash, Unsloth, buun-llama-cpp, beellama.cpp). Built with Go 1.26.2 + Charmbracelet bubbletea. 5-tab interface.

- **Entry point**: `cmd/model-loader/main.go` (subcommand dispatch: TUI, serve, download, benchmark, import)
- **Shared bootstrap**: `internal/app/bootstrap.go` — DI container for TUI, CLI, and headless modes
- **Domain model**: `internal/domain/` — Profile, Instance, Model, FlagSchema, BackendValidationSchema, BackendKind enum
- **24 service packages**: `internal/service/*` — each owns one domain concern (processmgr, profilestore, backendschema, httpproxy, etc.)
- **TUI layer**: `internal/ui/` — bubbletea 5-tab model with reusable components

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Service wiring / DI | `cmd/model-loader/bootstrap.go` | `bootServices` builds all services |
| llama.cpp --help parsing | `internal/service/llamahelp/` | embedded schema pinned to **v9761** (matches `testdata/help-v9761.golden.json`); runtime parser falls back to this if live `--help` parsing fails |
| Multi-backend catalog | `internal/service/backendcatalog/` | `catalog.json` + schema resolver, one entry per `BackendKind` |
| Per-backend arg shaping | `internal/service/processmgr/args.go` | `BuildArgsForBackend(p, kind, exe)` dispatcher; vLLM and DFlash use positional model, llama.cpp/SGLang/Unsloth use `--model`/`--model-path` |
| Per-backend env injection | `internal/service/processmgr/launch.go:252-276` | `pythonBackends` map + `buildLaunchEnv` injects `PYTHONUNBUFFERED=1` for vLLM/SGLang/Unsloth (without it, their stdout block-buffers and the TUI Server tab shows logs in late bursts) |
| Log streaming (TUI) | `internal/service/monitor/logs.go` | `fsnotify` + `bufio.ReadString('\n')`; **backend-agnostic** by design — Python unbuffering is the spawn-time fix |
| Schema generation | `internal/service/backendschema/` | orchestrates `AddBackend` for all kinds |
| Process lifecycle | `internal/service/processmgr/` | survives TUI exit, recovers from `instances.json`; 7 backends (llama.cpp / vLLM / SGLang / DFlash / Unsloth / buun-llama-cpp / beellama.cpp) |
| Profile CRUD | `internal/service/profilestore/` | FS-based JSON; `port` is reserved and stripped on read/write |
| HTTP proxy | `internal/service/httpproxy/` | OpenAI-shaped reverse proxy — only client channel to backends; auto-started by TUI (default 127.0.0.1:4321); `model` = profile ID |
| Web profile editor | `internal/service/configweb/` | on-demand HTTP GUI (HTMX/Alpine) |
| Benchmark engine | `internal/service/benchmark/` | SWE-bench Lite + needle probe |
| GPU monitoring | `internal/service/monitor/` | `nvidia-smi`; **6 goroutines per Subscribe** (log tail + log pump + slots poller + slots pump + GPU poller + metrics ticker); backend-agnostic log tailer (Python unbuffering is handled at spawn time, not here) |
| HF downloads | `internal/service/downloadmgr/` | queued with progress events |
| TUI pages | `internal/ui/pages/` | 5 tabs; web editor for profile create/edit |
| Profile schema (canonical) | `docs/profile-schema.json` | authoritative JSON Schema — must stay in sync with `domain.Profile` |
| Bug tracker | `BUGS.md` | single source of truth for known defects (L/B/D/T series) |

## COMMANDS

```bash
make build              # Build binary to bin/model-loader
make install            # Install to ~/.local/bin
make tests              # Run all tests (golden + unit)
go test ./... -update   # Update golden test fixtures
go test ./pkg/...       # Run a single package
go test ./pkg/... -run TestName  # Run a single test
```

> No linter, no formatter, no CI. Just `go test` and `make build`.

## CONVENTIONS

### Service Layer
- **Manager/Store suffix**: services are `*Manager` or `*Store` (e.g., `processmgr`, `profilestore`)
- **Interface in package**: each service exports its own interface; consumers import the interface
- **Config struct**: `type Config struct { ... }` with functional options (`WithLogger`, `WithWaitFunc`)
- **Logger fallback**: `log.Nop()` if no logger provided; never pass `nil`
- **Error sentinels**: `var ErrNotFound = errors.New(...)` at package level, never dynamic errors for sentinel conditions
- **Atomic JSON writes**: `fsx.WriteJSONAtomic` (write-temp + rename) for all on-disk JSON

### CLI (`internal/cli/`)
- **TUIRunner pattern**: `main.go` sets `cli.TUIRunner = runTUI` so CLI never imports `internal/ui`
- **ExitError**: commands return `&ExitError{Code: N}` for non-1 exit codes; `Execute()` unwraps
- **Bootstrap per command**: every leaf command calls `app.Bootstrap(logLevel)` and defers `svc.Close()`
- **JSON output**: `--json` is a persistent root flag; all table commands respect it
- **Resolve helpers**: `resolveProfileRef`, `resolveBackend`, `resolveInstance` — by ID then exact name then unique prefix

### TUI (`internal/ui/`)
- **Global shortcut gate**: every printable-rune shortcut in `RootModel.Update` MUST be wrapped in `if !m.activePageCapturesInput() { ... }`. Exception: `ctrl+c` is unconditional escape.
- **Page capture contract**: pages with editable surfaces (huh forms, pickers, modals, web-edit modal) MUST implement `InputCapture.IsCapturingInput() bool` returning `true` while active.
- **Forward non-key messages**: when a page hosts a `*huh.Form`, its `Update` MUST forward non-`tea.KeyMsg` messages to the form so focus/validation handshakes complete.
- **Tests**: use `teatest.NewTestModel(t, model, teatest.WithInitialTermSize(w, h))` with `WaitFor` helpers.

### Tests
- **Table-driven**: standard Go pattern throughout
- **Golden tests**: fixtures in `testdata/` — update via `go test ./... -update`
- **Test helpers**:
  - `fakeBinary(t)` — returns path to `testdata/fake-llama-server.sh` (no-op executable)
  - `freePort(t)` — binds `127.0.0.1:0` and returns the allocated port
  - `newTestManager(t)` — constructs a `processmgr` with temp dir + fake binary
  - `newManager(t)` — constructs a `backendschema.Manager` with temp dir + fake generator

## ANTI-PATTERNS

- **NEVER** call `app.Bootstrap()` twice — it performs filesystem side-effects (log rotation, migration)
- **NEVER** forget to defer `svc.Close()` — stops process manager and flushes logger
- **NEVER** import `internal/ui` from `internal/cli` — use the `TUIRunner` callback to avoid cycles
- **NEVER** hand-edit a backend schema's `presentation`/`rules` blocks in JSON — edit through the web Customize mode so `Source.Editable` is set and `RefreshSchema` preserves them
- **NEVER** extend `essentialSeed` (`backendschema/presentation.go`) without explicit user request — it's a curated seed, not a generic form abstraction
- **NEVER** run `llama-server` (or any other managed backend) manually while TUI is managing instances
- **NEVER** edit `testdata/help-v9761.golden.json` directly — regenerate via `go test ./... -update`
- **NEVER** assume process cleanup on TUI exit — processes are intentionally orphaned
- **NEVER** change the persisted profile structure (`domain.Profile` / profilestore JSON) without mirroring the change in `docs/profile-schema.json`
- **NEVER** intercept printable runes globally in `internal/ui/root.go` without first checking `activePageCapturesInput()`
- **NEVER** use `fmt.Print` directly in CLI commands — go through `cmd.OutOrStdout()` / `cmd.ErrOrStderr()` for testability

## LANGUAGE RULES

- ALL UI/UX interfaces (labels, help text, modals, messages) MUST be in English. No Portuguese, Spanish, or other languages.
- ALL configuration schemas (flag names, field names, JSON keys, TOML keys, `BackendValidationSchema` descriptions, `Presentation` group labels) MUST be in English.

## WEB PROFILE EDITOR CONTRACT

- Profile create/edit uses an **on-demand, in-process** HTTP server (`internal/service/configweb`) bound to `127.0.0.1:0`, opens the browser, and shows an "editing in browser…" modal. On save/cancel the server shuts down and the TUI reloads the list.
- The form is **schema-driven**: one render engine builds the page from `domain.BackendValidationSchema.Flags` + an editable `Presentation`.
- **Customize mode** edits the backend schema itself — per-flag constraints, add/remove flags, presentation, and `CrossFieldRule`s. Every customize handler sets `schema.Source.Editable = true` so `RefreshSchema` preserves manual edits across `--help` re-parsing.
- Curated highlights live in `essentialSeed` (`backendschema/presentation.go`); `BuildPresentation` seeds the "Essentials" group.
- Cross-field rules are evaluated by `validator/crossfield.go`.

## PROFILE NAMING CONVENTION

Profiles live in `~/.config/model-loader/profiles/<id>.json`. The naming rules below are a curation discipline for the on-disk library — they are NOT enforced by code; `profilestore` only requires a non-empty `id`.

### Filename ↔ id

- **The `.json` filename (basename, minus extension) MUST equal the profile's `id` field.** `gemma-4-12b-it-mtp-256k.json` ⇒ `"id": "gemma-4-12b-it-mtp-256k"`. No exceptions.
- Renaming a profile id means renaming the file too. Prefer `model-loader profile` CRUD (it keeps the two in sync and writes a `.history/<id>.previous.json` backup) over hand-editing JSON.

### id grammar

Lowercase kebab-case, segments joined by `-`, ordered most-significant first:

```
<model-family><version>-<size>[-<base-variant>][-<quant>][-<capability>…]-<ctx>[-<backend>][-<mode/disambiguator>]
```

| Segment | Rule | Examples |
|---------|------|----------|
| model-family+version | the base model line | `gemma-4`, `qwen3.6`, `qwen3-vl`, `nex-n2-mini` |
| size | params / MoE shape | `12b`, `26b-a4b`, `35b-a3b`, `e4b`, `0.6b` |
| base-variant | finetune lineage, only if not the plain base | `it`, `coder`, `obliterated`, `heretic-v2`, `neo-code`, `nsfw-caption-v45` |
| quant | quantization actually loaded | `q4kxl`, `q4km`, `qat-q4`, `awq`, `gptq` |
| capability | **only if ACTIVE in `args`** | `vision` (⇔ `args.mmproj` set), `mtp`/`dflash` (⇔ `args.spec-type` set) |
| ctx | the context label — see below | `256k`, `192k`, `8k`, `32k` |
| backend | only when the engine is not the default llama.cpp and is otherwise ambiguous | `vllm`, `beellama` |
| mode/disambiguator | distinguishes siblings that share all the above | `parallel12-32k`, `-speed`, `-throughput`, `-quality`, `-cpumoe` |

### Hard rules (a mismatch here is a bug, fix the id or the config)

- **ctx label MUST match the real context.** The source of truth is `args.ctx-size` (llama.cpp) or `args.max-model-len` (vLLM). The label uses **binary k (÷1024)**, floored to a whole k: `262144`→`256k`, `253952`→`248k`, `196608`→`192k`, `221184`→`216k`, `200000`→`195k` (195×1024=199680 ≤ 200000). If you shrink/grow ctx, rename the id (and file) and fix the `description`. Stale ctx labels like `…-262k` on a `ctx-size: 200000` profile are the most common drift.
- **Capability segments mirror `args`, not intent.** Add `vision` to the id only while `args.mmproj` is set; add `mtp`/`dflash` only while `args.spec-type` is set. Drop the segment when the flag is removed.
- **The family/size segment MUST name the real base model**, not the publisher's repackaging label (e.g. a Huihui abliteration of Qwen3.6-35B-A3B is `…-qwen3.6-35b-a3b-…`, not just `huihui-35b-a3b`).
- **Siblings differing only by serving mode MUST carry a disambiguator** (`-speed`/`-throughput`/`-quality`, `parallelN-Wk`, `-cpumoe`) so ids stay unique and self-describing.

### Keep description in sync

The `description` first sentence and headline metrics (ctx, VRAM, tok/s) should match current `args`. Append-only verification logs are fine, but when they contradict the opening line (e.g. "at 200k ctx" while `ctx-size: 262144`), rewrite the opener — the id, name, and description must agree on ctx and capabilities.

## NOTES

- **Binary managed**: `llama-server` (not model-loader)
- **Instance ports**: ephemeral, assigned by the process manager at launch; `port` in profile args is reserved — ignored and stripped on load/save. All inference goes through the proxy (health-check wait defaults to 180s)
- **Config path**: `~/.config/model-loader/config.toml`
- **State path**: `~/.local/state/model-loader/instances.json`
- **Profiles dir**: `~/.config/model-loader/profiles/` (config `paths.profiles_dir`)
- **Backend catalog**: `~/.config/model-loader/backends/catalog.json` + `schemas/*.json`
- **Schema version**: embedded-v9761 (matches `testdata/help-v9761.golden.json`; runtime `--help` parser may regenerate a newer version)
- **Docs**: `docs/superpowers/` contains design specs and PRDs
- **Embedded data**: `//go:embed` used for datasets (SWE-bench, instruction problems) and fallback schemas

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **model-loader** (9918 symbols, 31837 relationships, 300 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> If any GitNexus tool warns the index is stale, run `npx gitnexus analyze` in terminal first.

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `gitnexus_impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `gitnexus_detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `gitnexus_query({query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `gitnexus_context({name: "symbolName"})`.

## Never Do

- NEVER edit a function, class, or method without first running `gitnexus_impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `gitnexus_rename` which understands the call graph.
- NEVER commit changes without running `gitnexus_detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/model-loader/context` | Codebase overview, check index freshness |
| `gitnexus://repo/model-loader/clusters` | All functional areas |
| `gitnexus://repo/model-loader/processes` | All execution flows |
| `gitnexus://repo/model-loader/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
