# Screen: <name>

**Path in IA**: <e.g., Profiles tab > Profile Editor > Essentials sub-tab>
**Purpose**: <one sentence — what user goal does this screen serve?>
**Entry points**: <how the user reaches this screen>
**Exit points**: <where the user can go from here>
**Minimum terminal size**: <80×24 unless higher is required, in which case justify>

## Wireframe (default state, 80×24 minimum)

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ ... header / tabs ...                                                        │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
├──────────────────────────────────────────────────────────────────────────────┤
│ status bar · context info │ key hints                                        │
└──────────────────────────────────────────────────────────────────────────────┘
```

## Layout

- Panel A — <name>: <size policy on resize>, <content summary>
- Panel B — <name>: ...

## Components used

- <component-name> (see `03-components.md`)
- ...

## States

### Empty
<wireframe + copy + call-to-action key>

### Loading
<wireframe + spinner placement + cancel key>

### Error (recoverable)
<wireframe + retry path>

### Error (fatal)
<wireframe + escape path>

### Populated (default)
(see main wireframe above)

### <spec-specific state>
<wireframe>

## Contextual keybindings

| Key | Action | Confirm? |
|-----|--------|----------|
| `j` / `↓` | next item | — |
| `k` / `↑` | previous item | — |
| `enter` | open detail | — |
| ... | ... | ... |

Global keys (`?`, `q`, `esc`, `/`, `:`, `tab`) live in `04-keybindings.md`; only repeat here if this screen overrides one.

## Focus order

1. <focused-by-default widget>
2. <next on tab>
3. ...

Focus cycling rule: <tab cycles within panel A, then panel B / or wraps within current panel only / etc.>

## Edge cases

- **Terminal too small** (<80×24): <degrade plan or block with message>
- **Long content** (truncation, ellipsis): <rule>
- **Slow data** (loading > 2s): <fallback>
- **Concurrent updates**: <reconciliation strategy>
- **Spec-specific**: <any specific to this screen>

## Accessibility notes

- Focus marker: <`▶` prefix / `[brackets]` / double border>
- Color-coded states paired with: <glyphs and position>
- Reduced-motion fallback for any animation: <plan>

## Open design questions

- <question 1>
- <question 2>

(Remove this section if there are none.)
