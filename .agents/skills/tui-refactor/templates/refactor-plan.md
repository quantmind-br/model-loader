# Refactor plan — <tui-name>

> Phase 6. The deliverable. Maps the target design onto real files. No source
> code — precise enough that an implementer can execute it. Items back-referenced
> by the JSON schema in `references/refactor-planning.md`.

## Summary

- **Framework:** <…>
- **Ambition:** <cleanup / bold redesign>
- **Milestones:** <count> · **Plan items:** <count>
- **Keybinding changes:** <yes/no — see change log>
- **Structured items:** `plan-items.json`
- **No source files were modified.** This is a plan.

## Milestones

### M1 — Design-system foundation
Invisible to existing workflows; unblocks the rest.

#### P001 — <title>
- **What:** <concrete UI change>
- **Where:** `<file: symbol>`, `<file: symbol>`
- **How:** <framework-idiomatic approach>
- **Preserves:** <behavior/keys that must NOT change>
- **Changes keybindings:** <no / yes — see change log>
- **Effort / risk:** <S/M/L> / <low/med/high>
- **Validation:** <build + tui-validator + before/after diff>
- **Resolves:** <F00x, F00y>
- **Out of scope:** <per Phase 4, or n/a>

<repeat per item>

### M2 — Navigation & feedback shell
Status bar, `?` help, focus indicators, empty/loading/error states, async un-blocking.

### M3 — Per-screen rework
Apply target wireframes, highest-traffic screen first.

### M4 — Keybinding consolidation (only if Phase 4 approved)

#### Keybinding change log
| Key | Was | Now | Reason | Muscle-memory cost |
| --- | --- | --- | ------ | ------------------ |

## Coverage check (Phase 7)

- [ ] Every finding in `findings.json` maps to a plan item or is deferred
- [ ] Every keybinding change is in the change log
- [ ] Every target screen has plan items (or is out of scope)

## Next step

To implement: execute this plan milestone by milestone. To verify afterwards:
run `tui-validator` on the result and diff against `before/`.
