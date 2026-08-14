# Remove TokenSpeed Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove TokenSpeed as a model-loader backend, restore the nine-kind contracts, preserve the independent Unsloth configweb fix, and delete the incompatible 7.0 GiB local runtime tree.

**Architecture:** Perform a clean cutover rather than a compatibility deprecation: delete the TokenSpeed domain constant, schema/help implementation, generator, process-specific launch/readiness paths, validator allowance, operator surfaces, and documentation. Preserve shared behavior by restoring `WaitHealthy` as the sole `/health` HTTP poller, retaining Unsloth log-token readiness, and retaining the SGLang enum literal `tokenspeed_mla` because it is not a model-loader backend kind.

**Tech Stack:** Go 1.26.2, stdlib `testing`, Cobra CLI, Bubble Tea/configweb templates, Markdown/OpenWiki, shell verification.

## Global Constraints

- Implement as new commits on `main`; never reset, rebase, or rewrite the existing TokenSpeed-support commits.
- End with exactly nine built-in `BackendKind` values and nine registered schema generators.
- No compatibility aliases, deprecated TokenSpeed paths, dormant generators, or fake fallbacks.
- Preserve `domain.BackendKindUnsloth` in the configweb backend-kind selector.
- Preserve `tokenspeed_mla` in SGLang schema facts; it is an upstream SGLang attention backend.
- Do not modify the active catalog, profiles, or runtime registry: current evidence contains no TokenSpeed entries.
- Delete `backends/tokenspeed/` in full; the deletion intentionally reclaims approximately 7.0 GiB.
- Use test-first cycles for observable contract changes; deletion-only implementation follows only after the failing boundary tests exist.
- Format changed Go files with `gofmt`; final gate is `make build && go test ./...`.

---

### Task 1: Lock the nine-kind public boundary

**Files:**
- Modify: `internal/service/backendschema/register_test.go`
- Modify: `internal/cli/backend_test.go`
- Modify: `internal/service/configweb/render_test.go`
- Modify: `internal/service/configweb/backend_handlers.go`
- Modify: `internal/service/backendschema/register.go`

**Interfaces:**
- Consumes: `backendschema.RegisterDefaults(*Manager, backendcatalog.SchemaStore)`, `Manager.Generators() map[domain.BackendKind]Generator`, CLI `addBackend(io.Writer, backendManager, string, string, string) error`, configweb `Session.handleBackendIndex(http.ResponseWriter, *http.Request)`.
- Produces: exactly nine registered generator kinds; TokenSpeed rejected by CLI; configweb renders Unsloth and does not render TokenSpeed. The domain constant remains temporarily so later package-local removals compile independently.

- [ ] **Step 1: Change registration coverage to the desired nine-kind list**

In `internal/service/backendschema/register_test.go`, replace the current `want` slice with:

```go
want := []domain.BackendKind{
    domain.BackendKindLlamaServer,
    domain.BackendKindSGLang,
    domain.BackendKindVLLM,
    domain.BackendKindDFlash,
    domain.BackendKindBuunLlamaCpp,
    domain.BackendKindBeeLlamaCpp,
    domain.BackendKindIkLlamaCpp,
    domain.BackendKindUnsloth,
    domain.BackendKindTabby,
}
```

Keep both existing assertions: exact length and non-nil generator for every listed kind. This test must fail while TokenSpeed remains registered because `len(gens)` is 10 and `len(want)` is 9.

- [ ] **Step 2: Add the explicit CLI rejection regression**

Append to `internal/cli/backend_test.go`:

```go
func TestAddBackend_RemovedKindRejected(t *testing.T) {
    schemaStore := newFakeSchemaStore()
    mgr := backendschema.NewManager(nil, schemaStore)
    backendschema.RegisterDefaults(mgr, schemaStore)

    var out bytes.Buffer
    err := addBackend(&out, &fakeBackendManager{gens: mgr.Generators()}, "TokenSpeed", "/tmp/tokenspeed", "tokenspeed")
    if err == nil {
        t.Fatal("expected tokenspeed to be rejected as an unknown backend kind")
    }
    if !strings.Contains(err.Error(), `unknown backend kind "tokenspeed"`) {
        t.Fatalf("error = %q, want unknown-kind error", err)
    }
}
```

