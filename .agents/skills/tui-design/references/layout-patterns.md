# TUI Layout Patterns

Catalog of common layout patterns for terminal user interfaces. For each: when to use, an ASCII example at 80×24, and variants. Combine freely.

---

## 1. Full-Screen Single Pane

Use when the app is one focused workflow (editor, reader, viewer).

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ filename.md                                                       2024-01-01 │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  # Document                                                                  │
│                                                                              │
│  Body content scrolls here. The single pane fills the terminal.              │
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
│ NORMAL │ Ln 1, Col 1 │ q quit  ? help  : command                             │
└──────────────────────────────────────────────────────────────────────────────┘
```

Pros: maximum content area. Cons: navigation depends on modes or commands.

---

## 2. Top Tabs + Body + Status Bar

Default for "dashboard" apps with 3–6 peer functional areas.

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ [Profiles] Server  Models  Backends                              app v1.0    │
├──────────────────────────────────────────────────────────────────────────────┤
│ ... body of selected tab ...                                                 │
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
│ Profiles · 12 items · 1 running │ a new  e edit  l launch  d del  / find   ? │
└──────────────────────────────────────────────────────────────────────────────┘
```

Tab selection: `1`–`9` direct; `shift+l`/`shift+h` next/prev; mouse click optional. Focused tab is bracketed or background-highlighted; idle tabs dimmed.

---

## 3. Master-Detail (List + Detail)

User picks one item from a list and views/edits its details.

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ Profiles                                                          12 items   │
├────────────────────────┬─────────────────────────────────────────────────────┤
│ ▶ qwen-coder-30b       │ Name        : qwen-coder-30b                        │
│   llama-70b-q4         │ Model       : ~/models/qwen-30b-q5.gguf             │
│   mistral-7b-instruct  │ Backend     : llama-server (default)                │
│   deepseek-coder-33b   │ Port        : 8080                                  │
│   solar-10.7b-q5       │ NGL         : 99                                    │
│   yi-34b-chat          │ Ctx size    : 16384                                 │
│   mixtral-8x7b         │ Last used   : 2 hours ago                           │
│   phi-2                │                                                     │
│   ...                  │ ── Status ──                                        │
│                        │ ● Running on :8080  (PID 19342, 1h 12m)             │
│                        │                                                     │
│                        │ ── Tags ──                                          │
│                        │ #code #large #q5                                    │
│                        │                                                     │
│                        │                                                     │
│                        │                                                     │
├────────────────────────┴─────────────────────────────────────────────────────┤
│ j/k move  enter open  l launch  e edit  d delete  / search                 ? │
└──────────────────────────────────────────────────────────────────────────────┘
```

Left list ≈ 25–30 % width. Right detail ≈ 70–75 %. On narrow terminals (<100 cols), collapse to "list-then-detail" (detail replaces list on enter).

---

## 4. Three-Pane / Miller Columns

Hierarchical drill (file managers, nested categories).

```
┌─────────────┬─────────────┬──────────────────────────────────────────────────┐
│ Categories  │ Models      │ Details                                          │
├─────────────┼─────────────┼──────────────────────────────────────────────────┤
│ ▶ Code      │ ▶ qwen-30b  │ qwen-30b-coder-q5_k_m.gguf                       │
│   Chat      │   deepseek  │ Size       : 22.4 GB                             │
│   Embedding │   codellama │ Quant      : Q5_K_M                              │
│   Vision    │   starcoder │ Params     : 30B                                 │
│   Audio     │             │ Arch       : qwen2                               │
│             │             │ Blocks     : 48                                  │
│             │             │ Path       : ~/models/code/qwen-30b...           │
│             │             │                                                  │
│             │             │ Used by    : 2 profiles                          │
│             │             │   · qwen-coder                                   │
│             │             │   · qwen-coder-fast                              │
│             │             │                                                  │
├─────────────┴─────────────┴──────────────────────────────────────────────────┤
│ h/l columns  j/k items  enter drill  esc back  / search                    ? │
└──────────────────────────────────────────────────────────────────────────────┘
```

Width: 1:1:2 ratio is common. Selecting in left column repopulates middle; selecting in middle populates right.

---

## 5. Top Tabs + Master-Detail with Sub-Tabs

Complex apps where each tab contains its own master-detail with sub-views.

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ Profiles  [Server]  Models  Backends                                         │
├────────────────────────┬─────────────────────────────────────────────────────┤
│ Running instances  3   │ [Logs] Slots  Metrics  History                      │
├────────────────────────┼─────────────────────────────────────────────────────┤
│ ▶ qwen-coder :8080     │ [INFO]  llama_model_load: loading model             │
│   mistral-7b :8081     │ [INFO]  llama_print_meta: arch = qwen2              │
│   embed-model :8082    │ [WARN]  ggml_cuda_init: ggml_get_alloc...           │
│                        │ [INFO]  llm_load_tensors: offloaded 99/99           │
│                        │ [INFO]  HTTP server listening on 0.0.0.0:8080       │
│                        │ [INFO]  HTTP slot 0 idle                            │
│                        │ [INFO]  request POST /v1/chat/completions           │
│                        │ [INFO]  decoded 142 tokens in 1.23s (115.4 tok/s)   │
│                        │ ...                                                 │
│                        │                                                     │
│                        │                                                     │
│                        │                                                     │
├────────────────────────┴─────────────────────────────────────────────────────┤
│ x kill  r restart  tab switch view  / filter logs                          ? │
└──────────────────────────────────────────────────────────────────────────────┘
```

