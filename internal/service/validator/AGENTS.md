# internal/service/validator

## OVERVIEW
Profile validation against a `FlagSchema`. Checks type compatibility, extra-args syntax, and model file existence.

## WHERE TO LOOK
| File | Purpose |
|------|---------|
| `validator.go` | `Validator` interface, `Report`, `Severity`, `FieldIssue` |
| `rules.go` | Three rule sets: `applyTypeRules`, `applyExtraArgsRules`, `applyExistenceRules` |

## CONVENTIONS
- `canonicalFlag()` maps short keys (`ngl` → `n-gpu-layers`) before schema lookup
- Type rules: int accepts whole-number `float64`/`float32`; float accepts int; bool only `bool`; string only `string`; enum matches `EnumValues`
- Extra-args rules: `--flag` prefix required; `=value` or space-separated; bool flags must not have values; non-bool flags must have values
- Existence rule: `os.Stat` on `p.Model` — error = `SeverityError`
- `Report` separates `Errors` (blocking) and `Warnings` (non-blocking)

## ANTI-PATTERNS
- Do not add new short-to-long mappings without updating `shortToLong` table in `rules.go`
- Do not change type-checking float logic carelessly — `math.Trunc` + `math.IsInf`/`math.IsNaN` guards are required for JSON number edge cases
- Do not expect `FlagSchema.Lookup` to find all aliases — it searches map keys, long, short, and aliases

## NOTES
- Unknown flags (not in schema) are `SeverityError` — backend catalog must have a matching schema
- `applyExtraArgsRules` handles `--flag=value` and `--flag value` syntax; iterates with index to consume value tokens
- `checkExtraArgType` only validates int/float/enum; string/bool are implicitly valid if syntax is correct
