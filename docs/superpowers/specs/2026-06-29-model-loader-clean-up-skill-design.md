# Design — skill `model-loader-clean-up`

**Date:** 2026-06-29
**Status:** approved (brainstorming) → ready for implementation plan
**Author:** brainstorming session (Claude + operator)

## 1. Goal

A skill that analyzes every model-loader profile and helps the operator **reclaim disk
space** by removing model artifacts that are **incomplete/corrupt** or **unused by any
profile**. The skill is **destructive by design** (permanent `rm`), so its core discipline
is: always produce a read-only report first, then delete only what the operator confirms,
item by item.

Saved in two identical locations:
- `~/dev/model-loader/.claude/skills/model-loader-clean-up/` (project)
- `~/dev/skills/model-loader-clean-up/` (global skill collection, a git repo)

## 2. Decisions locked during brainstorming

| Topic | Decision |
|---|---|
| **Removal scope** | Orphans **+** unused sibling files inside used repos (per-file analysis). |
| **Deletion model** | Dry-run report first → permanent `rm`, confirmed **item by item**. |
| **Skill form** | Bundled analyzer script (`analyze.py`) + a lean `SKILL.md`. |
| **HF cache scope** | **Include** the shared HF cache, but **tiered** (see §5) to avoid nuking ComfyUI/other-tool assets. |
| **Analyzer impl** | Self-contained Python; reads profile JSON + `config.toml` directly, walks the store. No dependency on the `model-loader` binary or its CLI output. |

## 3. Environment facts that shape the design (observed 2026-06-29)

- **34 profiles** in `~/.config/model-loader/profiles/*.json` — the source of truth for "in use".
- **Store / search path**: `/home/diogo/models/huggingface` (`[models].search_paths`), **702 GiB**,
  HF layout `publisher/repo/<files>`. Files are mostly **real**; some GGUF repos hold **symlinks**
  to inner subdirs (e.g. `gemma-4-12B-it-Q8_0-MTP.gguf -> MTP/...`). Inner `.cache/` is tiny HF
  metadata (~36 K).
- **HF cache**: `~/.cache/huggingface` (**81 GiB**), `hub/models--org--repo/{blobs,snapshots,refs}`.
  **Shared with other tools** (ComfyUI/image gen: FLUX, RMBG, dinov3, TRELLIS, LivePortrait, …)
  *and* model-loader (cyankiwi AWQ, baidu Unlimited-OCR, LeaderboardModel1, Lorbus). Several repos
  exist in **both** store and cache (duplicates).
- Profiles reference artifacts via **four keys**: `model` (file *or* directory), `args.mmproj`,
  `args.spec-draft-model` (12 profiles), `args.chat-template-file` (a template, not a model), plus
  any `/`-paths in `extraArgs`.
- A single repo dir often holds **multiple referenced files** (base + mmproj + draft) → matching is
  **per file**, not per dir.
- Incomplete signals present: 2 `*.incomplete` under store `<repo>/.cache/huggingface/download/`,
  11 `*.incomplete` in the HF cache; plus `~/.local/state/model-loader/downloads/dl-*.json` states.
- No running instances at design time (`instances.json` → `{"instances": []}`).

## 4. Analyzer (`scripts/analyze.py`) — correctness is the whole point

Read-only. Inputs: `~/.config/model-loader/profiles/*.json`, `~/.config/model-loader/config.toml`
(for `search_paths`), `~/.local/state/model-loader/{instances.json,downloads/*.json}`,
`~/.cache/huggingface/hub/` (cache pass). Emits a JSON report + a human table.

### 4.1 Protected set (closure) — must not over-delete

For every profile, collect from `model`, `args.mmproj`, `args.spec-draft-model`,
`args.chat-template-file`, and `/`-prefixed `extraArgs`:

1. Protect the **literal path**.
2. Protect its **`realpath`** (a referenced **symlink** ⇒ its target is protected — this is the
   `MTP/` case that caused a false positive in the throwaway preview).
3. If `model` resolves to a **directory** → protect the **entire directory** as one unit
   (safetensors/AWQ/GPTQ/EXL2/EXL3 models; never prune inside).

### 4.2 Candidate categories (store)

- **Orphan repos** — `publisher/repo` dirs where nothing inside is in the protected set, and the dir
  itself isn't a protected directory-model. Unit = whole repo dir.
- **Unused sibling files** — only inside **used GGUF repos**, **per file**: real `.gguf` files
  (enumerated **recursively**, since real weights may live in subdirs like `MTP/`) that are **not in
  the protected closure** of §4.1 (refs ∪ their realpaths ∪ symlink targets). **Multipart-aware**:
  filenames matching `-(\d{5})-of-(\d{5})\.gguf`; if any part of a group is protected, **all** parts
  are protected. Never prune inside a directory-model repo.
