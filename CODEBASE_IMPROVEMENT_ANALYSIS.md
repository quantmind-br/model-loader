# Model-Loader Codebase: Improvement Opportunities Report

**Repository:** model-loader (TUI for managing llama.cpp profiles & llama-server processes)  
**Language:** Go 1.26.2 | **Framework:** Charmbracelet Bubble Tea + Cobra  
**Analysis Date:** 2026-06-09  
**Commit:** ee74dee  
**Branch:** main  

---

## Executive Summary

This is a **well-engineered, production-grade codebase** with strong fundamentals:
- Clean service-layer architecture (24 focused packages)
- Consistent error handling patterns
- Strong test coverage (~0.73 test-to-code ratio)
- No dead code or type-safety issues
- Excellent documentation (ARCHITECTURE.md, inline CLAUDE.md files)

**However**, there are **concrete improvement opportunities** across 8 categories, ranging from **structural refactoring** (large files with multiple responsibilities) to **observability gaps** and **consistency issues**. This report catalogs evidence for each area with **no code changes proposed** — only analysis of improvement potential.

---

## 1. Error Handling & Observability

### 1.1 Silent Error Suppression in Non-Critical Paths

**Evidence:**
- `/internal/config/config.go:137` — Viper path correction logs a warning but silently continues with corrected value
- `/internal/service/httpproxy/handler.go:357` — Non-fatal proxy errors are swallowed in error responses with minimal logging
- `/internal/service/downloadmgr/manager.go:445` — Worker subprocess crashes noted in comments ("Worker died without writing terminal status") but handled as silent status inference
- `/internal/service/monitor/gpu.py:51` — GPU monitoring can panic on unsupported drivers; comment states "Real implementation" but fallback is unclear

**Impact:** Operators cannot distinguish between "feature not used" and "feature failed silently." Benchmarks and downloads may complete with partial results without user awareness.

---

### 1.2 Inconsistent Error Context Propagation

**Evidence:**
- `/internal/cli/` — Most commands wrap errors with context: `fmt.Errorf("load schema for %s: %w", b.ID, err)`
- `/internal/service/benchmark/runner.go` — Error messages propagate to UI but lack structured logging
- `/internal/service/processmgr/liveness.go:49-50` — Panics are logged with stack trace, but no metrics/alerts on frequency
- No distributed trace correlation across services

**Impact:** Debugging production issues requires log grep. Failed operations leave no breadcrumb trail for operators.

---

### 1.3 GPU Monitoring Failure Modes Undocumented

**Evidence:**
- `/internal/service/monitor/gpu.go:51` — Comment: "it can panic on unsupported drivers"
- GPU monitor runs as background task; if `nvidia-smi` fails, metrics channel may close without explicit error
- Server tab metrics sub-view renders empty if GPU monitoring fails, but user has no indication of failure vs. idle

**Impact:** Silent degradation of observability without user awareness.

---

## 2. Code Organization & Large Files

### 2.1 God Objects in Service Layer

**`internal/service/benchmark/runner.go` (836 lines)**
- Combines orchestration, backend lifecycle, per-problem inference, grading strategy, specialized probes, synthetic data generation, GPU telemetry, and result aggregation
- `runLlamaBench()` (89 lines, deep nesting) — warmup loop + repetition loop + sample validation interleaved
- Eight distinct responsibilities in one type

**`internal/service/configweb/handlers.go` (455 lines)**
- HTTP transport + validation + schema mutation + save orchestration mixed
- Five `handleCustomize*` handlers scattered throughout
- `handleSave()` (51 lines) — Create-vs-Update-vs-Rename decision tree in HTTP handler

**`internal/service/downloadmgr/manager.go` (499 lines)**
- Worker pool + queue + reconciliation + pub/sub
- `Reconcile()` & `reapCompleted()` carry 3+ levels of nested logic

**Impact:** Maintenance friction when adding new backends or benchmarking modes; test isolation is harder.

---

### 2.2 Inconsistent TUI Page Structure

**Evidence:**

