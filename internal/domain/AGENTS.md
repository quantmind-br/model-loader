# DOMAIN KNOWLEDGE BASE

**Parent:** [../../AGENTS.md](../../AGENTS.md)

## OVERVIEW
Zero-dependency shared types. Profile, Backend, Instance, Model, and FlagSchema definitions used by all service and UI layers.

## WHERE TO LOOK
| File | Purpose |
|------|---------|
| `profile.go` | `Profile`, `LaunchConfig`, `ProfileMeta`, `Slugify` |
| `backend.go` | `Backend`, `BackendCatalog`, `BackendKind`, `BackendMeta` |
| `flag_schema.go` | `FlagSchema`, `FlagSpec`, `FlagType`, `Lookup` |
| `instance.go` | `Instance` (running process state) |
| `model.go` | `ModelFile` (GGUF metadata) |
| `backend_schema.go` | `BackendValidationSchema`, `SchemaSource`, `FlagSchemaToBackend` |

## STRUCTURE
```
internal/domain/
├── profile.go         # Profile, LaunchConfig, ProfileMeta, Slugify
├── backend.go         # Backend, BackendCatalog, BackendKind, BackendMeta
├── flag_schema.go     # FlagSchema, FlagSpec, FlagType, Lookup
├── instance.go        # Instance (running process state)
├── model.go           # ModelFile (GGUF metadata)
├── backend_schema.go  # BackendValidationSchema, SchemaSource, FlagSchemaToBackend
└── *_test.go          # JSON roundtrip tests
```

## CONVENTIONS
- **Zero deps**: No imports outside stdlib — domain is pure data
- `SchemaVersion = 1` for Profile JSON
- `Slugify` produces ASCII kebab-case safe for filenames
- `FlagSchema.Lookup` resolves map key, long name, short name, and aliases
- `LaunchConfig.ResolvedExecutable` is set at launch time (`json:"-"` — not persisted)
- `BackendKind` is a string enum: `llama-server`, `vllm`, `tabbyapi`, `sglang`
- **Mapstructure tags**: All config-bound fields have `mapstructure:"field_name"` for Viper
- **JSON tags**: All persistable fields have `json:"field_name,omitempty"`

## ANTI-PATTERNS
- **NEVER** add external imports to this package — it must remain zero-dependency
- **NEVER** change `SchemaVersion` without a migration plan
- **NEVER** modify `Slugify` regex without checking filename safety implications
- **NEVER** mutate domain structs in place after persistence — copy then modify
- **NEVER** omit `omitempty` on JSON tags — empty values must not appear in stored JSON

## NOTES
- `FlagType` enum: `Bool=0, Int=1, Float=2, String=3, Enum=4`
- `BackendValidationSchema` wraps `FlagSchema` with backend ID/kind and source metadata
- `FlagSchemaToBackend` converts a parsed `FlagSchema` to `BackendValidationSchema` (used by `backendschema` generator)
