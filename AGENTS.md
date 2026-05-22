# PROJECT KNOWLEDGE BASE

**Generated:** 2026-05-22
**Commit:** defda2f
**Branch:** main

## OVERVIEW
TUI application for managing llama.cpp profiles and llama-server processes. Built with Go 1.26.2 + Charmbracelet bubbletea. 5-tab interface.

## STRUCTURE
```
./
├── cmd/model-loader/   # Entry point (subcommand dispatch)
├── internal/
│   ├── config/             # Viper TOML loader
│   ├── domain/             # Profile, Instance, Model, FlagSchema
│   ├── log/                # slog wiring + rotation
│   ├── service/
│   │   ├── backendcatalog/ # Multi-backend catalog + resolver
│   │   ├── backendschema/  # Schema generation orchestrator
│   │   ├── benchmark/      # Profile eval engine (SWE-bench Lite + needle probe)
│   │   ├── benchmarkstore/ # Benchmark run persistence (1 JSON per run)
│   │   ├── buunhelp/       # Embedded schema for buun-llama-cpp backend
│   │   ├── configweb/      # On-demand web GUI for profile editing (HTMX/Alpine, embedded assets)
│   │   ├── downloadmgr/    # HuggingFace file downloader with progress
│   │   ├── dflashhelp/     # Embedded schema for DFlash backend
│   │   ├── hfhub/          # HuggingFace Hub API client
│   │   ├── httpproxy/      # OpenAI-shaped reverse proxy
│   │   ├── llamahelp/      # --help parser + embedded schema
│   │   ├── llamabin/       # Binary path resolver
│   │   ├── metricsstore/   # Rolling metrics persistence (Append/Read/Compact)
│   │   ├── migration/      # One-time config/state migrations
│   │   ├── modelscanner/   # GGUF model scanning
│   │   ├── monitor/        # GPU metrics via nvidia-smi
│   │   ├── playground/     # OpenAI-compatible chat streaming client
│   │   ├── processmgr/     # Process lifecycle + instance recovery
│   │   ├── profilestore/   # Profile persistence (FS)
│   │   ├── proxysupervisor/ # HTTP proxy lifecycle manager
│   │   ├── sglanghelp/     # Embedded schema for SGLang
│   │   ├── sizing/         # GPU memory fit calculator
│   │   ├── validator/      # Flag validation rules
│   │   └── vllmhelp/       # Embedded schema for vLLM
│   └── ui/
│       ├── components/    # Help, Modal, Picker, Sparkline, Statusbar
│       ├── pages/         # 5 tabs (profile create/edit launches the web GUI)
│       └── theme/
├── testdata/              # Golden test fixtures
├── docs/superpowers/      # Design specs
└── Makefile
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| llama-server --help parsing | internal/service/llamahelp/ | embedded schema pinned to v7376 |
| Buun embedded schema | internal/service/buunhelp/ | buun-llama-cpp fork — merges upstream + fork-specific flags |
| DFlash embedded schema | internal/service/dflashhelp/ | Lucebox speculative-decoding runtime — hand-curated |
| Multi-backend catalog | internal/service/backendcatalog/ | catalog.json + schema resolver |
| Schema generation | internal/service/backendschema/ | orchestrates AddBackend for all kinds |
| GGUF model metadata | internal/service/modelscanner/gguf.go | |
| Process lifecycle | internal/service/processmgr/ | survives TUI exit, recovers from instances.json |
| Profile CRUD | internal/service/profilestore/ | FS-based |
| Flag validation | internal/service/validator/ | |
| GPU monitoring | internal/service/monitor/ | nvidia-smi |
| HTTP proxy / Server tab | internal/service/httpproxy/ | OpenAI-shaped reverse proxy |
| HF download manager | internal/service/downloadmgr/ | queued downloads with progress events |
| HF Hub API client | internal/service/hfhub/ | model search, file listing |
| Binary resolution | internal/service/llamabin/ | PATH lookup + Python fallback |
| Proxy lifecycle | internal/service/proxysupervisor/ | state machine driving httpproxy |
| Benchmark engine | internal/service/benchmark/ | SWE-bench Lite + long-context needle probe |
| Benchmark store | internal/service/benchmarkstore/ | 1 JSON per run |
| Metrics persistence | internal/service/metricsstore/ | per-profile JSONL |
| TUI pages | internal/ui/pages/ | 5 tabs; Profiles tab launches the web editor for create/edit |
| Web profile editor | internal/service/configweb/ | on-demand in-process HTTP GUI (HTMX/Alpine); replaces the old huh editor |
| Editor presentation/rules | internal/domain/backend_schema.go | `Presentation` + `CrossFieldRule` on the schema envelope |
| Essentials seed | internal/service/backendschema/presentation.go | `essentialSeed` + `BuildPresentation` — curated per-backend highlights |
| Config | internal/config/ | Viper TOML at ~/.config/model-loader/ |
| Logging | internal/log/ | file-only slog, rotate-by-session |
| Profile config schema (canonical) | docs/profile-schema.json | JSON Schema for the persisted profile file format — authoritative reference, keep in sync with `domain.Profile` |

## CONVENTIONS
- **Tests**: Golden tests in `testdata/` — update via `go test ./... -update`
- **Build**: `make build` → `bin/model-loader`
- **Embedded schema**: Pinned to llama.cpp build "v7376 (380b4c9)" — refresh in embedded.go
- **Instance recovery**: Background llama-server processes survive TUI exit; processmgr.Reconcile restores at boot
- **Profile config schema is canonical**: `docs/profile-schema.json` is the authoritative JSON Schema (draft 2020-12) for the persisted profile file format (envelope: `schemaVersion`/`id`/`name`/`model`/`args`/`extraArgs`/`launch`/`meta`/`pinned`). Treat it as the source of truth when authoring or validating profiles. ANY change to the persisted profile shape — adding/renaming/removing fields in `domain.Profile`, `launch`, or `meta`; bumping `schemaVersion`; changing accepted `args`/`extraArgs` value types — MUST update `docs/profile-schema.json` in the SAME change so the doc never drifts from the code.

## WEB PROFILE EDITOR CONTRACT
- Profile create/edit no longer uses an in-TUI huh form. The Profiles tab launches an **on-demand, in-process** HTTP server (`internal/service/configweb`) bound to `127.0.0.1:0`, opens the browser, and shows an "editing in browser…" modal. On save/cancel the server shuts down and the TUI reloads the list.
- The form is **schema-driven**: one render engine builds the page from `domain.BackendValidationSchema.Flags` + an editable `Presentation` (groups/order/highlights). `configweb.BuildViewModel` produces the per-field widgets (number/select/toggle/text) from `FlagType`.
- The GUI's **Customize mode** edits the backend schema itself — per-flag constraints (min/max/enum/default/required), add/remove flags, presentation, and `CrossFieldRule`s. Every customize handler sets `schema.Source.Editable = true` so `backendschema.Manager.RefreshSchema` preserves manual edits across `--help` re-parsing.
- The curated highlights live in `essentialSeed` (`internal/service/backendschema/presentation.go`); `BuildPresentation` seeds the highlighted "Essentials" group. A one-time migration (`migration.ensurePresentations`) seeds a default `Presentation` into any schema lacking one (it does NOT mark `Editable`, since a synthesized default is not a manual edit).
- Cross-field rules are evaluated by the validator (`internal/service/validator/crossfield.go`, `applyCrossFieldRules`).

## ANTI-PATTERNS (THIS PROJECT)
- DO NOT hand-edit a backend schema's `presentation`/`rules` blocks in JSON — edit them through the web Customize mode so `Source.Editable` is set and `RefreshSchema` preserves them
- DO NOT extend `essentialSeed` (in `backendschema/presentation.go`) without explicit user request — it's a curated seed for the highlighted group, not a generic form abstraction
- DO NOT run `llama-server` manually while TUI is managing instances
- DO NOT edit `testdata/help-v7376.golden.json` directly — regenerate via golden test update
- DO NOT assume process cleanup on TUI exit — processes are intentionally orphaned
- DO NOT change the persisted profile structure (`domain.Profile` / profilestore JSON) without mirroring the change in `docs/profile-schema.json` — the schema doc and the code must never drift apart
- DO NOT intercept printable runes (`q`, `1-5`, `?`, letters, digits) globally in `internal/ui/root.go` without first checking `activePageCapturesInput()`. Only `ctrl+c` may bypass this gate. Pages with active huh forms / pickers / inline modals / the web-edit modal must implement `InputCapture.IsCapturingInput() bool` returning `true` while in those states (e.g. `ProfilesPage` returns `true` while `webEditing`). Otherwise the global shortcut steals the keystroke.

## LANGUAGE RULES
- ALL UI/UX interfaces (TUI labels, web editor labels, help text, modals, status messages, menu items) MUST be in English. Do not introduce Portuguese, Spanish, or any other language in the user-facing interface.
- ALL configuration schemas (flag names, field names, JSON keys, TOML keys, `BackendValidationSchema` descriptions, `Presentation` group labels) MUST be in English. No non-English identifiers or schema metadata.

## TUI INPUT ROUTING RULES
- **Global shortcut gate**: every shortcut in `RootModel.Update` that consumes a printable rune MUST be wrapped in `if !m.activePageCapturesInput() { ... }`. Exception: `ctrl+c` is unconditional escape.
- **Page capture contract**: a page that opens any editable surface (huh form, text input, inline picker, confirm dialog, the web-edit modal) MUST implement `InputCapture` and return `true` while that surface is on screen. See `ProfilesPage.IsCapturingInput()` for the pattern (captures while `webEditing`, `pickerActive`, or `confirmDelete`).
- **Forwarding non-key messages to huh**: when a page hosts a `*huh.Form` (e.g. the Backends page), its `Update` MUST forward non-`tea.KeyMsg` messages to the form so its internal Cmd→Msg handshake (Init focus, async validation) completes.
- **Tests**: any new global shortcut MUST have a paired test using the `capturingPage` test double in `internal/ui/root_test.go` proving the key is forwarded (not consumed) when the active page captures input.

## UNIQUE STYLES
- Charmbracelet TUI with 5-tab model (tea.Program)
- Viper config with mapstructure tags
- Domain-driven service layer under internal/service/
- Embedded fallback schema for llama-server --help (parses at runtime if binary present)
- Pages optionally implement `Reloader` for on-demand refresh on tab focus

## COMMANDS
```bash
make build    # Build binary to bin/model-loader
make install  # Install to $GOPATH/bin
make tests    # Run all tests including golden tests
go test ./... -update  # Update golden test fixtures
```

## NOTES
- Binary managed: `llama-server` (not model-loader)
- Config path: ~/.config/model-loader/config.toml
- State path: ~/.local/state/model-loader/instances.json
- Profiles dir: ~/.config/model-loader/profiles/ (config `paths.profiles_dir`; default in config.go:166)
- Schema version: embedded-v7376

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **model-loader** (7414 symbols, 25320 relationships, 300 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

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