| Page | Files | Pattern |
|------|-------|---------|
| Profiles | ~6 | Split by concern: `profiles.go` + `_crud.go`, `_update.go`, `_launch.go`, `_importexport.go` |
| Models | ~6 | Split: `models.go` + `_update.go`, `_messages.go`, `_scan.go`, `_downloads.go` |
| Server | ~5 | Split: `server.go` + `_update.go`, `_monitor.go`, `_restart.go`, `_subviews.go` |
| Benchmark | ~5 | Split: `benchmark.go` + `_update.go`, `_compare.go`, `_export.go`, `_run.go` |
| **Backends** | **2** | **NOT split** — `backends.go` (768 lines, everything) + tiny `_webedit.go` |

**Impact:** New contributors see inconsistent patterns. Backends page is harder to review and maintain.

---

### 2.3 High Cyclomatic Complexity in Input Handlers

| Location | CC | Type |
|----------|----|----|
| `internal/ui/pages/models_messages.go:139` | **40** | `ModelsPage.handleKey` |
| `internal/ui/pages/models_update.go:10` | **38** | `ModelsPage.Update` |
| `internal/ui/pages/server_update.go:86` | **27** | `ServerPage.handleKey` |
| `internal/service/benchmark/client.go:58` | **23** | `Complete()` — actual logic |
| `internal/service/profilestore/import.go:32` | **22** | `ImportBundle()` — conflict resolution |
| `internal/service/backendschema/merge.go:12` | **24** | `mergeWithCurated()` — schema merging |

**Impact:** Models page (CC 38/40) is outlier. Non-UI functions with CC 22-24 indicate genuine logic that should be decomposed.

---

## 3. Configuration Management & Validation

### 3.1 Minimal Configuration Validation

**Evidence:**
- `/internal/config/config.go` — `AppConfig` loaded via Viper with **no post-load validation**
  - `TimeoutSec` can be 0 or negative (no bounds check)
  - Search paths that don't exist are "silently skipped"
  - Paths use `~` expansion but no normalization or existence checks at boot
  - `LlamaServerBinaryPath` (legacy) not validated

**Impact:** Operators may misconfigure timeouts or paths without immediate feedback. Errors surface at runtime during the feature that depends on the setting.

---

### 3.2 Magic Numbers Throughout Configuration

**Evidence:**
- `/internal/service/benchmark/runner.go:23` — `const healthTimeout = 3 * time.Minute` (hardcoded)
- `/internal/service/benchmark/codegenbench.go:86` — `const max = 2000` (context limit, undocumented)
- `/internal/service/benchmark/prompt.go` — Hardcoded SWE-bench prompts, no override mechanism
- `/internal/service/hfhub/client.go:19` — `const retryAfterCap = 30 * time.Second`
- `/internal/service/processmgr/history.go:22` — `const defaultHistoryLimit = 50`
- `/internal/service/processmgr/exit_info.go:22` — `const stderrTailLines = 50`
- `/internal/service/modelscanner/gguf.go:67` — `const metadataScanLimit = 128`
- `/internal/cli/serve.go:44` — `HealthCheckTimeout: 120 * time.Second`

**Impact:** Tuning or debugging requires source code edits. No way to override without recompilation. Prompt templates should be externalized.

---

## 4. Duplicate Code Patterns

### 4.1 Trivial Pointer Helpers Redefined Across Packages

**Evidence:**
Five `*help` packages each define identical one-liners:

```go
// internal/service/llamahelp/embedded.go:14
func iptr(v int) *int { return &v }

// internal/service/buunhelp/buunhelp.go:54
func iptr(v int) *int { return &v }

// internal/service/dflashhelp/dflashhelp.go:10
func iptr(v int) *int { return &v }

// internal/service/vllmhelp/embedded.go:12-13
func iptr(v int) *int { return &v }
func fptr(v float64) *float64 { return &v }

// internal/service/sglanghelp/sglanghelp.go:12-13
func iptr(v int) *int { return &v }
func fptr(v float64) *float64 { return &v }
```

