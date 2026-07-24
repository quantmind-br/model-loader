# UI/UX Overview · <app name>

## Executive summary

<2–3 paragraphs: what the app is, the chosen IA pattern, and what the design optimizes for.>

## Design principles

(Pick the most relevant 5–8 from `references/inspiration.md` and `SKILL.md` and tailor to this app.)

1. <principle>
2. <principle>
3. <principle>
4. <principle>
5. <principle>

## Information architecture

Top-level navigation: <tabs | sidebar | single-screen | command-palette>

### Screen tree

```
Root
├── <Tab 1>
│   ├── <Sub-screen A>
│   └── <Sub-screen B>
├── <Tab 2>
│   └── ...
├── <Modal: Confirm Delete>
├── <Modal: ...>
└── ...
```

## Design decisions

| Decision | Choice | Why |
|----------|--------|-----|
| Top nav | <Top tabs / Sidebar / ...> | <reason> |
| Master/detail width | <30/70 / 50/50 / ...> | <reason> |
| Vim keys | <Yes alongside arrows / Arrows only> | <reason> |
| Confirmation default | <No / Cancel> | <reason> |
| Color tier | <Dark default + light + HC + mono> | <reason> |
| Async UX | <Spinner + cancel; toast on completion> | <reason> |
| ... | ... | ... |

## Open design questions

Items the spec under-specified or where multiple equally-valid designs exist. Resolve with the team before implementation.

- <question>
- <question>

## Document map

- `01-screens/` — per-screen wireframes and behavior
- `02-modals/` — modal designs
- `03-components.md` — component library
- `04-keybindings.md` — full keymap with conflict audit
- `05-flows.md` — critical user flows
- `06-style-guide.md` — colors, borders, glyphs, density
- `07-states.md` — empty/loading/error/disabled patterns
- `08-navigation-map.md` — screen graph and modal stack rules
- `09-accessibility.md` — keyboard, color-blind, screen-reader plan