This uses the real default-generator registry rather than a hand-maintained fake list. It must fail before removal because TokenSpeed is still registered and `addBackend` accepts it.

- [ ] **Step 3: Add the configweb option regression**

Append to `internal/service/configweb/render_test.go`:

```go
func TestBackendPageKindsIncludeUnslothAndExcludeRemovedKind(t *testing.T) {
    s := &Session{deps: Deps{InitialBackendDraft: BackendDraft{IsNew: true}}}
    rec := httptest.NewRecorder()
    s.handleBackendIndex(rec, httptest.NewRequest("GET", "/backend/", nil))
    body := rec.Body.String()

    if !strings.Contains(body, `value="unsloth"`) {
        t.Fatalf("backend kind selector must include unsloth: %s", body)
    }
    if strings.Contains(body, `value="tokenspeed"`) {
        t.Fatalf("backend kind selector must not include tokenspeed: %s", body)
    }
}
```

It must fail before removal because the rendered selector still contains TokenSpeed. It also permanently protects the independent Unsloth fix from being reverted.

- [ ] **Step 4: Run the three boundary tests and verify RED**

Run:

```bash
go test ./internal/service/backendschema -run TestRegisterDefaults_RegistersAllGeneratorKinds -count=1
go test ./internal/cli -run TestAddBackend_RemovedKindRejected -count=1
go test ./internal/service/configweb -run TestBackendPageKindsIncludeUnslothAndExcludeRemovedKind -count=1
```

Expected: all three commands fail for the intended TokenSpeed-present reason; no compile failure and no unrelated assertion failure.

- [ ] **Step 5: Remove TokenSpeed from exposed registries**

Apply these minimal source changes:

- Delete `m.Register(domain.BackendKindTokenSpeed, NewTokenSpeedGenerator(schemaStore))` from `internal/service/backendschema/register.go`.
- Delete `domain.BackendKindTokenSpeed` from the `Kinds` literal in `internal/service/configweb/backend_handlers.go`; retain `domain.BackendKindUnsloth` and the other eight kinds in their existing order.

Keep `domain.BackendKindTokenSpeed` temporarily. Tasks 2–4 remove package-local references before Task 4 deletes the constant, avoiding an uncompilable intermediate commit.

- [ ] **Step 6: Run the boundary tests and verify GREEN**

Run the same three commands from Step 4.

Expected: all three pass.

- [ ] **Step 7: Format and commit the public-boundary change**

Run:

```bash
gofmt -w internal/service/backendschema/register.go internal/service/backendschema/register_test.go internal/cli/backend_test.go internal/service/configweb/backend_handlers.go internal/service/configweb/render_test.go
git add internal/service/backendschema/register.go internal/service/backendschema/register_test.go internal/cli/backend_test.go internal/service/configweb/backend_handlers.go internal/service/configweb/render_test.go
git commit -m "refactor: remove tokenspeed public registration"
```

---

### Task 2: Remove TokenSpeed schema and help ownership

**Files:**
- Delete: `internal/service/backendschema/tokenspeed_generator.go`
- Delete: `internal/service/backendschema/tokenspeed_generator_test.go`
- Delete: `internal/service/tokenspeedhelp/tokenspeedhelp.go`
- Delete: `internal/service/tokenspeedhelp/tokenspeedhelp_test.go`
- Modify: `internal/service/backendschema/presentation.go`
- Modify: `internal/service/backendschema/presentation_coverage_test.go`
- Verify unchanged: `internal/service/backendschema/curated_sglang.go`

**Interfaces:**
- Consumes: `essentialSeed map[domain.BackendKind][]string`, presentation coverage over embedded schemas.
- Produces: no TokenSpeed generator/help package or TokenSpeed presentation seed; SGLang still accepts the `tokenspeed_mla` enum literal.

- [ ] **Step 1: Remove TokenSpeed presentation references**

In `internal/service/backendschema/presentation.go`, delete the `domain.BackendKindTokenSpeed` entry from `essentialSeed`.

In `internal/service/backendschema/presentation_coverage_test.go`:

