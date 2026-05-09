# PROJECT KNOWLEDGE BASE

**Generated:** 2026-04-29
**Commit:** 5699349
**Branch:** main

## OVERVIEW
TUI application for managing llama.cpp profiles and llama-server processes. Built with Go 1.26.2 + Charmbracelet bubbletea.

## STRUCTURE
```
./
├── cmd/model-loader/   # Entry point
├── internal/
│   ├── config/             # Viper TOML loader
│   ├── domain/             # Profile, Instance, Model, FlagSchema
│   ├── service/
│   │   ├── llamahelp/      # --help parser + embedded schema
│   │   ├── modelscanner/   # GGUF model scanning
│   │   ├── monitor/       # GPU metrics via nvidia-smi
│   │   ├── processmgr/    # Process lifecycle + instance recovery
│   │   ├── profilestore/  # Profile persistence (FS)
│   │   └── validator/     # Flag validation rules
│   └── ui/
│       ├── components/    # Help, Modal, Picker, Sparkline, Statusbar
│       ├── pages/         # Profiles, Models, Launcher, Monitor
│       └── theme/
├── testdata/              # Golden test fixtures (help-v7376.txt, .golden.json)
├── docs/superpowers/      # Design specs
└── Makefile
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| llama-server --help parsing | internal/service/llamahelp/ | embedded schema pinned to v7376 |
| GGUF model metadata | internal/service/modelscanner/gguf.go | |
| Process lifecycle | internal/service/processmgr/ | survives TUI exit, recovers from instances.json |
| Profile CRUD | internal/service/profilestore/ | FS-based |
| Flag validation | internal/service/validator/ | |
| GPU monitoring | internal/service/monitor/ | nvidia-smi |
| TUI pages | internal/ui/pages/ | 4 tabs: profiles/models/launcher/monitor |
| Config | internal/config/ | Viper TOML at ~/.config/model-loader/ |

## CONVENTIONS
- **Tests**: Golden tests in `testdata/` — update via `go test ./... -update`
- **Build**: `make build` → `bin/model-loader`
- **Embedded schema**: Pinned to llama.cpp build "v7376 (380b4c9)" — refresh in embedded.go
- **Instance recovery**: Background llama-server processes survive TUI exit; processmgr.Reconcile restores at boot

## ANTI-PATTERNS (THIS PROJECT)
- DO NOT run `llama-server` manually while TUI is managing instances
- DO NOT edit `testdata/help-v7376.golden.json` directly — regenerate via golden test update
- DO NOT assume process cleanup on TUI exit — processes are intentionally orphaned
- DO NOT intercept printable runes (`q`, `1-4`, `?`, letters, digits) globally in `internal/ui/root.go` without first checking `activePageCapturesInput()`. Only `ctrl+c` may bypass this gate. Pages with active huh forms / pickers / inline modals must implement `InputCapture.IsCapturingInput() bool` returning `true` while in those states. Otherwise the global shortcut steals the keystroke from the editable field and the user can't type that character.

## TUI INPUT ROUTING RULES
- **Global shortcut gate**: every shortcut in `RootModel.Update` that consumes a printable rune MUST be wrapped in `if !m.activePageCapturesInput() { ... }`. Exception: `ctrl+c` is unconditional escape.
- **Page capture contract**: a page that opens any editable surface (huh form, text input, inline picker, confirm dialog) MUST implement `InputCapture` and return `true` while that surface is on screen. See `ProfilesPage.IsCapturingInput()` for the pattern (`return p.editing || p.pickerActive || p.confirmDelete`).
- **Forwarding non-key messages to huh**: when a page hosts a `*huh.Form`, its `Update` MUST forward non-`tea.KeyMsg` messages to the form so its internal Cmd→Msg handshake (Init focus, async validation) completes. See `ProfilesPage.Update` tail block.
- **Tests**: any new global shortcut MUST have a paired test using the `capturingPage` test double in `internal/ui/root_test.go` proving the key is forwarded (not consumed) when the active page captures input.

## UNIQUE STYLES
- Charmbracelet TUI with 4-tab model (tea.Program)
- Viper config with mapstructure tags
- Domain-driven service layer under internal/service/
- Embedded fallback schema for llama-server --help (parses at runtime if binary present)
- Pages optionally implement `Reloader` for on-demand refresh on tab focus

## COMMANDS
```bash
make build    # Build binary to bin/model-loader
make install  # Install to $GOPATH/bin
make tests    # Run all tests including golden tests
go test ./... -update  # Update golden test fixtures
```

## NOTES
- Binary managed: `llama-server` (not model-loader)
- Config path: ~/.config/model-loader/config.toml
- State path: ~/.local/state/model-loader/instances.json
- Profiles dir: ~/.local/share/model-loader/profiles/
- Schema version: embedded-v7376
