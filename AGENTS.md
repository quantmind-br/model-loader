# PROJECT KNOWLEDGE BASE

**Generated:** 2026-05-21
**Commit:** 0bd3ab5
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
│   │   ├── downloadmgr/    # HuggingFace file downloader with progress
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
│       ├── pages/         # 5 tabs + profile_editor sub-package
│       │   └── profile_editor/  # huh-based profile editing
│       └── theme/
├── testdata/              # Golden test fixtures
├── docs/superpowers/      # Design specs
└── Makefile
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| llama-server --help parsing | internal/service/llamahelp/ | embedded schema pinned to v7376 |
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
| TUI pages | internal/ui/pages/ | 5 tabs + profile_editor sub-package |
| Profile editing | internal/ui/pages/profile_editor/ | huh forms, draft state machine |
| Essentials registry | internal/ui/pages/profile_editor/essentials.go | curated per-backend flag list |
| Config | internal/config/ | Viper TOML at ~/.config/model-loader/ |
| Logging | internal/log/ | file-only slog, rotate-by-session |

## CONVENTIONS
- **Tests**: Golden tests in `testdata/` — update via `go test ./... -update`
- **Build**: `make build` → `bin/model-loader`
- **Embedded schema**: Pinned to llama.cpp build "v7376 (380b4c9)" — refresh in embedded.go
- **Instance recovery**: Background llama-server processes survive TUI exit; processmgr.Reconcile restores at boot

## ESSENTIALS REGISTRY CONTRACT
- `essentialFields` in `essentials.go` is a `map[domain.BackendKind][]EssentialField` — curated UX layer, not a generic form abstraction. See `DYNAMIC_FORM_PLAN.md` for authoritative design.
- `Draft.Essentials map[string]string` stores long-keyed flag values as value types (not pointers), making the Draft copyable for snapshot/dirty-check.
- `Editor.essentialPtrs` is a `map[string]*string` of heap-allocated pointers, one per essential field, bound to huh form inputs.
- `syncEssentials()` drains `essentialPtrs` back into `Draft.Essentials` after every `form.Update`.
- `hydrateEssentials()` peels matching values from `Draft.Args` into `Draft.Essentials` and seeds defaults — called with re-snapshot in `Open()`, without re-snapshot on backend switch.
- `hydrateEssentialsForSwitch()` is the no-snapshot variant used during backend kind changes so the dirty flag reflects legitimate mutation.

## ANTI-PATTERNS (THIS PROJECT)
- DO NOT add fields to `essentialFields` without explicit user request — it's a curated UX layer, not a generic form abstraction
- DO NOT run `llama-server` manually while TUI is managing instances
- DO NOT edit `testdata/help-v7376.golden.json` directly — regenerate via golden test update
- DO NOT assume process cleanup on TUI exit — processes are intentionally orphaned
- DO NOT intercept printable runes (`q`, `1-5`, `?`, letters, digits) globally in `internal/ui/root.go` without first checking `activePageCapturesInput()`. Only `ctrl+c` may bypass this gate. Pages with active huh forms / pickers / inline modals must implement `InputCapture.IsCapturingInput() bool` returning `true` while in those states. Otherwise the global shortcut steals the keystroke from the editable field and the user can't type that character.

## TUI INPUT ROUTING RULES
- **Global shortcut gate**: every shortcut in `RootModel.Update` that consumes a printable rune MUST be wrapped in `if !m.activePageCapturesInput() { ... }`. Exception: `ctrl+c` is unconditional escape.
- **Page capture contract**: a page that opens any editable surface (huh form, text input, inline picker, confirm dialog) MUST implement `InputCapture` and return `true` while that surface is on screen. See `ProfilesPage.IsCapturingInput()` for the pattern (`return p.editing || p.pickerActive || p.confirmDelete`).
- **Forwarding non-key messages to huh**: when a page hosts a `*huh.Form`, its `Update` MUST forward non-`tea.KeyMsg` messages to the form so its internal Cmd→Msg handshake (Init focus, async validation) completes. See `ProfilesPage.Update` tail block.
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
- Profiles dir: ~/.config/model-loader/profiles/ (config `paths.profiles_dir`; default in config.go:131)
- Schema version: embedded-v7376

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **model-loader** (7144 symbols, 25334 relationships, 300 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

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
