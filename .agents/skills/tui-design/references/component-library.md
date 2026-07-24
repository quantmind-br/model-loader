# TUI Component Library

Canonical widgets. For each: purpose, ASCII rendering, properties, keys, accessibility.

---

## List

Vertical, single-column selection.

```
┌─── Profiles ──────────────────────────────┐
│ ▶ qwen-coder-30b              ● running   │
│   llama-70b-q4                · idle      │
│   mistral-7b                  · idle      │
│   deepseek-coder              ● running   │
│   ...                                     │
└───────────────────────────────────────────┘
```

Props: `selectable`, `multi-select` (space toggles, marker `◆`), `filterable`, `virtualized`.
Keys: `j`/`k` or arrows; `gg`/`G` jump; `enter` activate; `space` toggle multi-select.
A11y: focus marker `▶` is independent of color; selection state shown by `◆` not just background.

---

## Table

Multi-column, sortable, filterable.

```
NAME            ▼ SIZE     QUANT    PARAMS   PATH
qwen-30b-coder    22.4 GB  Q5_K_M   30B      ~/models/code/...
llama-70b-q4      38.5 GB  Q4_K_M   70B      ~/models/...
mistral-7b        4.1 GB   Q4_K_M   7B       ~/models/chat/...
```

Props: column sort (`▼` desc, `▲` asc), fixed or content-aware width, virtualized, hideable columns.
Keys: `s` cycle sort; `H`/`L` move column focus; `/` filter; `enter` open row.
A11y: sticky header; current row marked by `▶` or reverse video.

---

## Tabs

```
[Profiles]  Server  Models  Backends
```

With badge counts:

```
[Profiles · 12]  Server · 3  Models · 87  Backends · 4
```

Keys: `1`–`9` direct; `shift+l`/`shift+h` next/prev.
A11y: active tab uses both background AND a bracket marker.

---

## Form Field

Inline-edit, `label  value` layout.

```
Name        ▶ qwen-coder-30b
Port          8080
Backend       llama-server   [v]
Flash attn    [✓] enabled
Restart       (●) none  ( ) on-failure  ( ) always
```

Variants: text, number, dropdown (`[v]`), toggle (`[✓]`/`[ ]`), radio (`(●)`/`( )`), multi-line.
Keys: `tab`/`shift+tab` move; `enter` enter-edit; `esc` cancel-edit; `space` toggle for bool/radio; `ctrl+enter` commit multi-line.
A11y: focus marked by `▶` and a cursor; required fields marked `*`.

---

## Modal

Centered overlay with focus trap.

```
┌─ Confirm Delete ───────────────────────────┐
│                                            │
│  Delete profile "qwen-coder-30b"?          │
│  This cannot be undone.                    │
│                                            │
│                  [ No ]  [ Yes, delete ]   │
└────────────────────────────────────────────┘
```

Props: dismissable (`esc`), focus-trapped, default-focus on safe option for destructive modals.
Keys: `tab` cycle, `enter` activate, `esc` cancel.
A11y: title says what; body says why; buttons say what-will-happen (not "OK"/"Cancel").

---

## Toast / Notification

Non-blocking, top-right, auto-dismiss.

```
                                              ┌────────────────────────┐
                                              │ ✓ Profile saved        │
                                              └────────────────────────┘
```

Variants: success (`✓`), warning (`⚠`), error (`✗`), info (`ⓘ`).
Auto-dismiss in 3 s; sticky for errors until dismissed.

---

## Status Bar

Persistent, bottom, two zones: context info (left) + key hints (right).

```
Profiles · 12 items · 1 running │ a new  e edit  l launch  d del  / find    ?
```

Updates on focus change to show keys relevant to the focused widget.

---

## Spinner / Progress

```
⠋ Loading...
⣾ Probing backends...

████████████░░░░░░░░  62%  24.1 MB/s  ETA 4m
```

Spinner for indeterminate; bar for determinate. Always pair with a cancel key shown in the status bar.

Reduced-motion fallback: `[loading]` static token.

---

## Sparkline

