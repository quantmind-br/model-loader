# TUI Inspiration

Best-in-class TUIs and what to borrow from each.

## lazygit

Multi-pane Git porcelain.

- **Borrow:** Contextual panels that each own their own keybindings; key hints in the panel borders; the "5-pane" layout (files / branches / commits / stash / status); modal-light design (most actions are single keypress).
- **Specifically:** Per-panel borders contain the key hints. Excellent feedback for staged/unstaged state with color + symbols + position.

## k9s

Kubernetes browser.

- **Borrow:** Command bar (`:pods`, `:deploy`) for navigation; persistent header showing context; filter as a first-class action; consistent across all "resource" screens.
- **Specifically:** "Command-driven navigation" — typing `:` then a resource type jumps you. Powerful in apps with many lists.

## btop / htop / bottom (btm)

System monitors.

- **Borrow:** Sparklines and bar gauges for real-time metrics; resizable panels; color-coded but never color-only; smooth re-renders at 1–2 Hz.
- **Specifically:** btop's mouse support layered on a keyboard-first design. bottom's grid layout with user-defined widget placement.

## gh dash

GitHub dashboard.

- **Borrow:** Sortable, filterable, multi-section table layout; section-based config; "sections" as a first-class concept.

## neovim + telescope

Editor + fuzzy picker.

- **Borrow:** The telescope pattern (live preview alongside candidate list); modes; leader keys for namespacing actions; `:help` as discoverable doc.
- **Specifically:** Telescope's preview-on-the-right. Great for any "pick something" interaction.

## ranger

Miller-columns file manager.

- **Borrow:** Hierarchical drill with `h`/`l`; preview on right; sort modes; bulk operations with marks.

## gitui

Rust Git porcelain.

- **Borrow:** Smooth rendering and instant feedback; concise key hints; diff viewer with syntax-aware coloring.

## atac

API client TUI.

- **Borrow:** Multi-pane request/response layout; tabs as documents; env var management.

## taskwarrior-tui (vit)

Task manager.

- **Borrow:** Filter as URL-style query; inline edit; report views.

## micro

Editor.

- **Borrow:** Mouse-friendly without compromising keyboard; intuitive shortcuts (`ctrl+s` save); plugin discovery.

## tig

Git history browser.

- **Borrow:** Stacked views with smooth back/forward navigation; consistent `j`/`k`/`enter` everywhere.

## glow

Markdown reader.

- **Borrow:** Beautiful rendering of styled content; pager UX done right.

## fzf

Fuzzy finder (used standalone or embedded).

- **Borrow:** Incredible search-as-you-type performance; preview pane; multi-select with tab.

---

## Patterns Worth Stealing

1. **lazygit borders** — put the panel's keybindings inside its bottom border.
2. **k9s `:` command bar** — when actions outnumber screen keys.
3. **btop dashboard layout** — for any monitoring tab.
4. **telescope preview** — for any picker.
5. **gh dash sections** — for grouping related lists in one tab.
6. **gitui diff view** — for any side-by-side comparison.
7. **tig stacked views** — push/pop navigation with consistent back semantics.
8. **fzf multi-select** — tab to mark, enter to confirm.

## Anti-Patterns to Avoid

The anti-pattern catalogue is maintained once in `references/design-principles.md`
(shared with `tui-refactor`). See it there — not duplicated here.