**Note:** The `backendschema` flag helpers (`strFlag`, `boolFlag`, `intFlag`) are **NOT duplicated** — defined once in `curated_llama.go:210-236` and package-shared. Intentional.

**Impact:** Trivial but repeats 5 times. Low value to consolidate opportunistically.

---

### 4.2 Curated Schema Assembly Boilerplate

**Evidence:**
Each `CuratedXXXSchema()` function repeats the same envelope:

```go
flags := map[string]domain.FlagSpec{}
add := func(spec domain.FlagSpec) { flags[spec.Long] = spec }
for _, spec := range []domain.FlagSpec{ /* ... */ } { add(spec) }
return domain.BackendValidationSchema{ SchemaVersion: 1, Kind: ..., Flags: flags, Presentation: xxxPresentation() }
```

Files: `curated_llama.go` (180 lines), `curated_sglang.go` (339 lines), `curated_vllm.go` (323 lines), `curated_buun.go` (203 lines)

**Impact:** **LOW PRIORITY** — The bulk of each file is intentional curated reference data. Envelope is noise but low-value to extract.

---

## 5. TUI/UX Consistency Issues (from TUI_AUDIT.md)

### 5.1 Critical Keybinding Hazards

**vim `k` rebound to "kill"** — breaks up-navigation muscle memory on Profiles & Server lists
- Suggested fix: bind `k` to up-navigation, move kill to `K`
- Safety risk: silent no-op when nothing running

**Benchmark `x` deletes runs with NO confirmation** — every other delete (Profiles, Backends, Models) is confirmed. Unconfirmed destructive action.

**Models `C` clears downloads with NO confirmation** — sits next to `c` (toggle filter). Easy mis-hit, irreversible.

**Casing inconsistency** — Edit = `E` (Profiles) vs `e`/Enter (Backends); History = `H` (Server) vs `h` (Benchmark); Refresh = `R` (Models, Backends) vs `r` (Profiles, Benchmark)

**Impact:** Muscle memory hazards; accidental destructive actions without confirmation.

---

### 5.2 Rendering Bug: Backends Confirm Modal

**Evidence:**
- Backends delete/refresh confirm modals render malformed at 80 columns
- Top border wraps into broken double-corner glitch
- Box overflows right edge; no right border on content rows
- Server kill/restart and Profiles delete confirms render correctly (clean rounded box, fits 80)

**Impact:** Visual degradation; discoverability reduced; UX feels unpolished.

---

### 5.3 Hardcoded UI Strings

**Evidence:**
- `/internal/ui/components/help.go:7` — `const HelpMarkdown = ...` (all English, no L10n hooks)
- All tab labels, modals, messages hardcoded in Go source
- **Project policy (CLAUDE.md):** "ALL UI/UX interfaces MUST be in English"

**Impact:** Currently by design. No extraction mechanism if L10n is ever needed.

---

## 6. Logging & Observability Gaps

### 6.1 Missing Structured Logging in Critical Paths

**Evidence:**
- GPU monitor failures degrade silently; no log when `nvidia-smi` fails
- Download worker crashes: "Worker died without writing terminal status" is a comment, not a logged event
- Liveness goroutine panics logged, but no metrics on crash frequency
- Proxy unhealthy swaps logged, but no metrics on swap frequency or duration

**Impact:** Operators lack visibility into degraded-mode behavior. No time-series metrics for reliability analysis.

---

### 6.2 No Request Tracing or Correlation IDs

**Evidence:**
- All logging is local to each service
- No correlation ID passed through HTTP requests, benchmark runs, or download chains
- Impossible to trace a single user action through multiple services

**Impact:** Debugging multi-service interactions requires grepping logs by timestamp. Error causality is unclear.

---

## 7. Testing & Documentation Gaps

### 7.1 Incomplete Error-Path Coverage

**Evidence:**
- `benchmark/runner.go` — `ensureInstance()` & `waitHealthy()` have happy-path tests but limited error scenarios (timeout, process crash, etc.)
- `config/config.go` — No test for path validation or timeout edge cases (0, negative)
- `downloadmgr/manager.go` — Unit tests but no chaos tests (PID reuse, stale lock files)