- **Incomplete / corrupt** —
  - GGUF whose first 4 bytes ≠ `GGUF`;
  - multipart group missing shards (have < N of M);
  - `*.incomplete` under `<repo>/.cache/huggingface/download/`;
  - `*.partial` anywhere in the store;
  - downloads in `~/.local/state/model-loader/downloads/` whose state is
    `failed`/`abandoned`/`cancelled` (report the partial target path).

### 4.3 Size accounting

Count **real bytes once**: do **not** follow symlinks when summing (`os.path.getsize` on the link is
~0; walk with `followlinks=False`). Deleting an orphan dir reclaims the real files inside it. Ignore
the inner `.cache/` HF-metadata dir in size totals (but a stale `.cache/.../*.incomplete` is itself a
reclaim candidate). Every reported item carries its real size + mtime + a short reason.

### 4.4 Safety guards

- If `instances.json` lists a **running** instance whose profile uses a candidate → mark that
  candidate **BLOCKED** and exclude it from deletion.
- Never propose a path outside `search_paths` (store pass) or outside `~/.cache/huggingface/hub`
  (cache pass).
- Never propose a referenced `chat-template-file` or anything in the protected set.

## 5. HF cache handling — tiered (operator opted to include it)

The cache is shared, so "everything not referenced by a profile" is **not** a safe rule (it would
target ComfyUI assets). The analyzer tiers cache findings; the item-by-item confirmation is the
backstop:

- **Tier 0 — incomplete** (`*.incomplete`, broken blob refs): always reclaimable, any tool.
- **Tier A — store↔cache duplicates**: `models--org--repo` whose `org/repo` also exists as a real
  repo in the store. The store copy is what model-loader uses (profiles point at store paths); the
  cache copy is a leftover download → high-confidence reclaim.
- **Tier B — other unreferenced cache repos**: present in a **separate, loudly-warned** section
  ("may belong to ComfyUI or other tools — verify before deleting"), with repo name, size, and
  last-access mtime. **Never** auto-classified as a model-loader orphan; pure manual review.

Cache deletion unit = `rm -rf ~/.cache/huggingface/hub/models--org--repo` (blobs hold the real bytes;
snapshots are symlinks). Prefer `huggingface-cli delete-cache` when available; `rm -rf` of the repo
dir is the documented fallback.

## 6. Report format

JSON (machine) + a human table. Sections, each item with real size, mtime, reason, and BLOCKED flag:

1. **Store — orphan repos**
2. **Store — unused sibling files**
3. **Store — incomplete / corrupt**
4. **Cache — Tier 0 incomplete**
5. **Cache — Tier A duplicates**
6. **Cache — Tier B other (review)**

Footer: reclaimable totals **per section** and grand total; count of BLOCKED items. The JSON is the
deletion manifest the SKILL.md drives confirmation from.

## 7. Deletion flow (driven by SKILL.md)

1. Run `analyze.py` (always read-only) → show the table + totals.
2. Walk candidates with the operator, **item by item**; default is keep.
3. `rm` / `rm -rf` only confirmed items (cache repos via the §5 unit). Permanent, as chosen.
4. Re-run `analyze.py` to confirm reclaimed space.
5. Note: `gio trash` / `trash` exist on the box as an optional safety net if the operator changes
   their mind about permanence; `rm` is the chosen default.

## 8. Skill layout

```
model-loader-clean-up/
  SKILL.md              # lean: when to use, how to run, how to read the report, deletion flow
  scripts/analyze.py    # read-only analyzer → JSON + human table
  references/safety.md   # detailed safety rules: symlink closure, dir-vs-file, multipart, cache tiers
```

`SKILL.md` stays free of changelog/dev-notes (per user global instruction); any such notes live in a
separate `README.md` only if needed. All skill text is English (skill convention); operator-facing
chat remains Portuguese.

## 9. Dual-save & sync

Author once, place **identical** copies in both locations (§1). Keep them byte-identical. The plan
will decide author-location-then-copy vs symlink; default is two real copies kept in sync.

## 10. Testing posture (for the plan)

Because deletion is permanent, `analyze.py` is implemented **test-first** against a fixture tree that
reproduces every tricky case: a referenced symlink into a subdir (`MTP/`), a multipart group with a
referenced part, a directory-model (safetensors) that must never be pruned inside, a genuine orphan
repo, a bad-magic GGUF, a missing shard, a `*.incomplete`, a store↔cache duplicate, a Tier-B cache
repo, and a BLOCKED (running-instance) candidate. The analyzer must be proven correct on these before
the skill is allowed to drive any `rm`.

## 11. Out of scope

- No changes to model-loader source, profiles, or the catalog.
- No automatic deletion without per-item confirmation.
- No attempt to disambiguate Tier-B cache repos automatically (manual review only).
