
## F3 Re-Run Verification (commit 0835265) — 2026-05-19

### PASS (7/9)
- **F-01** Esc on Profiles + Backends: process PID 3377438 survived Esc on both tabs. App did NOT quit.
- **F-04** Filter captures keystrokes: `/` + `Qwen` narrowed list to 2 Qwen profiles. Letters consumed by filter.
- **F-06** `e` in filter does NOT export: typed `/` + `e`, filter showed "Filter: e", export dir count stayed at 3.
- **F-07** `/` filter in Backends: `cublas` narrowed list to 3 matching items (cublas16f, buun-cublas16f, v11 — tags match too).
- **F-08** Server H hint includes metrics_dir: hint reads `history: no metrics directory configured (set logging.metrics_dir in config.toml)`.
- **F-11** Args renders as --flag value lines: detail panel shows `--batch-size 4096`, `--cache-type-k q8_0`, etc. — NOT `map[...]`.
- **F-12** Help modal scrolls: PgDown produced 31-line diff between before/after captures. Content advances.

### FAIL (2/9)
- **F-02 PARTIAL FAIL** — `i` toggle works (open/close on repeated press), but **Esc does NOT close the info panel**. After `3 → Down → i → i → i → Esc`, Model Info panel remained visible. Esc-close handler missing for info overlay in Models page.
- **F-09 FAIL** — Pinned profile "Gemma 4 E4B Q4_K_S (RTX 3090, max TPS)" appears **TWICE** in Profiles list (positions 1 and 5). No pin glyph (📌 or ★) visible on either entry. Pinning collapse + glyph render both broken.

### Result
Scenarios [7/9 pass] | Original findings [2/9 recurrent] | VERDICT: REJECT
