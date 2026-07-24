# Current design — <tui-name>

> Phase 2 reverse-design. Faithful, non-judgmental snapshot of how the TUI looks
> and behaves TODAY. Save opinions for the gap analysis.

## Overview

- **TUI:** <name> (<version if known>)
- **Framework:** <Bubble Tea / Textual / Ratatui / …>
- **Launch:** <command>
- **UI source files:** <list from meta.json>
- **Live capture:** <done at 80×24 + wide / skipped — reason>
- **Safety mode:** <demo data / temp project / read-only fixture / live data — why>
- **Detection:** <manual / scripts/tui-refactor-detect.sh reviewed>

## Information architecture (as built)

- Top-level navigation pattern: <tabs / sidebar / single-screen / palette>
- Screen tree:

```
<ascii tree of screens/modals as they exist today>
```

## Screens (as built)

### <screen name>
- **Purpose:** <one line>
- **Source:** `<file:symbol>`
- **Layout:** <regions and how they size on resize>
- **States rendered:** <happy only? empty/loading/error?>
- **Before screenshot:** `before/<slug>-80x24.png` (if captured)
- **Notes:** <hard-coded sizes, etc.>

<repeat per screen>

## Keybindings (as built)

> Also written structured to `keybindings.json` as {key, action, context}.

| Context | Key | Action | Source |
| ------- | --- | ------ | ------ |
|         |     |        |        |

Structured file: `keybindings.json`.

## Visual system (as built)

- **Colors:** <hard-coded literals? central theme? semantic roles?>
- **Capability handling:** <truecolor only? fallbacks?>
- **Borders / glyphs / density:** <observed>

## State handling (as built)

- Empty: <rendered? how?>
- Loading: <spinner / blocking / none>
- Error: <how surfaced>
- Async: <blocking the UI? command/worker-based?>
