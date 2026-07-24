# TUI Interaction Patterns

## Confirmation Pattern

For destructive or irreversible actions:

```
┌─ Confirm Delete ─────────────────────┐
│  Delete profile "qwen-coder-30b"?    │
│  This cannot be undone.              │
│                                      │
│             [ No ]  [ Yes, delete ]  │
└──────────────────────────────────────┘
```

- **Default focus on the safe option** (No / Cancel).
- Destructive button names the action ("Yes, delete"), not "OK".
- Body explains *what* and *why irreversible*.
- `enter` triggers focused button. `esc` cancels. `tab` cycles.

Less-destructive ops can use inline status-bar prompts: `Discard unsaved changes? [y/N]`.

## Validation Pattern

Inline + summary. Errors next to the offending field; summary at top/bottom for screens with many fields.

```
Name        ▶ qwen coder 30b
            ⚠ Slug cannot contain spaces. Suggestion: qwen-coder-30b
Port          80
            ⚠ Port 80 is reserved. Choose ≥ 1024.

────────────────────────────────────────────────────────
⚠ 2 errors must be fixed before saving.
```

- Errors block save; warnings only signal.
- Validate on blur, not on every keystroke (avoids flicker and false positives).
- Provide a suggestion when you can.

## Async Operation Pattern

Long ops show progress + cancel.

```
⠋ Probing backends... press esc to cancel
```

- Never freeze the UI.
- Show ETA when measurable.
- Always allow cancel.
- On completion, show a toast and update affected views.

## Undo Pattern

For edits to user-owned data:

- One level of undo is the **minimum**. Multi-level is better.
- Show a diff before applying undo so the user knows what's reverting.
- Undo confirms; never silently overwrite.

```
┌─ Undo last change to "qwen-coder-30b"? ──────────────┐
│ Diff:                                                │
│   - port: 8081                                       │
│   + port: 8080                                       │
│   - ctx_size: 32768                                  │
│   + ctx_size: 16384                                  │
│                                                      │
│                    [ No ]  [ Yes, revert ]           │
└──────────────────────────────────────────────────────┘
```

## Search-as-You-Type

For lists/tables:

- Open with `/`; type to filter.
- Debounce ~150 ms when the dataset is large.
- Show match count: `/ qwen   (3 matches)`.
- `enter` commits the filter and re-focuses the list; `esc` cancels and clears.
- `n` / `N` jump between matches when results scroll out of view.

## Multi-Select Pattern

For batch operations:

- `space` toggles selection of the focused item.
- Selected items show `◆` marker (NOT just background color).
- Status bar updates: `3 selected · d delete  y copy`.
- Bulk actions confirm with the count: "Delete 3 profiles?".

## Conflict Resolution Pattern (Import / Merge)

```
┌─ Import conflict ────────────────────────────────────┐
│  4 profiles in bundle, 2 already exist:              │
│    · qwen-coder-30b                                  │
│    · llama-70b-q4                                    │
│                                                      │
│  How to resolve?                                     │
│    [ Skip conflicts ]                                │
│    [ Overwrite ]                                     │
│    [ Rename (add suffix) ]                           │
│    [ Cancel import ]                                 │
└──────────────────────────────────────────────────────┘
```

- Default focus on the **least destructive** option (Skip or Rename).
- Show conflicting items by name.
- Report results after: "Added 2, Skipped 2".

## Boot Blocker Pattern

When a critical resource is missing, block the UI with a modal that explains what's missing and how to fix it.

```
┌─ Cannot start ────────────────────────────────────────────────┐
│                                                               │
│  ⚠ No backends are available.                                 │
│                                                               │
│  Add a backend to continue:                                   │
│    1. Press [b] to open the Backends tab                      │
│    2. Press [a] to add a backend                              │
│    3. Provide the executable path                             │
│                                                               │
│  Or run:                                                      │
│    $ app backend add llama-server /usr/local/bin/llama-server │
│                                                               │
│                              [ Open Backends ]  [ Quit ]      │
└───────────────────────────────────────────────────────────────┘
```

The modal is the **only** thing usable until resolved.

## Empty State Pattern

Empty lists/tables show a friendly nudge — never a blank rectangle.

```
┌─── Profiles ──────────────────────────────────────────┐
│                                                       │
│                                                       │
│                   No profiles yet.                    │
│              Press [a] to create one.                 │
│                                                       │
│                                                       │
└───────────────────────────────────────────────────────┘
```

Center horizontally and vertically. Always include the call-to-action key.

## Error State Pattern

Recoverable errors are inline with retry. Fatal errors block with explanation.

```
│ ✗ Failed to launch qwen-coder-30b: port 8080 already in use.            │
│   [ Choose different port ]  [ View logs ]  [ Dismiss ]                 │
```

Always include: what failed, why, what the user can do next.

## Optimistic UI Pattern

For fast, reversible operations (toggle a flag, pin/unpin):

- Apply visually immediately.
- Send the operation in the background.
- On failure, revert visually and show an error toast.

For slow, irreversible operations (kill process, delete file):

- Show a spinner before applying.
- Wait for confirmation before updating the view.

## Stale Data Pattern

When the view shows cached data that may be out of date:

```
│ ⓘ Last refreshed 12s ago · press r to refresh                            │
```

If staleness might affect decisions, show it prominently. If not, hide it.

## Progressive Disclosure

Don't dump every option on the first screen.

- **Essentials** sub-tab → 4–8 most-common fields.
- **Advanced** sub-tab → full table.
- **Environment / Sizing / etc.** → specialty sub-tabs.

Same idea for tables: hide columns by default; let users reveal more with a "show all columns" toggle.

## Drag-Free Reordering

Terminals don't drag well. Use keyboard:

- `K` move item up; `J` move item down (uppercase to distinguish from normal navigation).
- Or: enter "reorder mode" (a key like `m`), then `j`/`k` move the marked item.

## Multi-Step Wizards

Avoid when possible. If unavoidable:

- Show a stepper at top: `① Choose model → ② Configure → ③ Confirm`.
- Always allow `←` or `b` to go back.
- Preserve state on back-navigation.
- Allow `esc` at any step to abort with confirm.
