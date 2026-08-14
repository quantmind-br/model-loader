# Remove TokenSpeed backend support

## Goal

Remove TokenSpeed as a supported model-loader backend because the current upstream runtime requires Hopper-or-newer CUDA instructions and does not compile for this workstation's RTX 3090 GPUs (`sm_86`). Return the application to nine `BackendKind` values without leaving compatibility aliases, dormant generators, stale documentation, or local runtime artifacts.

## Current evidence

- Active catalog: no backend with id or kind `tokenspeed`.
- Active profiles: no TokenSpeed references.
- Active state: no TokenSpeed instances or runtime records.
- Local runtime tree: `backends/tokenspeed/`, approximately 7.0 GiB.
- CUDA toolchain is present (`cuda 13.3.1`, `/opt/cuda/bin/nvcc`).
- An explicit `sm_86` build fails on Hopper-only instructions and APIs, including `griddepcontrol`, programmatic launch dependency modifiers, and cluster cooperative groups.

## Removal boundary

### Domain and schema

- Remove `domain.BackendKindTokenSpeed` and its value test.
- Delete `internal/service/tokenspeedhelp/`.
- Delete `TokenSpeedGenerator` and its tests.
- Remove TokenSpeed from `RegisterDefaults`, registration coverage, `essentialSeed`, and Pattern-C presentation coverage.

### Process and validation

- Remove the TokenSpeed argument-builder dispatch and helper/test.
- Remove TokenSpeed from Python-unbuffered launch classification, legacy recovery tokens, and Python command fallback.
- Remove TokenSpeed-specific `/readiness` dispatch and its test.
- Restore `WaitHealthy` to directly own the fixed `/health` polling implementation; remove the path-selectable helper introduced solely for TokenSpeed.
- Remove TokenSpeed from native Hugging Face repository support and delete the related validator test.

### Operator surfaces

- Remove TokenSpeed from the configweb backend-kind list.
- Preserve the independent fix that added `BackendKindUnsloth` to that list.
- `backend add ... --kind tokenspeed` must return the normal unknown-kind error after removal.

### Documentation and persistent guidance

- Return README, AGENTS, backend-schema documentation, and OpenWiki counts/lists from ten kinds to nine.
- Remove TokenSpeed-specific readiness, process, dependency, and generator references.
- Remove TokenSpeed routing from the live `backend-schema-update` and `rtx3090-inference-profiles` skills; restore their nine-kind wording.
- Preserve `tokenspeed_mla` where it is an upstream SGLang `attention-backend` enum value. That string describes an SGLang kernel backend, not a model-loader `BackendKind`.

### Local runtime state

- Delete `backends/tokenspeed/` in full, including checkout, venv, generated objects, wrapper, and build script.
- This deletion is intentional and expected to reclaim approximately 7.0 GiB.
- Do not alter the active backend catalog, profiles, or runtime registry because none contains TokenSpeed state.

## Verification

1. Targeted Go packages pass: domain, backendschema, processmgr, validator, backendcatalog, and configweb.
2. `make build && go test ./...` passes.
3. The CLI rejects `backend add TokenSpeed --executable /tmp/tokenspeed --kind tokenspeed` as an unknown kind in an isolated config tree.
4. Registration coverage reports exactly the nine remaining built-in generator kinds.
5. `backends/tokenspeed/` no longer exists.
6. Searches find no backend-support symbols or documentation for TokenSpeed. Allowed residual: `tokenspeed_mla` in SGLang schema facts and backups generated before this removal.

## Delivery

Implement as a new commit on `main`, preserving the existing nine TokenSpeed-support commits in history. Do not reset or rewrite `main`.