- Remove the `tokenspeedhelp` import.
- Remove `{domain.BackendKindTokenSpeed, tokenspeedhelp.EmbeddedSchema()}` from the table.

- [ ] **Step 2: Delete TokenSpeed-owned schema packages**

Delete these files/directories:

```text
internal/service/backendschema/tokenspeed_generator.go
internal/service/backendschema/tokenspeed_generator_test.go
internal/service/tokenspeedhelp/tokenspeedhelp.go
internal/service/tokenspeedhelp/tokenspeedhelp_test.go
```

Do not alter `curated_sglang.go`; its enum member `tokenspeed_mla` remains authoritative upstream SGLang schema data.

- [ ] **Step 3: Run schema package tests**

Run:

```bash
go test ./internal/service/backendschema/... -count=1
```

Expected: PASS; presentation coverage checks all remaining kinds and no import references `tokenspeedhelp`.

- [ ] **Step 4: Confirm the SGLang enum survives**

Run:

```bash
go test ./internal/service/backendschema -run 'TestCuratedSGLang|TestPresentation' -count=1
```

Expected: PASS. Then search `internal/service/backendschema/curated_sglang.go` for `tokenspeed_mla`; expected exactly the existing enum occurrence, unchanged.

- [ ] **Step 5: Format and commit schema removal**

Run:

```bash
gofmt -w internal/service/backendschema/presentation.go internal/service/backendschema/presentation_coverage_test.go
git add -A internal/service/backendschema internal/service/tokenspeedhelp
git commit -m "refactor: remove tokenspeed schema support"
```

---

### Task 3: Remove TokenSpeed launch and readiness behavior

**Files:**
- Modify: `internal/service/processmgr/args.go`
- Modify: `internal/service/processmgr/args_test.go`
- Modify: `internal/service/processmgr/enrichment.go`
- Modify: `internal/service/processmgr/readiness.go`
- Modify: `internal/service/processmgr/launch.go`
- Modify: `internal/service/processmgr/manager_test.go`
- Modify: `internal/service/processmgr/recover.go`
- Delete: `internal/service/processmgr/tokenspeed_readiness_test.go`
- Modify: `internal/service/backendcatalog/resolver.go`

**Interfaces:**
- Consumes: `BuildArgsForBackend`, `fsManager.WaitHealthy`, `fsManager.WaitReady`, `resolveExecutable`, `buildLaunchEnv`, `processTokens`.
- Produces: only the nine supported kinds dispatch; default readiness uses `/health`; Unsloth still uses log-token readiness; SGLang and VLLM retain Python fallback.

- [ ] **Step 1: Delete TokenSpeed-specific process tests**

- Delete `TestBuildArgsForBackend_TokenSpeed` from `internal/service/processmgr/args_test.go`.
- Delete `internal/service/processmgr/tokenspeed_readiness_test.go` in full.
- Remove `domain.BackendKindTokenSpeed` from the Python-unbuffered kind table in `internal/service/processmgr/manager_test.go`.

These are obsolete positive-support tests, not contracts to preserve.

- [ ] **Step 2: Remove launch dispatch and metadata**

Apply these source changes:

- `internal/service/processmgr/args.go`: remove the `BackendKindTokenSpeed` switch case and delete `buildTokenSpeedArgs` plus its TokenSpeed comment.
- `internal/service/processmgr/launch.go`: remove `BackendKindTokenSpeed` from `pythonUnbufferedKinds`.
- `internal/service/processmgr/recover.go`: remove the TokenSpeed `processTokens` entry.
- `internal/service/backendcatalog/resolver.go`: restore the Python-fallback switch to only `BackendKindSGLang` and `BackendKindVLLM`.

- [ ] **Step 3: Restore fixed `/health` ownership**

In `internal/service/processmgr/enrichment.go`, replace the wrapper/helper pair with a single `WaitHealthy` body:

