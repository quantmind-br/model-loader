# internal/service/llamahelp

## OVERVIEW
Parses llama-server --help into domain.FlagSchema and supplies a curated embedded fallback when the binary is not in PATH.

## WHERE TO LOOK

| File | Purpose |
|------|---------|
| `llamahelp.go` | Parser interface (Parse, DetectVersion) |
| `parser.go` | Text parsing: section regex, flag regex, type inference, default extraction |
| `exec_parser.go` | Invokes real llama-server binary for live --help / --version |
| `embedded.go` | Compile-time fallback schema with essential flags only |

## CONVENTIONS

- **Type inference** is best-effort from placeholder tokens: `N`, `INDEX`, `PORT` → int; `F`, `RATE` → float; `[a|b|c]` / `{a,b,c}` → enum; missing placeholder → bool.
- **Enum extraction from continuations**: lines following a flag that start with `allowed values:` are parsed and injected into `spec.EnumValues`.
- **Hardcoded overrides** in `hardcodedFlagOverrides()` patch flags whose --help text does not expose enum values (e.g., `--cache-type-k`).
- **Embedded schema** covers only essential flags; it is intentionally smaller than a full parsed schema.
- Enum values for KV-cache quant types are hardcoded in `cacheTypeEnum` because --help renders them as opaque `TYPE`.

## ANTI-PATTERNS

- DO NOT add flags to `embedded.go` by hand; refresh via `go test ./internal/service/llamahelp -update` after capturing new help text.
- DO NOT assume all llama.cpp builds format --help identically; the regexes target the canonical layout.

## NOTES

- Live parser prefers the positive form when a flag has both `--flag` and `--no-flag` aliases.
- Embedded flags carry Group `"embedded"`; live-parsed flags use the section header name (e.g., `"common"`).
- stderr from `llama-server --help` is merged into the parser input; some builds emit help to stderr.
