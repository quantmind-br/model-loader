# Web-based Profile Configuration GUI — Design

**Date:** 2026-05-21
**Status:** Approved (design)
**Author:** brainstorming session

## Summary

Replace the in-TUI `huh`-based profile editor with a browser-based GUI launched
on demand by the TUI. The GUI is the single way to create and edit profiles. It
renders a per-backend, schema-driven form and lets the user edit the backend's
validation criteria, fields, highlights, and cross-field rules directly from the
browser. Automatic schema generation (from `--help`) still seeds everything; the
user may override freely.

## Goals

- Profile creation/editing happens in a beautiful, intuitive web GUI, not in the
  TUI form.
- Each backend gets a tailored interface produced by **one** render engine driven
  by the backend schema plus an editable presentation layer (no bespoke per-backend
  UI code).
- The user can manually edit, per backend:
  - per-flag constraints (type, min/max, enum values, default, required);
  - add custom flags / remove unwanted flags;
  - which flags are highlighted and how groups are ordered;
  - cross-field validation rules (`IF field op value THEN effect · severity`).
- Edits apply to the whole backend (all profiles using it) and are preserved
  across `--help` re-parsing.

## Non-goals (this version)

- Backend catalog CRUD (add/remove backend executables) — stays in the TUI.
- Models tab and Server tab — stay in the TUI.
- Per-profile validation overrides — out of scope; validation edits are per-backend.
- Standalone/headless config server subcommand — server is launched only by the TUI.

## Key decisions (from brainstorming)

| Decision | Choice |
|----------|--------|
| Web vs TUI editor | Web **replaces** the `huh` profile editor |
| Server lifecycle | **On-demand**, launched by the TUI; dies on save/cancel |
| Server process | **In-process** goroutine (same binary, shared stores) |
| Editable scope | **Per-backend** schema (affects all its profiles) |
| Render approach | **Schema-driven engine** + editable presentation metadata |
| Persistence of new layers | **Extend the `BackendValidationSchema` envelope** |
| Frontend stack | **Go `html/template` + HTMX + Alpine + embedded CSS** (no Node) |
| GUI scope | Profiles + the used backend's validation/presentation/rules |
| Layout | Sidebar (group nav) + mode tabs (Configure / Customize) |
| Edit model | Inline flag edit + highlight/reorder/add/remove + `IF→THEN` rule builder |

## Data model

`domain.BackendValidationSchema` gains two optional blocks (still one JSON file
per backend):

```go
type BackendValidationSchema struct {
    // ...existing fields (SchemaVersion, Kind, BackendKind, BackendID, Source, Flags)...
    Presentation *Presentation    `json:"presentation,omitempty"`
    Rules        []CrossFieldRule `json:"rules,omitempty"`
}

type Presentation struct {
    Groups []PresentationGroup `json:"groups"`
}

type PresentationGroup struct {
    Name        string   `json:"name"`        // e.g. "Essenciais", "Sampling"
    Highlighted bool     `json:"highlighted"` // first/highlighted group renders at top
    Flags       []string `json:"flags"`       // ordered flag long-names in this group
}

type CrossFieldRule struct {
    ID       string `json:"id"`
    When     Cond   `json:"when"`     // condition
    Then     Effect `json:"then"`     // consequence
    Severity string `json:"severity"` // "warning" | "error"
}

type Cond struct {
    Flag  string `json:"flag"`
    Op    string `json:"op"`    // "eq" | "ne" | "le" | "ge"
    Value string `json:"value"` // compared after type coercion via FlagSpec
}

type Effect struct {
    Kind    string `json:"kind"`              // "limit" | "require" | "message"
    Flag    string `json:"flag,omitempty"`    // target flag for limit/require
    Op      string `json:"op,omitempty"`      // for "limit": le|ge|eq|ne
    Value   string `json:"value,omitempty"`   // for limit/require
    Message string `json:"message,omitempty"` // shown to user; required for "message"
}
```

`domain.FlagSpec` gains `Required bool` (`json:"required,omitempty"`).

Editing any of these via the GUI sets `Source.Editable = true`. `RefreshSchema`
already preserves editable schemas, so manual edits survive `--help` re-parsing;
an explicit "Restore from --help" action in the GUI is the only path that
discards them (calls the existing delete+regenerate flow).

**Schema-doc sync (project rule):** the same change updates `docs/profile-schema.json`
if any persisted profile shape is touched, and the backend validation-schema doc /
golden fixtures for the envelope change. New fields are additive and
backward-compatible (older files simply lack `presentation`/`rules`/`required`).

## Validator

`validator.Validate` gains a step `applyCrossFieldRules(p, schema, rep)` evaluated
after the existing type/extra-args/existence rules:

