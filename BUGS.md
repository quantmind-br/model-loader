# BUGS — model-loader

**Status legend:** 🔴 Open · 🟡 Partial / Mitigated · 🟢 Fixed · ⚪ Won't fix / by design / N/A · ❓ Unknown

**Severity legend:** **C** Critical · **H** High · **M** Medium · **L** Low · **I** Info

All detailed bug reports and known limitations are documented in `AGENTS.md`. This file is a compact reference.

## Summary

The following table lists all identified bugs (including those that have been fixed) and known limitations.

| ID | Sev | Status | Component | One-line |
|----|-----|--------|-----------|----------|
| L3 | I | 🟢 | `internal/service/monitor/metrics.go` | Regex is llama.cpp-specific (harmless wasted work) |
| B1 | C | 🟢 | `internal/ui/root.go` | Tab/Shift+Tab swallowing inputs in forms & sub-views |
| B4 | H | 🟢 | `models_actions.go` | Models action menu missing "Use in existing profile" |
| B5 | H | 🟢 | `models_actions.go` | huh form in Models action menu doesn't submit on Enter |
| B6 | M | 🟢 | `statusbar.go` | Status bar global hint omits [?] help |
| B7 | M | 🟢 | `profiles.go` | Profile detail-pane hint omits [L] (collapsed into B3) |
| B8 | M | 🟢 | `fs_store.go` | Duplicate carried the source port; risked bind collision |
| B9 | M | 🟢 | `models_scan.go` | Invalid scan path in Models header — truncated and now removable in-app |
| B10 | L | 🟢 | `models_messages.go` | Models filter shows `filter: ""` while filtering (race) |
| B11 | L | 🟢 | `profiles_update.go` | ProfilesPage does not reload on tab focus |
| B12 | L | 🟢 | `profiles.go` | Editor preserves draft after accidental global Tab |
| B13 | Doc | 🟢 | `server_update.go` | Monitor footer claimed [Tab] cycle view while Tab was global |
| D1 | M | 🟢 | `README.md` | Both rewritten for multi-backend; README shortcuts realigned to help.go |
| D2 | M | 🟢 | `AGENTS.md` | Schema version v9761 everywhere |
| D3 | M | 🟢 | `AGENTS.md:162` | Schema version: embedded-v9761 |
| D4 | M | 🟢 | `internal/service/monitor/AGENTS.md` | 6 goroutines per subscription — confirmed correct |
| D5 | M | 🟢 | `internal/service/processmgr/AGENTS.md` | Log path documented as `<profile-id>-<port>.log` |
| D6 | L | 🟢 | `internal/service/AGENTS.md` | Service-layer KB v7376 → v9761 |
| T1 | M | 🟢 | `monitor/logs_test.go` | Regression test for partial-line flush added |
| T2 | M | 🟢 | `monitor/subscribe_test.go` | Backpressure-drop test added |
| T3 | L | 🟢 | `processmgr/manager_test.go` | PYTHONUNBUFFERED=0 override already covered |
| T4 | M | 🟢 | TUI responsive layout | Negative-width safeguards in centeredDivider/renderBar |
| S1 | M | 🟢 | curated enums + `validator/rules.go` | list-valued enum + extraArgs passthrough + draft-dflash |
| N1 | M | 🟢 | `scripts/setup-sndr-backend.sh` | SNDR_WHEEL_INDEX override for rotating index |
| N2 | M | 🟢 | `docs/sndr-backend.md` + SNDR profiles launch.env | GENESIS_ENFORCE_VERSION_RANGE=1 added |
| P1 | H | 🟢 | `internal/cli/serve.go` + `config.go` | `HealthCheckTimeout` made configurable + default raised to 360s |
| P3 | M | 🟢 | `internal/service/processmgr/enrichment.go` + `readiness.go` | Liveness check added to WaitHealthy/WaitReady |
| BM1 | M | 🟢 | `internal/service/benchmark/llamabench_probe.go` | llama-bench fill-90 prompt sizing fixed |
| DL1 | M | 🟢 | `internal/service/downloadmgr/pathing.go` | --snapshot checks file instead of directory |
| DL2 | M | 🟢 | `internal/service/downloadmgr/manager.go` | Queued downloads auto-promoted instead of abandoned |
| V1 | H | 🟢 | `backends/vllm-nightly/backend-build.sh` + venv | Build script + venv rebuilt; fp8-KV inference restored |

For full descriptions of all issues, including those considered by design, see `AGENTS.md`.

## Verification

- `go build ./...` → exit 0.
- `go test ./...` → all packages OK.