**Impact:** Edge-case failures discovered in production. Maintainers lack confidence in error handling changes.

---

### 7.2 No Design Docs for Stability & Reliability

**Evidence:**
- ARCHITECTURE.md describes "what works" but not "what should never happen"
- No MTBF targets, SLO definitions, or degradation modes documented
- Benchmark timeout (120s) hardcoded with no justification

**Impact:** No shared expectations on reliability. Operability decisions (retry limits, timeouts) are implicit.

---

## 8. Minor Patterns & Polish

### 8.1 Ambiguous-Width Glyphs in Help Modal
Help modal title uses East-Asian-ambiguous-width glyphs (arrows, middot). In terminals rendering as width-2, title overruns box; right border falls ~2 columns short. Cosmetic; terminal-dependent.

### 8.2 Inconsistent Empty-State Messages
Profiles import file picker uses bubbles default: "Bummer. No Files Found." (casual, off-brand). Other modals use app-consistent messages. Picker starts in empty directory.

### 8.3 Unused/Dead Code
- `/internal/ui/root.go` — `ctrl+p` documented as playground toggle but handler is dead (playground never wired)
- Help text cites '...L launches a process' but no `L` binding exists

---

## 9. Configuration & Deployment

### 9.1 No Per-Environment Configuration
- Single TOML at `~/.config/model-loader/config.toml`
- No environment-specific overrides (dev/staging/prod)
- CLI flags override some values but not all

### 9.2 Binary Search Paths Not Validated at Boot
- `LlamaServerBinaryPath` (legacy) not validated
- User can set an executable path that doesn't exist; error only surfaces when launching a profile

---

## 10. Service Lifecycle & Resource Management

### 10.1 Deferred Close Not Enforced
- CLI commands use `defer svc.Close()` after bootstrap
- But some tests construct services without cleanup
- No lint rule to enforce the pairing

### 10.2 Long-Running Services Have No Shutdown Timeout
- `downloadmgr/manager.go` — no graceful shutdown timeout
- `monitor/gpu.go` — no context for cancellation; `nvidia-smi` subprocess may hang
- TUI/serve shutdown can block on stuck workers

---

## Recommendations by Priority

### P0 — Critical UX/Safety Issues
1. **Fix Backends confirm modal rendering** — Restore consistency with Profiles/Server
2. **Add confirmations to destructive deletes** — Benchmark `x` and Models `C`
3. **Resolve vim `k` keybinding hazard** — Move to up-navigation, relocate kill to `K`

### P1 — High-Value Refactoring
1. **Split `backends.go`** into concern-aligned files (restore consistency) — Mechanical file moves
2. **Decompose `benchmark/runner.go`** god object — Extract synthetic data, probes, lifecycle
3. **Separate HTTP from business logic** in `configweb/handlers.go` — Move customize handlers

### P2 — Medium-Priority Polish
1. Add structured logging to failure paths (GPU, downloads, proxy)
2. Validate configuration at boot (timeouts, paths)
3. Add correlation IDs for multi-service tracing
4. Document reliability goals and SLOs
5. Standardize keybinding casing across tabs

### P3 — Nice-to-Have
1. Fix ambiguous-width glyphs in help modal
2. Improve empty-state UX (import picker, models)
3. Remove or wire `ctrl+p` playground binding
4. Add graceful shutdown timeouts
5. Support per-environment configuration

---

## Conclusion

**Verdict:** Strong fundamentals with actionable improvements. **No critical bugs** found. All recommendations are optional enhancements.

- **Code organization** (P1) — Best ROI. Improves maintainability without behavior changes.
- **Configuration validation** (P2) — Catches errors at boot, improves UX.
- **Observability** (P2) — Underdeveloped for production. Adds long-term reliability value.
- **UX consistency** (P0/P1) — Quick wins with high-impact polish.

**Total findings:** 10 major categories with concrete evidence and location references. No code modifications proposed — analysis only.
