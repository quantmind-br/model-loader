# Target design — <tui-name>

> Phase 5. The optimized design, sized to the ambition the user chose in Phase 4
> (conservative cleanup vs bold redesign). Use this as `03-target-design/00-overview.md`
> and split per-screen files into `01-screens/`.

## Ambition & constraints (from Phase 4)

- **Ambition:** <cleanup / bold redesign>
- **Scope:** <whole app / these screens>
- **Keybindings:** <preserve / may change>
- **Aesthetic:** <dense/spacious, border style, color philosophy>
- **Hard constraints:** <framework, min terminal size, color tiers, i18n>

## Target information architecture

- Navigation pattern: <tabs / sidebar / single-screen / palette> (5±2 rule)
- Screen tree:

```
<ascii tree of target screens/modals>
```

## Design principles applied

<which of the 14 principles drive the biggest changes here, and how>

## Accessibility summary

> Required when the ambition is a bold redesign; `n/a — <reason>` for a
> conservative cleanup. Full detail lives in `09-accessibility.md`.

- **No color-only signals:** <how state is shown besides color (glyph/label)>
- **Color-tier fallback:** <truecolor → 16-color → monochrome behavior>
- **Keyboard-only path:** <every action reachable without a mouse>

## Open design questions

- <decisions still owed by the user>

## Assumptions made without blocking

- <safe default used because asking would not materially change the plan>

---

## Per-screen wireframe (one file each under 01-screens/)

### <screen name>
- **Purpose:** <one line>
- **Before → after:** what changes and why

```
┌─ <title> ───────────────────────────────────────────────────────────┐
│ ▶ <focused row>                                                       │
│   <sample content>                                                    │
│   …                                                                   │
├───────────────────────────────────────────────────────────────────── ┤
│ q quit  ? help  / search  <contextual keys>                           │
└────────────────────────────────────────────────────────────────────── ┘
```

- **Layout:** <regions, size policy, minimum size>
- **States:** empty / loading / error (recoverable+fatal) / populated wireframes
- **Keys (contextual):** <table>
- **Focus order:** <…>
- **Out of scope:** <per Phase 4, or n/a>
