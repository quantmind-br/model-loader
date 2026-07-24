# Safety rules enforced by `analyze.py`

These are the invariants that keep the analyzer from proposing an in-use artifact.

## Protected closure (never proposed for deletion)
- Every **absolute path** a profile references is protected: from `model`, from **any** `args`
  value, and from **any** `extraArgs` value. Values are harvested recursively, so a path nested
  inside a **JSON-valued** arg (e.g. vLLM `--speculative-config '{"method":"dflash","model":"/…/DFlash"}'`)
  or under any non-obvious key (`mtp-head`, `speculative-config`, …) is protected too — there is
  **no key whitelist**.
- A protected path protects **both its literal path and its `realpath`**. A referenced **symlink**
  (e.g. `draft.gguf -> MTP/draft.gguf`) therefore protects the real target in the subdir.
- Any referenced path that resolves to a **directory** (safetensors / AWQ / GPTQ / EXL2 / EXL3, or a
  JSON-referenced drafter dir) protects the **entire directory** — the analyzer never prunes
  individual files inside a directory-model.

## Multipart GGUF
- Files named `<base>-<NNNNN>-of-<MMMMM>.gguf` form a group. If **any** part is protected, **all**
  parts are protected. Only part `00001` carries the `GGUF` magic, so magic checks skip parts `2+`.
- A group present on disk with fewer than `MMMMM` shards is reported as `store-incomplete`.

## Sizing
- Real bytes are counted once; symlinks contribute 0 and are never followed. Deleting a directory
  reclaims the real files it contains.

## HF cache tiers (shared with other tools)
- `cache-incomplete`: partial blobs — safe to remove for any tool.
- `cache-duplicate`: `models--org--repo` whose `org/repo` also exists in the store — the store copy
  is what model-loader uses, so the cache copy is redundant.
- `cache-other`: any other cache repo — **not** auto-classified as model-loader's; manual review
  only, because it may belong to ComfyUI or another tool.

## BLOCKED items
- A candidate whose target is being written by an **alive download worker**, or referenced by a
  **running instance's** profile, is marked `[BLOCKED]` and excluded from deletion.
