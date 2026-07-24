# Keybinding conventions

Used in Phase 5 to design the target keymap and in Phase 6 to flag every change
against the current map. In a refactor, a key change is a cost — list it.

The canonical keymap below is **shared verbatim with `tui-design`** so that a
greenfield design and a redesign of the same class of app land on the same
scheme. Keep the block between the markers byte-identical across the two skills.

> **Host-project convention wins.** If the target project already documents its
> own keybinding convention (e.g. an `AGENTS.md`/`CLAUDE.md` rule such as
> "UPPERCASE = destructive"), that convention takes precedence over the table
> below. Apply these defaults only where the project is silent.

<!-- BEGIN canonical keymap (identical in tui-design and tui-refactor) -->
## Sacred / Reserved Global Keys

| Key | Meaning | Notes |
|-----|---------|-------|
| `?` | Open help overlay | Works in every screen and modal. |
| `q` | Quit current scope | App-level quits with confirm if unsaved. |
| `esc` | Cancel / close modal / clear search | Never destroys data. |
| `ctrl+c` | Hard exit (immediate) | Bypasses confirm. Emergency only. |
| `enter` | Activate primary action of focused widget | List item → open; button → press. |
| `tab` / `shift+tab` | Move focus to next/previous element | |
| `/` | Open search/filter for current view | Incremental. |
| `:` | Open command palette (if app has one) | |
| `ctrl+l` | Redraw screen | Old terminal habit, harmless to honor. |

## Navigation Keys

Provide BOTH vim-style AND arrow keys. Don't force one.

| Action | Vim | Arrow |
|--------|-----|-------|
| Up one | `k` | `↑` |
| Down one | `j` | `↓` |
| Left | `h` | `←` |
| Right | `l` | `→` |
| Top | `gg` | `home` |
| Bottom | `G` | `end` |
| Page up | `ctrl+u` | `pgup` |
| Page down | `ctrl+d` | `pgdn` |
| Next tab | `shift+l` | `ctrl+tab` |
| Prev tab | `shift+h` | `ctrl+shift+tab` |
| Direct tab | `1`-`9` | — |

## Action Verb Vocabulary

Pick from this vocabulary — it makes apps feel consistent across the ecosystem.

| Key | Verb | When |
|-----|------|------|
| `a` | add / new | Create new item. |
| `e` | edit / rename | Open editor on the focused item (in-place rename lives here). |
| `d` | delete (with confirm) | Destructive. Always confirm; never default-focus delete. |
| `D` | duplicate / clone | Non-destructive copy. |
| `r` | refresh / reload | Re-fetch or restart a process. |
| `R` | regenerate (heavier) | Rebuild from source. |
| `x` | kill / cut | Process kill or text cut. |
| `p` | pin / paste | Toggle pin or paste. |
| `P` | pin all / paste-before | |
| `i` | info / inspect | Reveal metadata. |
| `s` | save / sort | Context dependent. |
| `S` | save-as | |
| `u` | undo | One level minimum. |
| `U` | redo | If supported. |
| `y` | yank / copy | |
| `f` | follow / find-forward | |
| `F` | follow-toggle / find-backward | |
| `g` | go / first | Mnemonic prefix. |
| `G` | last | |
| `n` | next match | After search. |
| `N` | previous match | |
| `t` | tag / toggle | |
| `c` | configure / change | |
| `<` / `>` | shift left / right | Indent, demote/promote. |
| `space` | select / toggle multi-select | In lists/tables. |

## Modifier Etiquette

- **No modifier** — frequent, safe actions.
- **Shift** — variant or "bigger" of a base action (`d` delete one, `D` duplicate; `r` refresh, `R` regenerate).
- **Ctrl** — system-level (save, quit, undo) or where terminals already capture (`ctrl+c`, `ctrl+z`, `ctrl+d`).
- **Alt/Meta** — last resort. Many terminals eat alt or remap it.

Avoid: `ctrl+s` if the user might be on a terminal where it freezes flow (XON/XOFF). Provide an alternative like `:w` or a save key in the menu.

## Conflict Audit Rules

1. A key MUST have the same meaning across all screens. Need a different meaning? Pick a different key.
2. No key in the **Sacred** table is ever reused.
3. Per-component keys (e.g., `space` toggle in a list) must not collide with the active screen's keys.
4. Document every unavoidable contextual override explicitly in `04-keybindings.md`.
<!-- END canonical keymap (identical in tui-design and tui-refactor) -->

## Contextual overrides

A key may mean different things in different **contexts** only when the context
is unambiguous (e.g. inside a text input, letter keys type rather than trigger).
Document every such override in `04-keybindings.md`; never silently overload a
global key.

## Conflict audit (context × key)

Build a table of (context × key). Flag:
- the same key bound to two actions in one context;
- a global key shadowed by a screen with a different meaning (allowed only if
  documented and clearly contextual);
- a reserved key doing something non-standard.

## Refactor change log

In `04-keybindings.md`, add a **Changes vs current** table: `key | was | now |
reason | muscle-memory cost`. Phase 4 decides whether each change ships.