Sub-tabs sit inside the right pane and scope to the selected master item.

---

## 6. Top Tabs + Bottom Panel (Drawer)

Use when there's a global secondary stream (logs, console, downloads queue).

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ Profiles  Server  [Models]  Backends                                         │
├──────────────────────────────────────────────────────────────────────────────┤
│ NAME                     SIZE      QUANT       PARAMS    PATH                │
│ ▶ qwen-30b-coder         22.4 GB   Q5_K_M      30B       ~/models/code/...   │
│   llama-70b              38.5 GB   Q4_K_M      70B       ~/models/...        │
│   mistral-7b             4.1 GB    Q4_K_M      7B        ~/models/chat/...   │
│   deepseek-coder         12.4 GB   Q4_K_M      14B       ~/models/code/...   │
│   ...                                                                        │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
├──────────────────────────────────────────────────────────────────────────────┤
│ Downloads (2 active, 1 queued)                                  toggle: D    │
│ ⬇ qwen-72b-q4.gguf     ████████████░░░░░░  62%   24.1 MB/s  ETA 4m           │
│ ⬇ codellama-13b.gguf   ██░░░░░░░░░░░░░░░░  11%    3.2 MB/s  ETA 23m          │
│ ⏸ mixtral-8x7b.gguf    queued                                                │
├──────────────────────────────────────────────────────────────────────────────┤
│ j/k move  enter info  u use in profile  D toggle downloads  / search       ? │
└──────────────────────────────────────────────────────────────────────────────┘
```

Bottom panel toggles open/closed with a dedicated key. When closed, only a count remains in the status bar.

---

## 7. Modal Overlay

Centered, ~60–80 % of screen, with a darken/dim layer behind. Focus trapped inside.

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░ │
│ ░░         ┌──────────────────────────────────────────────────┐         ░░░ │
│ ░░         │ Confirm Delete Profile                           │         ░░░ │
│ ░░         ├──────────────────────────────────────────────────┤         ░░░ │
│ ░░         │                                                  │         ░░░ │
│ ░░         │  Delete profile "qwen-coder-30b"?                │         ░░░ │
│ ░░         │                                                  │         ░░░ │
│ ░░         │  This cannot be undone.                          │         ░░░ │
│ ░░         │                                                  │         ░░░ │
│ ░░         │                       [ No ]   [ Yes, delete ]   │         ░░░ │
│ ░░         └──────────────────────────────────────────────────┘         ░░░ │
│ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░ │
├──────────────────────────────────────────────────────────────────────────────┤
│ tab focus  enter confirm  esc cancel                                         │
└──────────────────────────────────────────────────────────────────────────────┘
```

Always default focus on the safer button. Destructive modals: default is "No".

