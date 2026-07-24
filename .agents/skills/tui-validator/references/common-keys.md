# Common TUI keys to probe

Load this file when you reach **Phase 2 — Inventory** or **Phase 3 — Probe**.
It tells you which keys to try when no documented inventory exists, in what
order, and which keys to handle with care.

## Help-screen probe order

Try these in this exact order. Capture the screen between each — many TUIs
toggle help (so the same key both opens and closes it), and many ignore an
unknown help key silently.

| Order | Key       | Reason                                                                  |
| ----- | --------- | ----------------------------------------------------------------------- |
| 1     | `?`       | The most common help binding in modern Go/Rust TUIs (Bubble Tea, ratatui) |
| 2     | `h`       | Vim-style apps                                                          |
| 3     | `F1`      | Classic "help" function key                                             |
| 4     | `C-h`     | Emacs/readline-style                                                    |
| 5     | `Escape`  | Get back to a known state if the previous keys opened something         |

If the screen changes after any of those, you found the help screen. Capture
both the open and closed states so you can compare. If none of them produced
any change, fall back to the default probe set below.

## Default probe set (when no inventory exists)

These cover ~90% of TUI use cases. Send each one, capture before/after, and
classify per Phase 3 rules.

### Navigation
`Up` `Down` `Left` `Right` `PgUp` `PgDn` `Home` `End` `Tab` `BTab`
`j` `k` `h` `l`            (vim navigation, harmless if not bound)
`g` `G`                    (top / bottom in vim-style)

### Focus / cycling
`Tab` `BTab` `1` `2` `3` `4` `5`   (tab switching is often number-bound)

### Toggles / actions
`Enter` `Space`            (almost always "activate" / "toggle")

### Search / filter
`/` `:`                    (vim-style; many TUIs open a search prompt)

### Common modal entrants
`n` `e` `a`                (new / edit / add — but see danger list first)
`r`                        (refresh / reload — usually safe)
`s`                        (save — sometimes safe, sometimes destructive)

### Exit (always last; tear down via tui-cleanup.sh after)
`q` `Escape` `C-c`

## Danger list — require explicit user opt-in

These keys frequently map to destructive actions (delete file, kill process,
discard buffer). Do **not** send them in an unattended audit unless the user
passed `--allow-destructive` (or equivalent verbal go-ahead).

`d` `D` `x` `X` `Delete` `Backspace` (in non-input mode) `C-k` `C-w` `C-u`
`!` (shell escape in some pagers) `:q!` (force-quit prompts)

When skipping a danger key, write a finding with severity `info` (use the
canonical `{"findings": [...]}` wrapper — see `pitfalls.md`):

```json
{
  "findings": [
    {
      "severity": "info",
      "title": "Skipped destructive key: d",
      "phase": "probe",
      "description": "Key was on the danger list. Re-run with --allow-destructive to test."
    }
  ]
}
```

## Modal context detection

The same key can mean very different things depending on the active modal /
tab. To detect modal changes:

1. Snapshot the screen as a hash (md5 of the plain-text capture).
2. After any action, snapshot again.
3. If the bottom 1–2 rows changed (status bar) **and** the diff is large, the
   modal/tab likely changed — re-inventory from the new state.
4. Tag every probed binding with the modal hash that was active when probed.
   This lets you distinguish "binding `q` quits the app" from "binding `q`
   submits the search prompt".

## Bubble Tea / ratatui specifics

- Bubble Tea apps almost always honour `C-c` as a hard kill regardless of
  modal context — use it as your last-resort exit.
- ratatui apps often expose their help via a footer hint (`?` for help) but
  hide it after a few seconds — capture twice, 3 s apart, to see if hints
  fade.
- Textual (Python) apps support `Tab` + `Shift-Tab` for focus cycling and
  almost always have a `quit` action bound — check `C-q` as well as `q`.