```go
// WaitHealthy polls GET http://127.0.0.1:<port>/health with capped exponential
// backoff (100ms, 200ms, 400ms, ..., max 1s) until 200 OK or timeout.
func (m *fsManager) WaitHealthy(pid int, port int, timeout time.Duration, attemptID string) error {
    lg := m.logger.With("pid", pid, "port", port, "attempt_id", attemptID)
    lg.Info("healthcheck_start", "timeout", timeout)
    deadline := time.Now().Add(timeout)
    delay := 100 * time.Millisecond
    const maxDelay = time.Second
    url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
    client := &http.Client{Timeout: 2 * time.Second}
    for time.Now().Before(deadline) {
        if !procutil.Alive(pid) {
            lg.Warn("healthcheck_process_exited")
            return fmt.Errorf("port %d: %w", port, ErrProcessExited)
        }
        resp, err := client.Get(url)
        if err == nil {
            _ = resp.Body.Close()
            if resp.StatusCode == http.StatusOK {
                m.mu.Lock()
                inst, ok := m.tracked[pid]
                if ok {
                    inst.RestartCount = 0
                    m.tracked[pid] = inst
                }
                m.mu.Unlock()
                if ok && m.sink != nil {
                    _ = m.sink.MarkLastUsed(inst.ProfileID, time.Now().UTC())
                }
                lg.Info("healthcheck_ok")
                return nil
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
    lg.Warn("healthcheck_timeout")
    return fmt.Errorf("port %d: %w", port, ErrHealthCheckTimeout)
}
```

Preserve the existing restart-budget and `MarkLastUsed` comments if they improve local clarity; the behavioral requirement is exact `/health` probing with no selectable-path helper.

- [ ] **Step 4: Restore `WaitReady` to the two-mode contract**

In `internal/service/processmgr/readiness.go`:

- Change the function comment to: `Unsloth waits for its printed API key; every other kind polls /health and returns an empty token.`
- Delete the TokenSpeed branch.
- Keep the Unsloth log-token branch byte-for-byte in behavior.
- Leave the final `return "", m.WaitHealthy(...)` as the default.

- [ ] **Step 5: Run process, resolver, and readiness tests**

Run:

```bash
go test ./internal/service/processmgr/... -count=1
go test ./internal/service/backendcatalog/... -count=1
```

Expected: PASS. Existing health-check tests prove `/health`; existing Unsloth readiness tests prove log-token behavior; SGLang/VLLM resolver tests prove Python fallback remains.

- [ ] **Step 6: Format and commit process removal**

Run:

```bash
gofmt -w internal/service/processmgr/args.go internal/service/processmgr/args_test.go internal/service/processmgr/enrichment.go internal/service/processmgr/readiness.go internal/service/processmgr/launch.go internal/service/processmgr/manager_test.go internal/service/processmgr/recover.go internal/service/backendcatalog/resolver.go
git add -A internal/service/processmgr internal/service/backendcatalog/resolver.go
git commit -m "refactor: remove tokenspeed launch support"
```

---

### Task 4: Remove TokenSpeed validator allowance and domain constant

**Files:**
- Modify: `internal/service/validator/rules.go`
- Modify: `internal/service/validator/validator_test.go`
- Modify: `internal/domain/backend.go`
- Modify: `internal/domain/backend_kind_test.go`

**Interfaces:**
- Consumes: `supportsHFRepo(domain.BackendKind) bool`, `Validator.Validate`, `domain.BackendKind` constants.
- Produces: native Hugging Face repository IDs remain accepted only for VLLM, SGLang, and Unsloth; no `domain.BackendKindTokenSpeed` symbol remains.

- [ ] **Step 1: Remove the obsolete positive TokenSpeed test**

Delete `TestValidator_HFRepoIDAllowedForTokenSpeed` from `internal/service/validator/validator_test.go`.

The CLI unknown-kind test from Task 1 is the removal boundary; a validator test cannot reference a removed constant.

- [ ] **Step 2: Remove TokenSpeed from `supportsHFRepo`**

Replace the switch case in `internal/service/validator/rules.go` with:

```go
case domain.BackendKindVLLM, domain.BackendKindSGLang, domain.BackendKindUnsloth:
    return true
```

Do not change model-path heuristics or the behavior of any surviving kind.

- [ ] **Step 3: Remove the TokenSpeed domain constant**

- Delete `BackendKindTokenSpeed BackendKind = "tokenspeed"` from `internal/domain/backend.go`.
- Delete `TestBackendKindTokenSpeedValue` from `internal/domain/backend_kind_test.go`.

