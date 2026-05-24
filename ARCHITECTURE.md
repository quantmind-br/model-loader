# Architecture

**Project:** model-loader  
**Generated:** 2026-05-24 from GitNexus knowledge graph  
**Stats:** 424 files · 9,411 symbols · 30,723 relationships · 300 execution flows  
**Indexed commit:** f5d2a92

## Overview

`model-loader` is a terminal UI (TUI) application for managing llama.cpp profiles and `llama-server` processes. Built with **Go 1.26** and the **Charmbracelet bubbletea** stack, it provides a 5-tab interface for model discovery, profile editing, server lifecycle management, benchmarking, and an OpenAI-compatible HTTP proxy.

The codebase follows a domain-driven layout: a thin TUI layer (`internal/ui/`) drives focused services (`internal/service/`) over a shared domain model (`internal/domain/`). Background `llama-server` processes intentionally survive TUI exit and are recovered at boot from persisted state in `~/.local/state/model-loader/instances.json`.

## Functional Areas

GitNexus clusters the codebase into 27 functional modules. The most significant by symbol count and cohesion:

| Module | Symbols | Cohesion | Responsibility |
|--------|---------|----------|----------------|
| **Pages** | 441 | 72% | 5 TUI tabs (Profiles, Models, Backends, Server, Benchmark) + page logic |
| **Components** | 251 | 82% | Reusable UI widgets: Help, Modal, Picker, Sparkline, Statusbar |
| **Cli** | 199 | 71% | Subcommand dispatch, argument parsing, TUI/CLI dual entry |
| **Benchmark** | 179 | 81% | SWE-bench Lite + long-context needle probe engine |
| **Processmgr** | 119 | 81% | `llama-server` process lifecycle + instance recovery |
| **Backendschema** | 117 | 84% | Schema generation orchestrator across all backend kinds |
| **Configweb** | 88 | 77% | On-demand HTTP server for web-based profile editing |
| **Profilestore** | 81 | 82% | FS-based profile CRUD |
| **Ui** | 71 | 83% | Root model, tab orchestration, global input routing |
| **Downloadmgr** | 62 | 84% | HuggingFace file downloader with progress events + state |
| **Monitor** | 46 | 90% | GPU metrics via `nvidia-smi` |
| **Httpproxy** | 44 | 89% | OpenAI-shaped reverse proxy |
| **Validator** | 40 | 84% | Flag validation rules + cross-field rule engine |
| **Modelscanner** | 35 | 89% | GGUF model metadata extraction |
| **Llamahelp** | 31 | 89% | `llama-server --help` parser + embedded schema (v7376) |
| **Domain** | 16 | 79% | Shared types: Profile, Instance, Model, BackendValidationSchema |
| **Hfhub** | 21 | 74% | HuggingFace Hub API client (search, file listing) |
| **Backendcatalog** | 16 | 76% | Multi-backend catalog + binary resolver |
| **Proxysupervisor** | 14 | 98% | HTTP proxy lifecycle state machine |
| **Llamabin** | 12 | 76% | Binary path resolution (PATH + Python fallback) |

Plus smaller modules: Config, Log, Migration, Metricsstore, Playground, SGLang/vLLM/DFlash/buun help embeds, and internal `fsx` atomic-write helper.

## Key Execution Flows

The graph holds 300 flows. The five highest-impact traces, read directly from the knowledge graph:

### 1. TUI Bootstrap — `RunTUI → ApplyDefaults` (5 steps)

Cross-community flow connecting **main**, **app**, and **Config**. The TUI entry point bootstraps the application, loads the TOML config, and fills in any missing defaults.

```
runTUI (cmd/model-loader/main.go)
  → Bootstrap (internal/app/bootstrap.go)
    → Load (internal/config/config.go)
      → LoadFrom (internal/config/config.go)
        → applyDefaults (internal/config/config.go)
```

### 2. HTTP Proxy On-Demand Launch — `HandleForward → LaunchProfile` (3 steps)

Cross-community flow connecting **Httpproxy** and **Proxysupervisor**. When a request hits the OpenAI-compatible proxy, the handler ensures the target profile is loaded, triggering an on-demand backend start if necessary.

```
handleForward (internal/service/httpproxy/handler.go)
  → ensureLoaded (internal/service/httpproxy/handler.go)
    → launchProfile (internal/service/httpproxy/handler.go)
```

### 3. Backend Binary Probing — `Probe → CheckExecutable` (7 steps)

Cross-community flow connecting **Backendcatalog** and **Llamabin**. Health-checks a configured backend by resolving its executable (PATH lookup, then a Python-launcher fallback) before probing it.

```
Probe (internal/service/backendcatalog/probe.go)
  → probeOne (internal/service/backendcatalog/probe.go)
    → resolveExecutable (internal/service/backendcatalog/resolver.go)
      → ResolveCommandWithPythonFallback (internal/service/llamabin/resolver.go)
        → Resolve (internal/service/llamabin/resolver.go)
          → resolveInPATH (internal/service/llamabin/resolver.go)
            → checkExecutable (internal/service/llamabin/resolver.go)
```

### 4. Download Worker & Persistence — `Init → StateFilename` (7 steps)

Cross-community flow connecting **Cli**, **Downloadmgr**, and internal state. A download worker streams an HF file to disk and atomically persists its state record so progress survives a crash.