Inline chart, one row tall.

```
tokens/s   ▁▃▆▇▆▄▃▅█▇▅▃▂▁▂▄▆█▇▅▃   115.4
```

Use 8-block Unicode (`▁▂▃▄▅▆▇█`). Right-align latest value. ASCII fallback: `:::.,...,:::` (low resolution).

---

## Bar Gauge

Multi-row meter — VRAM/GPU usage.

```
VRAM   ████████████████░░░░░░  16.2 / 24.0 GB  (68 %)
GPU    ██████████░░░░░░░░░░░░  42 %
```

Colorless variant uses density (`█`/`░`) only.

---

## Log Viewer

Tail-friendly, level-tinted, searchable.

```
┌─── Logs · qwen-coder ─────────────────────────────────────── tail ──────┐
│ [INFO]  llama_model_load: loading model                                 │
│ [INFO]  llama_print_meta: arch = qwen2                                  │
│ [WARN]  ggml_cuda_init: ggml_get_alloc...                               │
│ [INFO]  llm_load_tensors: offloaded 99/99                               │
│ [ERROR] HTTP request failed: connection reset                           │
│ ...                                                                     │
└─────────────────────────────────────────────────────────────────────────┘
```

Level prefix uppercase in brackets; color secondary. Keys: `p` pause tail; `/` search; `f` toggle follow.

---

## Picker / Dropdown

Inline popover from a field.

```
Backend   llama-server [v]
                       ┌────────────────────┐
                       │ ▶ llama-server     │
                       │   vllm             │
                       │   sglang           │
                       │   tabbyapi         │
                       └────────────────────┘
```

Triggered by `enter` or `space` on the field. `esc` closes.

---

## Help Overlay

Triggered by `?`. Modal, scrollable, grouped by section.

```
┌─ Help · Profiles tab ─────────────────────────────────────────────────────┐
│ Navigation                                                                │
│   j / ↓        next profile                                               │
│   k / ↑        previous profile                                           │
│   gg           top                                                        │
│   G            bottom                                                     │
│                                                                           │
│ Actions                                                                   │
│   enter        open profile detail                                        │
│   a            new profile                                                │
│   e            edit selected                                              │
│   l            launch                                                     │
│   d            delete (with confirm)                                      │
│   D            duplicate                                                  │
│   p            pin / unpin                                                │
│   / : ?        search · command palette · this help                       │
│                                                                           │
│ Press ? or esc to close                                                   │
└───────────────────────────────────────────────────────────────────────────┘
```

---

## Confirmation Inline

Less-destructive ops in the status bar instead of a modal.

```
│ Discard unsaved changes? [y/N]                                             │
```

Default is the negative (capitalized `N`). Any other key cancels.

---

## Slider / Numerical Input

```
n-gpu-layers   [────●───────────]   42 / 99      (●●●●○) fit: yellow
```

Movable cursor (`●`). `h`/`l` or `←`/`→` decrement/increment. `H`/`L` jump by 10. Status badge shows fit/range hint.

---

## Tree

Hierarchical list with expand/collapse.

```
▼ ~/models
  ▼ code
    qwen-30b-coder.gguf
    deepseek-coder-33b.gguf
  ▶ chat
  ▶ embed
  qwen-vl-72b.gguf
```

Keys: `enter` expand/collapse; `h`/`l` collapse/expand; `j`/`k` move.
A11y: expanded state shown by `▼` / `▶` (color-independent).

---

## Search Input

```
┌─ Search ──────────────────────────────────────────┐
│ > qwen 30b q5                          3 matches  │
└───────────────────────────────────────────────────┘
```

Debounced ~150 ms. Match count visible at right. `n`/`N` jumps when results scroll out of view.

---

## Tag / Chip

```
#code  #large  #q5
```

Selectable: `▶ #code  #large  #q5`. Multi-selectable: `◆ #code  #large ◆ #q5`.

---

## Breadcrumb

```
Profiles › Edit · qwen-coder-30b › Essentials
```

Always shown at top of nested screens. `h` or backspace goes up one level.
