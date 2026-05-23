# Architecture

**Project:** model-loader  
**Generated:** 2026-05-23 from GitNexus knowledge graph  
**Stats:** 309 files · 7,414 symbols · 30,571 relationships · 300 execution flows  
**Indexed commit:** 0cc9eae

## Overview

`model-loader` is a terminal UI (TUI) application for managing llama.cpp profiles and `llama-server` processes. Built with **Go 1.26** and the **Charmbracelet bubbletea** stack, it provides a 5-tab interface for model discovery, profile editing, server lifecycle management, benchmarking, and an OpenAI-compatible HTTP proxy.

The codebase follows a domain-driven layout: a thin TUI layer (`internal/ui/`) drives focused services (`internal/service/`) over a shared domain model (`internal/domain/`). Background `llama-server` processes intentionally survive TUI exit and are recovered at boot from persisted state in `~/.local/state/model-loader/instances.json`.

## Functional Areas

GitNexus clusters the codebase into 27 functional modules. The most significant by symbol count and cohesion:

| Module | Symbols | Cohesion | Responsibility |
|--------|---------|----------|----------------|
| **Pages** | 453 | 73% | 5 TUI tabs (Profiles, Models, Backends, Server, Benchmark) + page logic |
| **Components** | 251 | 82% | Reusable UI widgets: Help, Modal, Picker, Sparkline, Statusbar |
| **Cli** | 196 | 71% | Subcommand dispatch, argument parsing, TUI/CLI dual entry |
| **Benchmark** | 179 | 81% | SWE-bench Lite + long-context needle probe engine |
| **Backendschema** | 117 | 84% | Schema generation orchestrator across all backend kinds |
| **Processmgr** | 114 | 82% | `llama-server` process lifecycle + instance recovery |
| **Profilestore** | 81 | 82% | FS-based profile CRUD |
| **Ui** | 69 | 83% | Root model, tab orchestration, global input routing |
| **Downloadmgr** | 62 | 84% | HuggingFace file downloader with progress events + state |
| **Configweb** | 60 | 78% | On-demand HTTP server for web-based profile editing |
| **Httpproxy** | 49 | 85% | OpenAI-shaped reverse proxy |
| **Monitor** | 46 | 90% | GPU metrics via `nvidia-smi` |
| **Validator** | 39 | 83% | Flag validation rules + cross-field rule engine |
| **Modelscanner** | 35 | 89% | GGUF model metadata extraction |
| **Llamahelp** | 31 | 89% | `llama-server --help` parser + embedded schema (v7376) |
| **Domain** | 24 | 70% | Shared types: Profile, Instance, Model, BackendValidationSchema |
| **Hfhub** | 21 | 74% | HuggingFace Hub API client (search, file listing) |
| **Backendcatalog** | 16 | 76% | Multi-backend catalog + binary resolver |
| **Proxysupervisor** | 14 | 98% | HTTP proxy lifecycle state machine |
| **Llamabin** | 12 | 76% | Binary path resolution (PATH + Python fallback) |

Plus smaller modules: Config, Log, Migration, Metricsstore, Playground, SGLang/vLLM/DFlash/buun help embeds, and internal `fsx` atomic-write helper.

## Key Execution Flows

The graph holds 300 flows. The five highest-impact cross-community traces, read directly from the knowledge graph:

### 1. Backend Binary Probing — `Probe → CheckExecutable` (7 steps)

Cross-community flow connecting **Backendcatalog** and **Llamabin**. Health-checks a configured backend by resolving its executable (PATH lookup, then a Python-launcher fallback) before probing it.

```
Probe (backendcatalog/probe.go)
  → probeOne (backendcatalog/probe.go)
    → resolveExecutable (backendcatalog/resolver.go)
      → ResolveCommandWithPythonFallback (llamabin/resolver.go)
        → Resolve (llamabin/resolver.go)
          → resolveInPATH (llamabin/resolver.go)
            → checkExecutable (llamabin/resolver.go)
```

### 2. Benchmark Streaming — `Execute → StreamChunk` (6 steps)

Cross-community flow connecting **Benchmark** and its OpenAI-compatible client. Drives an instruction-consistency benchmark against a running backend instance and parses streaming SSE chunks.

```
Execute (benchmark/instructionbench.go)
  → runInstructionBench (benchmark/instructionbench.go)
    → runInstConsistency (benchmark/instructionbench.go)
      → Complete (benchmark/client.go)
        → parseChunk (benchmark/client.go)
          → streamChunk (benchmark/client.go)
```

### 3. Download Persistence — `Init → WriteJSONAtomic` (6 steps)

Cross-community flow connecting **Cli**, **Downloadmgr**, and the internal `fsx` helper. A download worker streams an HF file to disk and atomically persists its state record so progress survives a crash.

```
init (cli/download.go)
  → RunWorker (downloadmgr/worker.go)
    → runDownload (downloadmgr/worker.go)
      → streamWithProgress (downloadmgr/worker.go)
        → SaveRecord (downloadmgr/state.go)
          → WriteJSONAtomic (internal/fsx/atomic_write.go)
```

### 4. Config Bootstrap — `Init → ApplyDefaults` (6 steps)

Cross-community flow connecting **Cli**, **app**, and **Config**. A profile subcommand bootstraps the application, which loads the TOML config and fills in defaults for any unset key.

```
init (cli/profile_crud.go)
  → profileMutationRunE (cli/profile_crud.go)
    → Bootstrap (app/bootstrap.go)
      → Load (config/config.go)
        → LoadFrom (config/config.go)
          → applyDefaults (config/config.go)
```

### 5. Profile Picker Scan — `Update → PickerScanClosedMsg` (7 steps)

Cross-community flow spanning **Pages** and **Components**. When an async model/profile scan goroutine finishes, the picker component emits `PickerScanClosedMsg` back up through the page's `Update` loop.

```
Update (ui/pages/profiles_update.go)
  → handlePickerScan (ui/pages/profiles_update.go)
    → updatePicker (ui/pages/profiles_picker.go)
      → Update (ui/components/picker.go)
        → handleScanStarted (ui/components/picker.go)
          → pickerWaitForEvent (ui/components/picker.go)
            → PickerScanClosedMsg (ui/components/picker.go)
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
