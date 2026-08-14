# Updating backend schemas

Backend schemas describe the flags accepted by each inference runtime. Update
them whenever a supported backend changes its command-line interface.

## Choose the schema source

| Backend kind | Source |
|---|---|
| `llama-server`, `beellama-cpp`, `buun-llama-cpp` | Live `--help` parsing plus the matching curated overlay in `internal/service/backendschema/` |
| `vllm`, `sglang`, `ik-llama-cpp` | Curated Go schema in `internal/service/backendschema/` |
| `dflash`, `unsloth`, `tabby` | Embedded rows in the matching `internal/service/*help/` package |

For live-help backends, compare the current binary and source declarations with
the parsed schema. For curated backends, the runtime's argument declarations are
the source of truth for flag names, types, defaults, and accepted values.

## Apply the update

1. Record the backend version or commit being synchronized.
2. Compare additions, removals, renames, types, defaults, constraints, and
   deprecations against the current model-loader schema.
3. Update only the source appropriate to the backend kind from the table above.
4. Preserve English labels, help text, presentation groups, and cross-field
   rules unless the backend contradicts them.
5. Do not add flags to the highlighted Essentials group without an explicit
   product decision.

For `llama-server`, regenerate the golden fixture when the parsed surface
changes:

```bash
go test ./internal/service/llamahelp -update
cp testdata/help-v10152.golden.json \
  internal/service/backendschema/testdata/help-v10152.golden.json
```

The two golden copies must remain byte-identical. If the embedded backend build
changes, update their filenames and embedded version identifiers together.

## Validate

```bash
go test ./internal/service/backendschema/... ./internal/service/<kind>help/...
go build ./...
go test ./...
go vet ./...
```

`go run ./cmd/regenerate-schemas` refreshes every configured catalog backend and
is destructive to pristine generated schema facts. Back up operator schemas
before running it. Customized presentation and rules survive only when the
schema carries `source.customized: true`.