---

## 8. Command Palette

Use when actions are many and screen-scoped menus become bloated.

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░ │
│ ░░░ ┌────────────────────────────────────────────────────────────────┐ ░░░ │
│ ░░░ │ > launch profile                                               │ ░░░ │
│ ░░░ ├────────────────────────────────────────────────────────────────┤ ░░░ │
│ ░░░ │ ▶ Launch profile...                                  enter     │ ░░░ │
│ ░░░ │   Launch with custom port...                                   │ ░░░ │
│ ░░░ │   Launch all favorites                                         │ ░░░ │
│ ░░░ │   Show launch history                                          │ ░░░ │
│ ░░░ └────────────────────────────────────────────────────────────────┘ ░░░ │
│ ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░ │
├──────────────────────────────────────────────────────────────────────────────┤
│ enter run  esc close  ↑↓ navigate                                            │
└──────────────────────────────────────────────────────────────────────────────┘
```

Opens with `:` or `ctrl+k`. Fuzzy-matched commands ranked by recency + relevance.

---

## 9. Form / Editor Layout

Configuration screens with many fields.

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ Edit Profile · qwen-coder-30b                                       *unsaved │
├──────────────────────────────────────────────────────────────────────────────┤
│ [Essentials]  Advanced  Environment  Sizing                                  │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│ ── Identity ──────────────────────────────────────────────────────────────── │
│     Name      ▶ qwen-coder-30b                                               │
│     Slug        qwen-coder-30b                                               │
│     Tags        #code #large #q5                                             │
│                                                                              │
│ ── Model ─────────────────────────────────────────────────────────────────── │
│     Path        ~/models/qwen-30b-q5.gguf                          [pick]    │
│     Backend     llama-server                                          [v]    │
│                                                                              │
│ ── Launch ────────────────────────────────────────────────────────────────── │
│     Port        8080                                                         │
│     NGL         99                                                           │
│     Ctx size    16384                                                        │
│     Restart     on-failure                                            [v]    │
│                                                                              │
├──────────────────────────────────────────────────────────────────────────────┤
│ tab next  enter edit  ctrl+s save  ctrl+z undo  esc discard               ?  │
└──────────────────────────────────────────────────────────────────────────────┘
```

Sub-tabs at top scope the form. The `*unsaved` indicator is sacred — never let the user lose work without confirm.

---

## 10. Picker With Live Preview

Telescope-style: candidate list on left, preview on right.

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ > qwen                                                            3 matches  │
├──────────────────────┬───────────────────────────────────────────────────────┤
│ ▶ qwen-coder-30b     │  Profile preview                                      │
│   qwen-7b-chat       │  ───────────────                                      │
│   qwen-vl-72b        │  Model       : qwen-30b-q5.gguf                       │
│                      │  Backend     : llama-server                           │
│                      │  Port        : 8080                                   │
│                      │  NGL         : 99                                     │
│                      │  Ctx size    : 16384                                  │
│                      │                                                       │
│                      │  Last used   : 2 hours ago                            │
│                      │  Tags        : code, large, q5                        │
│                      │                                                       │
│                      │  Args:                                                │
│                      │    --flash-attn                                       │
│                      │    --cache-type-k q8_0                                │
│                      │                                                       │
├──────────────────────┴───────────────────────────────────────────────────────┤
│ enter pick  esc cancel  ctrl+space details                                   │
└──────────────────────────────────────────────────────────────────────────────┘
```

Type filters; preview updates live as selection moves.

---

## Selection Criteria

| Spec implies… | Use |
|---|---|
| Single focused workflow | Pattern 1 |
| 3–6 peer functional areas | Pattern 2 |
| Browse-then-inspect | Pattern 3 |
| Hierarchical drill-down | Pattern 4 |
| Per-tab master-detail with sub-views | Pattern 5 |
| Global secondary stream | Pattern 6 |
| Confirmations / alerts | Pattern 7 |
| Action-rich app, many commands | Pattern 8 |
| Multi-field configuration | Pattern 9 |
| Fuzzy pickers | Pattern 10 |

Non-trivial apps combine 3–5 of these.
