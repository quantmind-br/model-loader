# Contributing to model-loader

Thank you for helping improve model-loader. This guide covers the path from a
fresh checkout to a reviewable change.

## Before you start

- Search existing GitHub Issues before opening a duplicate.
- Open an issue before a broad architectural change or new benchmark mode.
- Keep pull requests focused on one coherent outcome.
- Never include model weights, local configuration, credentials, logs, backend
  source checkouts, or generated binaries.

By participating, you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Development setup

Required:

- Linux
- Go 1.26.2 or newer
- Git

Clone and validate the checkout:

```bash
git clone https://github.com/quantmind-br/model-loader.git
cd model-loader
go build ./...
go test ./...
```

An inference backend, GPU, and model are not required for most unit tests.
Some process tests start local fake binaries and allocate loopback ports.

## Find your way around

| Path | Responsibility |
|---|---|
| `cmd/model-loader/` | Binary entry point and TUI wiring |
| `internal/cli/` | Cobra command tree |
| `internal/domain/` | Dependency-free domain types |
| `internal/service/` | Process, proxy, storage, backend, and benchmark services |
| `internal/ui/` | Bubble Tea pages and components |
| `docs/` | User guides, schemas, and design records |
| `openwiki/` | Maintained architecture and contributor reference |
| `testdata/` | Golden inputs and fake executables |

Read the [OpenWiki quickstart](openwiki/quickstart.md) and then the page relevant
to your change. The root [`AGENTS.md`](AGENTS.md) records detailed repository
invariants used by maintainers and coding tools.

## Make a change

1. Create a branch from the current default branch.
2. Add the smallest change that completely solves the issue.
3. Add a regression test for a subtle bug or a new behavioral boundary.
4. Update documentation when commands, configuration, persisted data, or user
   behavior changes.
5. If `internal/domain/profile.go` changes, update
   `docs/profile-schema.json` in the same pull request.
6. Link bug fixes to their GitHub Issue when one exists and keep regression
   tests focused on the behavior that failed.

The project uses standard-library `testing` assertions and hand-written test
doubles. Do not introduce a mocking or assertion framework for a local change.

## Validate

The required quality gate is:

```bash
go build ./...
go test ./...
go vet ./...
```

For a narrow edit, run the affected package first, then run the full gate. If
you change a user-facing surface, exercise that surface manually as well: run
the CLI command, use the TUI flow, or send a request to the local HTTP service.

Backend-help changes may require schema regeneration. Follow
[`docs/backend-schema-update.md`](docs/backend-schema-update.md) and include the
regenerated schema artifacts in the same pull request.

## Pull requests

A reviewable pull request:

- explains the user-visible problem and the chosen solution;
- links its GitHub Issue when one exists;
- lists the commands and manual scenarios used for verification;
- calls out compatibility, migration, and operational impact;
- contains no unrelated formatting or cleanup; and
- keeps all checks passing.

Maintainers may ask to split a pull request when independent changes make it
hard to review or revert safely.
