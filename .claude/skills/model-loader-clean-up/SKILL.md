---
name: model-loader-clean-up
description: Use when the operator wants to reclaim disk space from the model-loader model store or Hugging Face cache by removing incomplete/corrupt or unused-by-any-profile model artifacts. Triggers — "clean up models", "free disk space", "remove unused models", "delete old/incomplete downloads", "limpar modelos", "recuperar espaço em disco", "modelos não usados", or any request to audit which downloaded weights no profile still references. Cross-references every profile against the store + HF cache, reports reclaimable space by category, and deletes only what the operator confirms item by item.
---

# model-loader Clean-Up

Reclaim disk space by deleting model artifacts that are **incomplete/corrupt** or **unused by
every profile**. Deletion is **permanent** — so the flow is always: read-only report → confirm
item by item → `rm`.

## Step 1 — Analyze (read-only, never deletes)

```bash
python3 "$SKILL_DIR/scripts/analyze.py" --json /tmp/cleanup-manifest.json
```

(`$SKILL_DIR` is this skill's directory. Add `--no-cache` to skip the shared HF cache pass.)

The report has these categories (each line shows real reclaimable bytes + the exact delete unit):

- `store-orphan-repo` — a `publisher/repo` dir no profile references at all. Delete unit = the dir.
- `store-unused-sibling` — an unused `.gguf` inside an otherwise-used repo (e.g. an extra quant).
- `store-incomplete` — corrupt GGUF (bad magic), missing multipart shards, `*.partial`,
  `*.incomplete`, or the target of a failed download.
- `cache-incomplete` — partial blobs in `~/.cache/huggingface` (safe for any tool).
- `cache-duplicate` — a cache repo that also exists in the store (store copy is canonical).
- `cache-other` — a cache repo not in the store/profiles. **May belong to ComfyUI or another
  tool** — verify before deleting.

Items marked `[BLOCKED]` (active download or running instance) are excluded — never delete them.

## Step 2 — Review with the operator, item by item

Read the manifest (`/tmp/cleanup-manifest.json`). Present candidates grouped by category, largest
first, with size and reason. Default is **keep**. Treat each category by risk:

- `store-orphan-repo` / `store-incomplete` / `cache-incomplete` / `cache-duplicate` — low risk, but
  still confirm each (an "orphan" may be kept intentionally for a planned profile — e.g. a draft
  model for a not-yet-created profile).
- `store-unused-sibling` — confirm the operator does not want that extra quant.
- `cache-other` — **highest caution**: confirm it is not a ComfyUI/other-tool asset before deleting.

## Step 3 — Delete only confirmed items

For each confirmed candidate, delete its `delete_unit`:

```bash
rm -f  "<delete_unit>"     # files (sibling / incomplete / cache-incomplete)
rm -rf "<delete_unit>"     # directories (orphan repos / cache repos)
```

(Optional safety net: `gio trash "<delete_unit>"` or `trash "<delete_unit>"` are installed if the
operator prefers reversible removal over permanent `rm`.)

## Step 4 — Verify reclaimed space

Re-run Step 1 and confirm the deleted items are gone and the GRAND TOTAL dropped.

See `references/safety.md` for the exact protection rules (symlink closure, directory-models,
multipart groups, cache tiers) the analyzer enforces.