```
init (internal/cli/download.go)
  → RunWorker (internal/service/downloadmgr/worker.go)
    → runDownload (internal/service/downloadmgr/worker.go)
      → streamWithProgress (internal/service/downloadmgr/worker.go)
        → SaveRecord (internal/service/downloadmgr/state.go)
          → StatePath (internal/service/downloadmgr/state.go)
            → stateFilename (internal/service/downloadmgr/state.go)
```

### 5. Profile Import Validation — `HandleKey → ProfileExists` (6 steps)

Cross-community flow connecting **Pages** and **Profilestore**. A keypress in the Profiles tab triggers a bundle import, which checks for existing IDs to avoid collisions.

```
handleKey (internal/ui/pages/profiles_update.go)
  → startImportWithPath (internal/ui/pages/profiles_importexport.go)
    → importBundleCmd (internal/ui/pages/profiles_importexport.go)
      → ImportBundle (internal/service/profilestore/import.go)
        → nextImportedID (internal/service/profilestore/import.go)
          → profileExists (internal/service/profilestore/import.go)
```

## Architecture Diagram

```mermaid
graph TD
    Main["cmd/model-loader<br/>(entry point)"]

    subgraph UI["UI Layer (internal/ui)"]
        Root["Ui / Root model<br/>tab routing + input gate"]
        Pages["Pages<br/>5 tabs"]
        Components["Components<br/>Modal · Picker · Sparkline · Statusbar"]
        Theme["Theme"]
    end

    subgraph Web["Web Editor (internal/service/configweb)"]
        ConfigWeb["configweb<br/>on-demand HTTP server<br/>schema-driven form builder"]
    end

    subgraph Services["Service Layer (internal/service)"]
        ProcMgr["Processmgr<br/>process lifecycle + recovery"]
        ProfStore["Profilestore<br/>FS CRUD"]
        DownloadMgr["Downloadmgr<br/>queued HF downloads"]
        HfHub["Hfhub<br/>HF API client"]
        ModelScan["Modelscanner<br/>GGUF metadata"]
        Monitor["Monitor<br/>nvidia-smi GPU"]
        Proxy["Httpproxy +<br/>Proxysupervisor"]
        Catalog["Backendcatalog +<br/>Backendschema"]
        LlamaBin["Llamabin<br/>binary resolver"]
        LlamaHelp["Llamahelp<br/>--help parser"]
        Validator["Validator<br/>flag validation"]
        BenchmarkSvc["Benchmark<br/>+ benchmarkstore"]
    end

    Domain["Domain<br/>Profile · Instance · Model · FlagSchema"]

    Main --> Root
    Main --> DownloadMgr
    Main --> ProcMgr
    Main --> Catalog

    Root --> Pages
    Pages --> Components
    Components --> Theme

    Pages --> ProfStore
    Pages --> DownloadMgr
    Pages --> ModelScan
    Pages --> ProcMgr
    Pages --> Monitor
    Pages --> Proxy
    Pages --> BenchmarkSvc
    Pages --> ConfigWeb

    ConfigWeb --> ProfStore
    ConfigWeb --> Catalog
    ConfigWeb --> Validator

    DownloadMgr --> HfHub
    ProcMgr --> Catalog
    Catalog --> LlamaBin
    Catalog --> LlamaHelp
    Proxy --> ProcMgr
    BenchmarkSvc --> Proxy

    ProfStore --> Domain
    ProcMgr --> Domain
    ModelScan --> Domain
    Catalog --> Domain
    Validator --> Domain
```

## Notable Design Decisions

- **Process survival:** background `llama-server` processes are intentionally orphaned on TUI exit; `processmgr.Reconcile` restores them from `~/.local/state/model-loader/instances.json` at boot.
- **Atomic persistence:** state records (downloads, instances, profiles) are written through `fsx.WriteJSONAtomic` (write-temp + rename) so a crash mid-write never corrupts persisted JSON.
- **Input routing:** every global shortcut consuming a printable rune must be gated behind `activePageCapturesInput()`; pages with active forms / pickers / modals implement `InputCapture`. See `CLAUDE.md` → TUI INPUT ROUTING RULES.
- **Web profile editor:** Profile create/edit no longer uses an in-TUI `huh` form. The Profiles tab launches an on-demand HTTP server (`configweb`) bound to `127.0.0.1:0`, opens the browser, and shows an "editing in browser..." modal. On save/cancel the server shuts down and the TUI reloads the list.
- **Embedded schema:** the `llama-server --help` schema is pinned to build `v7376 (380b4c9)`; parsed at runtime if the binary is present, falling back to the embedded copy.
- **Schema-driven profiles:** Profiles are schema-aware. A `SchemaBuilder` constructs structured `FlagSchema` per backend kind; the profile editor maps drafts via `ApplyToWithSchema`/`ToProfileWithSchema`, and the validator runs `Validate(profile, FlagSchema, BackendKind)` so flag checks adapt to the selected backend.
- **Essentials seed is curated UX:** `essentialSeed` in `backendschema/presentation.go` is a hand-picked list of per-backend flags — not a generic form abstraction. Do not extend without explicit user request.
- **Profile schema is canonical:** `docs/profile-schema.json` is the authoritative JSON Schema for the persisted profile file format. Any change to `domain.Profile` must mirror it there.
- **Config / state paths:** config at `~/.config/model-loader/config.toml`, profiles at `~/.config/model-loader/profiles/`, backends at `~/.config/model-loader/backends/`, state at `~/.local/state/model-loader/`.
