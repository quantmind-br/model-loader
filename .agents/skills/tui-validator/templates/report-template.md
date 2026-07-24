# TUI Validator - Audit Report

**Application**: `{{ TUI_BIN }}`
**Version**: `{{ TUI_VERSION }}`
**Args**: `{{ TUI_ARGS }}`
**Working directory**: `{{ TUI_CWD }}`
**Timestamp**: `{{ TIMESTAMP }}` UTC
**Pipeline**: `tui-validator` skill (tmux + capture-pane + optional screenshots)
**Workspace**: `{{ WORKSPACE }}`

---

## 1. Summary

<!-- One paragraph: what was tested, what was found at a glance, and the
verdict. Keep this skimmable in about 10 seconds. -->

**Severity breakdown:**

| Severity | Count |
| --- | ---: |
| Blocker | {{ N_BLOCKERS }} |
| Major | {{ N_MAJORS }} |
| Minor | {{ N_MINORS }} |
| Cosmetic | {{ N_COSMETIC }} |
| Info | {{ N_INFO }} |

| Audit stat | Value |
| --- | --- |
| Captures (text + ANSI) | {{ N_CAPS }} |
| Screenshots | {{ N_SHOTS }} |
| Keybindings inventoried | {{ N_BINDS }} |
| Initial geometry | {{ COLS }} x {{ ROWS }} |
| TERM | `{{ TERM }}` |

---

## 2. Keybindings Inventory

Raw file: `{{ WORKSPACE }}/keybindings.json`.

{{ KEYBINDINGS_TABLE }}

---

## 3. Findings

{{ FINDINGS_SECTIONS }}

---

## 4. Visual Gallery

Diff maps, when generated with `tui-screenshot.sh --diff`, are stored next to
the screenshots.

{{ VISUAL_GALLERY }}

---

## 5. Methodology

### Phases Executed

| Phase | What was done | Status |
| --- | --- | --- |
| 1. Discover | Located binary, read project docs, ran `--help`/`--version` when safe | |
| 2. Inventory | Captured help screen(s); parsed keybindings into `keybindings.json` | |
| 3. Probe | Sent documented/common bindings per context; classified each as active / dead / error / crash | |
| 4. Stress | Sent Unicode, paste/control characters, and rapid input where safe | |
| 5. Visual | Captured resize matrix and optional diffs against baseline | |
| 6. Report | Rendered this document | |

### Coverage

- **Keys probed**:
- **Modes tested**:
- **Geometries**:
- **Not tested (and why)**:

### Limitations

<!-- Note missing tools, headless screenshot fallback, skipped destructive
keys, network-bound actions, permissions, fonts, or other constraints. -->

---

## 6. Reproducibility

Every blocker and major finding should be reproducible from a fresh launch.

| Finding | Repro from fresh boot? | Steps |
| --- | --- | --- |
| | | |

---

## 7. Improvement Suggestions

<!-- UX nits, design proposals, missing affordances, and future improvements
that are not bugs. -->

---

## 8. Prioritized Recommendations

| Priority | Item | Resolves |
| --- | --- | --- |
| P0 | | |
| P1 | | |

---

## 9. Workspace

```
{{ WORKSPACE }}/
├── meta.json
├── keybindings.json
├── findings.json
├── captures/      ({{ N_CAPS }} text + ANSI scrapes)
└── screenshots/   ({{ N_SHOTS }} PNGs)
```

---

## 10. Appendix - Environment

- **TERM**: `{{ TERM }}`
- **Initial geometry**: {{ COLS }} x {{ ROWS }}
- **Binary**: `{{ TUI_BIN }}`
- **Version**: `{{ TUI_VERSION }}`
- **Args**: `{{ TUI_ARGS }}`
- **CWD**: `{{ TUI_CWD }}`
