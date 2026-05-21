# internal/service/backendcatalog

## OVERVIEW
Multi-backend catalog: persists backend definitions (llama-server forks/versions) and their validation schemas as JSON.

## WHERE TO LOOK
| File | Purpose |
|------|---------|
| `store.go` | `Store` and `SchemaStore` interfaces + sentinel errors |
| `fs_store.go` | JSON persistence for catalog + schemas on disk |
| `resolver.go` | `Resolver` interface: profile → `ResolvedBackend` (backend + executable + schema) |
| `default.go` | `DefaultCatalog(executable)` — single-backend catalog for first-run |
| `probe.go` | `Prober` / `NewProber` — runs backend executables to verify availability (`ProbeStatus`, `ProbeEvent`) |

## CONVENTIONS
- Catalog JSON at `~/.config/model-loader/backends/catalog.json`
- Schemas stored in `~/.config/model-loader/backends/schemas/` (relative refs)
- `SchemaRef` uses `schemas/<id>.json` prefix; `SchemaStoreRef` strips it for filesystem paths
- `DefaultBackendID` is the fallback when `profile.Launch.BackendID` is empty
- Resolver validates schema/backend ID and kind match on load

## ANTI-PATTERNS
- Do not write `catalog.json` or schema JSON manually — use `backendschema.Manager` or UI
- Do not store absolute paths in `SchemaRef` — `SchemaStore` rejects them as `ErrInvalidSchemaRef`
- Do not create backends without generating schemas first — `AddBackend` generates schema before saving catalog to avoid broken entries

## NOTES
- `ResolvedBackend.ExecutablePath` is resolved via `resolveExecutable`: `llamabin.Resolve` for CLI backends, `llamabin.ResolveCommandWithPythonFallback` for SGLang/vLLM
- `findBackend` and `upsertBackend` are unexported helpers (linear scan, catalog is small)