At this point Tasks 1–3 and Steps 1–2 have removed every production/test reference, so deleting the constant must not create an undefined-symbol compile failure.

- [ ] **Step 4: Run validator and domain tests**

Run:

```bash
go test ./internal/domain ./internal/service/validator/... -count=1
```

Expected: PASS, including VLLM and Unsloth HF-repo acceptance, llama-server rejection, and compilation without `BackendKindTokenSpeed`.

- [ ] **Step 5: Format and commit validator/domain removal**

Run:

```bash
gofmt -w internal/service/validator/rules.go internal/service/validator/validator_test.go internal/domain/backend.go internal/domain/backend_kind_test.go
git add internal/service/validator/rules.go internal/service/validator/validator_test.go internal/domain/backend.go internal/domain/backend_kind_test.go
git commit -m "refactor: remove tokenspeed validation support"
```

---

### Task 5: Remove TokenSpeed documentation and skill guidance

**Files:**
- Modify: `AGENTS.md`
- Modify: `README.md`
- Modify: `docs/backend-schema-update.md`
- Modify: `openwiki/backend-schema.md`
- Modify: `openwiki/data-model.md`
- Modify: `openwiki/http-proxy.md`
- Modify: `openwiki/index.md`
- Modify: `openwiki/operations.md`
- Modify: `openwiki/process-manager.md`
- Modify: `openwiki/quickstart.md`
- Modify locally (gitignored): `.agents/skills/backend-schema-update/SKILL.md`
- Modify locally (gitignored): `.agents/skills/rtx3090-inference-profiles/SKILL.md`

**Interfaces:**
- Consumes: approved specification `docs/superpowers/specs/2026-08-14-remove-tokenspeed-backend-design.md`.
- Produces: user/operator guidance states nine backend kinds and contains no TokenSpeed backend workflow; historical removal spec remains as the decision record.

- [ ] **Step 1: Revert tracked docs to their pre-TokenSpeed facts**

For every tracked documentation file listed above:

- Remove TokenSpeed from backend-kind tables/lists.
- Change counts from ten backends/kinds/generators to nine.
- Remove TokenSpeed-specific `tokenspeed serve`, positional-model, `/readiness`, Python dependency, embedded schema, and process-lifecycle wording.
- Restore generic/default readiness wording to `/health`, while retaining the existing Unsloth log-token exception.
- Preserve unrelated edits and all references to the SGLang enum value `tokenspeed_mla` if present as schema data.

Use `git show origin/main:<path>` as the factual baseline for each changed paragraph, then reapply only post-baseline non-TokenSpeed corrections where needed.

- [ ] **Step 2: Restore live repository skills to nine-kind guidance**

In `.agents/skills/backend-schema-update/SKILL.md`:

- Remove the `tokenspeed` Pattern-C row.
- Remove source-authority instructions for TokenSpeed.
- Remove `tokenspeedhelp` from embedded help-package lists and editing steps.
- Restore any totals from five embedded/four curated/ten kinds to the corresponding nine-kind values.

In `.agents/skills/rtx3090-inference-profiles/SKILL.md`:

- Remove TokenSpeed from the skill description and backend-choice tables.
- Remove TokenSpeed dispatch/profile instructions and Ampere support warnings specific to that backend.
- Preserve the evidence-backed general rule that schema enums do not imply hardware support.

- [ ] **Step 3: Search tracked guidance for residual backend support**

Run a case-insensitive search across:

```text
AGENTS.md
README.md
docs/backend-schema-update.md
openwiki/
.agents/skills/backend-schema-update/SKILL.md
.agents/skills/rtx3090-inference-profiles/SKILL.md
```

Expected: no `tokenspeed`, `TokenSpeed`, `BackendKindTokenSpeed`, `/readiness`, or `tokenspeedhelp` references, except an explicitly verified `tokenspeed_mla` SGLang schema fact if such a documentation occurrence already existed independently.

The approved spec and implementation plan are intentional historical records and are excluded from this residual check.

- [ ] **Step 4: Commit tracked documentation cleanup**

The `.agents/skills/` files are intentionally gitignored local guidance. Verify them in Step 3, but do not force-add them.