- For each `CrossFieldRule`, evaluate `When` against the profile args (coercing
  values through the flag's `FlagSpec` type). If the condition holds, apply `Then`:
  - `limit`: assert the target flag satisfies `Op Value`; otherwise emit a
    `FieldIssue` on that flag.
  - `require`: assert the target flag equals `Value` (or is present); otherwise emit.
  - `message`: always emit `Message` when the condition holds.
- Severity comes from the rule (`warning` → `rep.Warnings`, `error` → `rep.Errors`).

Per-flag constraints (min/max/enum/default/required) already flow through
`applyTypeRules` and the existence rules; they now read the **edited** schema
values rather than any hardcoded defaults. `Required` adds a presence check.

The validator stays pure (no I/O), keeping it usable from both the web handlers
and any future caller.

## Server lifecycle (TUI ⇄ web)

New package `internal/service/configweb`:

1. The Profiles tab's create/edit action returns a `tea.Cmd` that starts an
   `http.Server` on `127.0.0.1:0` (ephemeral port) in a goroutine, bound to a
   per-session `context.Context`. Stores (`profilestore`, `backendcatalog`
   stores, `backendschema.Manager`, `validator`) are passed in-process.
2. The TUI opens the browser (`xdg-open`/platform equivalent) at the session URL
   and shows a modal: *"editando no navegador… (esc cancela)"*. The Profiles page
   reports `IsCapturingInput() = true` while the modal is up (per the TUI input
   routing rules).
3. Endpoints:
   - `GET /` — render the editor (Configure mode) for the draft profile.
   - `POST /validate` — run validator + cross-field rules; return HTMX partials
     (inline issues + status bar). Debounced client-side.
   - `POST /save` — persist the profile via `profilestore`; persist any backend
     schema/presentation/rule edits via `schemaStore`; then signal completion.
   - `POST /cancel` — discard and signal completion.
   - `GET /assets/*` — embedded static assets.
4. Save/cancel cancels the session context → server shuts down → a result channel
   delivers `{saved bool, profileID string, err error}` back to the TUI → the TUI
   reloads the profile list and closes the modal.
5. Single session at a time. No concurrent edits, so no locking beyond the normal
   store writes. Bind/`xdg-open` failures surface as a TUI error message without
   hanging (timeout on "browser opened" is not required; the modal's esc cancels).

## Render engine & frontend

- `internal/service/configweb` — server, session, handlers, validation glue.
- `internal/ui/webedit` (or `configweb/assets`) — `embed.FS` with templates,
  vendored HTMX + Alpine, and CSS.
- The engine reads `schema.Flags` + `schema.Presentation` and renders groups in
  order, the highlighted group first ("Essenciais"), choosing a widget per
  `FlagType`: int → number input (with min/max), float → number, enum → select,
  bool → toggle, string → text. Unknown/extra args → a raw "Extra args" section.
- **Configure mode**: fill in profile values; live validation via `/validate`.
- **Customize mode**: per-flag inline editor (type/min/max/default/required),
  highlight toggle, drag-reorder (Alpine), add/remove flag, and the cross-field
  rule builder (`IF flag op value THEN effect · severity`). Operators: `=, ≠, ≤, ≥`.
  Effects: limit another flag, require a value, or message-only. Extensible later.

## Migration & retirement

- One-time migration in `internal/service/migration`: for each backend whose
  schema has no `Presentation`, synthesize one from the current
  `profile_editor.essentialFields` (essentials → highlighted first group) plus the
  remaining flags grouped by `FlagSpec.Group`. Idempotent; skips schemas that
  already have a `Presentation`.
- `internal/ui/pages/profile_editor` (huh forms, draft state machine, essentials
  UI) is removed once the web editor is wired. Reused assets: the essentials
  default values (as the migration seed), the Draft field set (mapped to the web
  form payload), and the validator integration.

## Error handling

- Schema/profile persistence errors return to the browser as inline error
  banners; the session stays open so the user can retry. The TUI is only signaled
  on a clean save or explicit cancel.
- Invalid rule definitions (e.g., reference to a nonexistent flag) are caught at
  rule-save time in Customize mode and shown inline; they are never silently
  persisted.
- Port bind / browser-open failure → TUI error toast, modal not shown, editing
  aborts cleanly.

## Testing

- **Unit**: extended-envelope (de)serialization round-trip; `Required` validation;
  each `CrossFieldRule` effect (`limit`/`require`/`message`) for warning and error;
  essentials→`Presentation` migration; handler tests for `/validate` and `/save`
  with `httptest`.
- **Golden**: a schema fixture carrying `presentation` + `rules`; rendered HTML per
  backend kind (update via `go test ./... -update`).
- **TUI**: open→save→reload cycle drives a list reload; `IsCapturingInput()`
  returns `true` while the editing modal is up (paired test using the
  `capturingPage` double in `internal/ui/root_test.go`).

## Open risks

- Cross-field rules are the largest new surface. The model above intentionally
  starts with a small operator/effect set; expanding it later is additive.
- Vendoring HTMX/Alpine into `embed.FS` keeps the no-Node guarantee but pins their
  versions in-repo; document the versions where they live.

## Affected areas

- `internal/domain/`: `flag_schema.go` (`Required`), `backend_schema.go`
  (`Presentation`, `Rules`, `Cond`, `Effect`, `CrossFieldRule`).
- `internal/service/validator/`: new `applyCrossFieldRules`; read edited constraints.
- `internal/service/backendschema/` + `backendcatalog/`: persist/serve new blocks.
- `internal/service/configweb/` (new): server, session, handlers, render engine.
- `internal/ui/webedit/` (new): embedded assets/templates.
- `internal/ui/pages/profiles_*.go`, `root.go`: launch flow, modal, input gate.
- `internal/ui/pages/profile_editor/`: removed.
- `internal/service/migration/`: essentials→Presentation migration.
- `docs/profile-schema.json` + backend schema doc/goldens: kept in sync.
