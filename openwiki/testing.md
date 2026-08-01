---
type: Reference
title: Testing and QA
description: Test conventions for model-loader — stdlib testing only with hand-rolled doubles, white-box by default, the single golden fixture pair for llama-server --help, regression-test bundles linked to BUGS.md ids, and the two-command quality gate.
tags: [testing, qa, golden, regression, bugs]
---

# Testing & QA

model-loader uses **stdlib `testing` only** for assertions — no `testify`, `gomock`, `mockery`, or `go-cmp`. The only test-only third-party import is `github.com/charmbracelet/x/exp/teatest` in two files (`internal/ui/root_test.go`, `internal/ui/pages/profiles_test.go`).

## Conventions

- **`package <x>` (white-box)** by default — needed to seed unexported fields. The only black-box exceptions are `internal/app/{bootstrap,lock}_test.go` (`package app_test`).
- File naming: `*_test.go` next to source. No `tests/` subdir.
- Function naming: `TestFoo` for standard cases; `TestFoo_bar_baz` for scenarios. Sub-tests via `t.Run` always inside table-driven loops over a `tests := []struct{…}{…}` literal.
- Hermeticity: `t.Setenv("NO_COLOR", "")`, `HOME`, `XDG_CONFIG_HOME`, etc. `t.TempDir()` everywhere; no `ioutil.TempDir`.
- Doubles are hand-rolled (`stub*`, `fake*`) — never a mock framework. Status-sequencing pattern: doubles like `fakeProxy` carry a `statusSeq []httpproxy.Status` field; `Status()` pops from head and falls back to a default once empty.

## Canonical helpers

| Helper | Where |
|--------|-------|
| `freePort(t)` | `internal/service/processmgr/manager_test.go` (duplicated as `freePortSup` in `proxysupervisor/supervisor_test.go`). |
| `fakeBinary(t)` | `processmgr/manager_test.go` — abs path to `testdata/fake-llama-server.sh`. |
| `newTestManager(t)` | `processmgr/manager_test.go`. |
| `spawnGroupLeader(t)` | `processmgr/lifecycle_audit_test.go` — Setsid leader + child PID. |
| `newTestServer(t, store, mgr)` | `httpproxy/swap_test.go`. |
| `newAnthropicMux(t, store, mgr)` | `httpproxy/anthropic_handlers_test.go`. |
| `startBackend(t, respond)` | `httpproxy/anthropic_handlers_test.go` — fake upstream + `upstreamCapture` channel. |
| `runStream(t, script)` | `httpproxy/anthropic_stream_test.go` — drives `runAnthropicStream` and parses SSE. |
| `mustTranslate{Anthropic,Gemini,Responses}(t, raw)` | `httpproxy/*_translate*_test.go`. |
| `drainCmd(cmd)` | `internal/ui/pages/server_test.go` — flattens `BatchMsg` and returns leaves. |
| `stubStore` / `stubManager` | `httpproxy/mocks_test.go` — most-reused doubles (full in-memory `profilestore.Store` + `processmgr.Manager`). |
| `shrinkWatchdogTiming(t, …)` | `benchmark/terminalbench_watchdog_test.go` — swap+restore package vars via `t.Cleanup`; shared with `agentic_watchdog_test.go`. |

## Golden files

Exactly one canonical pair at `testdata/` (recently renamed from `help-v9761` to `help-v10152` during the schema-sync work):

```
testdata/help-v10152.txt         # input fixture (real --help capture)
testdata/help-v10152.golden.json # expected parsed JSON
```

Generator: `internal/service/llamahelp/parser_test.go::TestParseHelp_Golden`:
```go
var updateGolden = flag.Bool("update", false, "regenerate golden files")
if *updateGolden { os.WriteFile(goldenPath, got, 0o644); … }
// else: bytes.Equal + fail with hint: go test -update
```

A duplicate `//go:embed` copy at `internal/service/backendschema/testdata/help-v10152.golden.json` **must be kept byte-identical by hand** when the root golden is regenerated. `//go:embed` is used heavily in production code (curated schemas, benchmark datasets, configweb assets) but **in zero** `_test.go` files.

## Test categories

- **Pure unit (no I/O):** `internal/domain/*`, `internal/log/*`, `internal/service/internal/{fsx,shellsplit,…}`.
- **httptest-based (no real processes):** `httpproxy/*`, `configweb/*`, `hfhub/*`, `backendcatalog/*`, `downloadmgr/*`.
- **Real processes + port allocation:** `processmgr/*`, `proxysupervisor/*`, `internal/service/internal/procutil/*`, `benchmark/*_watchdog_test.go`, `benchmark/{llamabench,longcontext}_probe_test.go`.
- **CLI-level integration:** ~27 files in `internal/cli/*`. Uses `bytes.Buffer` as `io.Writer`; calls leaf command funcs directly; never `exec.Command` of the binary itself.
- **TUI integration:** `internal/ui/root_test.go` + `profiles_test.go` use `teatest.NewTestModel` at 120×30. The rest of `internal/ui/pages/*_test.go` is pure-function style: construct page struct, invoke `Update`, call `drainCmd(cmd)`, assert on `[]tea.Msg`.

## Regression tests and BUGS linkage

Regression tests cite their `BUGS.md` id in a comment and are **not** named `Test*Regression*` — they describe the scenario and keep the audit id in a comment.

Two dedicated regression bundles:
- `internal/service/benchmark/br_regression_test.go` — **BR1–BR8** (benchmark reliability).
- `internal/service/benchmark/reliability_regression_test.go` — **T1/T3/T5/T7**.

Other themed regressions:
- `benchmark/terminalbench_watchdog_test.go` + `agentic_watchdog_test.go` — **UIUX-012** (hang watchdog).
- `processmgr/lifecycle_audit_test.go` — **AUD-A1/A3/A4/A5/A7/A10/A11/A12**.
- `profilestore/fs_store_test.go` — **PV1**.
- `validator/validator_test.go` — **S1**.

## Quality gate

```bash
go build ./...     # compile
go test ./...      # full suite
```

That is the entire gate. No linter, no formatter target, no CI. Before yielding, run both and confirm 0 exit codes. There is no coverage tooling in the repo (`.gitignore` excludes `coverage.out`/`coverage.html`).