Run:

```bash
git add AGENTS.md README.md docs/backend-schema-update.md openwiki
git commit -m "docs: remove tokenspeed backend guidance"
```

---

### Task 6: Delete local runtime assets and run the final gate

**Files:**
- Delete: `backends/tokenspeed/` (gitignored local runtime tree)
- Verify: `/home/diogo/.config/model-loader/backends/catalog.json`
- Verify: `/home/diogo/.config/model-loader/profiles/`
- Verify: `/home/diogo/.local/state/model-loader/`

**Interfaces:**
- Consumes: completed source/docs removal and the approved destructive boundary.
- Produces: no local TokenSpeed checkout/venv/build assets; no active state modified; clean, buildable nine-kind repository.

- [ ] **Step 1: Reconfirm no active TokenSpeed state before deletion**

Read the active catalog and search active profiles/state for `tokenspeed` case-insensitively.

Expected:

- catalog: no backend id/kind/executable containing TokenSpeed;
- profiles: no matches;
- live registry/state: no TokenSpeed instance or process record.

Do not delete or rewrite any active state file.

- [ ] **Step 2: Record the runtime tree size and delete it**

Run:

```bash
du -sh backends/tokenspeed
rm -rf backends/tokenspeed
```

Expected before deletion: approximately 7.0 GiB. Expected after deletion: `backends/tokenspeed` does not exist.

This deletion is explicitly approved by the specification; do not retain the wrapper, venv, source checkout, or build output.

- [ ] **Step 3: Run targeted package verification**

Run:

```bash
go test ./internal/domain ./internal/cli ./internal/service/backendschema/... ./internal/service/processmgr/... ./internal/service/validator/... ./internal/service/backendcatalog/... ./internal/service/configweb/... -count=1
```

Expected: PASS.

- [ ] **Step 4: Run the full repository gate**

Run:

```bash
make build && go test ./...
```

Expected: build succeeds and all Go packages pass.

- [ ] **Step 5: Smoke-test CLI rejection with isolated state**

Create an isolated config/state root so the real catalog is untouched, then run the built binary:

```bash
root="$(mktemp -d)"
mkdir -p "$root/config/model-loader" "$root/state" "$root/data"
XDG_CONFIG_HOME="$root/config" XDG_STATE_HOME="$root/state" XDG_DATA_HOME="$root/data" \
  ./bin/model-loader backend add TokenSpeed --executable /tmp/tokenspeed --kind tokenspeed
```

Expected: non-zero exit and an error containing `unknown backend kind "tokenspeed"`. Confirm the valid-kind list contains the nine remaining kinds and does not contain `tokenspeed`.

- [ ] **Step 6: Audit residual source references**

Search current source, tests, tracked docs, and live skills for:

```text
BackendKindTokenSpeed
NewTokenSpeedGenerator
TokenSpeedGenerator
tokenspeedhelp
buildTokenSpeedArgs
waitHTTPReady
TokenSpeed
```
Expected: no backend-support occurrences. The lowercase `tokenspeed` literal in the two negative boundary tests and CLI smoke assertion is allowed because it proves the removed kind is rejected; it must not appear in a supported-kind registry, switch, schema generator, or operator option. Additional exclusions:

- `docs/superpowers/specs/2026-08-14-remove-tokenspeed-backend-design.md`;
- `docs/superpowers/plans/2026-08-14-remove-tokenspeed-backend.md`;
- Git history;
- state backups predating removal;
- `tokenspeed_mla` in `internal/service/backendschema/curated_sglang.go` and generated SGLang schema backups.

- [ ] **Step 7: Verify branch state and commit any final tracked cleanup**

Run:

```bash
git diff --check
git status --short --branch
git log --oneline -6
```

Expected:

- `git diff --check` succeeds;
- no uncommitted tracked changes remain;
- `backends/tokenspeed/` is absent but does not appear in Git status because `backends/` is ignored;
- history includes the removal commits after the preserved TokenSpeed-support commits and approved spec commit.

If formatting or residual tracked cleanup changed files after the previous commits, commit only those verified changes:

```bash
git add -A
git commit -m "chore: finish tokenspeed backend removal"
```
