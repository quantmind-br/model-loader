# Historical reports and evidence

These records preserve what was observed or proposed at their stated dates.
They are not current installation defaults, authorization to execute archived
prompts, or evidence that every planned gate passed. Current commands are in
[the maintained documentation](../README.md).

## Workstation tuning

- [Qwen3.8-27B audit, 2026-09-07](QWEN38_27B_AUDIT_REPORT.md): historical
  vLLM 0.28 calibration at 290 W/card; later work is in the
  [HyperQwen parity audit, 2026-09-27](hyperqwen-parity-dual-rtx3090-2026-09-27.md)
  at 270 W/card. Do not compare throughput without accounting for power caps.
- [Flash-Next research, 2026-09-08](QWEN3.8-FLASH-NEXT-OTIMIZACAO-DUAL-RTX3090.md),
  [earlier audit](qwen3.8-flash-next-dual-rtx3090-2026-09-04.md),
  [tuning results](qwen-flash-tuning-results.md) and
  [patches/measurements](qwen-flash-tuning-artifacts/README.md). The seven
  unused local tuning backend trees were retired in a separate cleanup; their
  source/history backup is recorded with the tuning artifacts.
- [Qwen3.6 workflow, June 2026](qwen36-benchmark-workflow-2026-06.md): retired
  profiles and helpers, retained as history. Use [Benchmark](../BENCHMARK.md).

## Strata

- [Audit originals and relocation guide](strata-audit-2026-10-01/README.md):
  the consolidated report and original independent audit retain exact bytes.
- [Independent Claude audit](strata-independent-claude-opus-5-5-2026-10-01.md)
  and [consolidated review](strata-consolidated-independent-review-2026-10-01.md).
- [Implementation and final V7 decision](strata-implementation-2026-10-01/IMPLEMENTATION.md):
  V7 performance improved, but independent tool-quality failures blocked both
  profile sizes. Canonical 256k and 500k profiles remain on V6.
- [256k calibration](strata-iq2-xs-256k-calibration-2026-10-01.md) and
  [retired 976k context experiment](strata-iq2-xs-context-2026-10-01.md).
- Retired uncensored experiments: [IQ4_XS](strata-iq4-xs-uncensored-2026-10-01.md),
  [IQ2_XS requant](strata-uncensored-iq2-xs-requant-2026-10-01.md),
  [Orca setup](strata-orca-iq3-xxs-profile-2026-10-01.md),
  [Orca calibration](strata-orca-iq3-xxs-calibration-2026-10-01.md) and
  [frozen Orca example configs](strata-orca-iq3-xxs-examples-2026-10-01/README.md).
- [Archived session-2 task prompt](strata-implementation-2026-10-01/CLAUDE_TASK-session-2.md)
  is distinct from the existing session-1 `CLAUDE_TASK.md`; neither is a live
  instruction. Raw `claude-session/runs/v7ab-*`, comparisons and rollback assets
  remain available. Repeated measurements are not disposable duplicates.

## UI audits and plans

- [UI/UX audit, 2026-07-30](uiux-audit-2026-07-30/README.md): unchanged report
  with terminal captures, screenshots and accessibility measurements.
- [Archived plans](plans/README.md): proposals and task briefs, not automatic
  requirements or assertions of completion.

## Preservation and relocation

The 2026-10-04 organization preserved original files before removal or movement:

- Backup directory:
  `~/.local/state/model-loader/cleanup-backups/repository-organization-20261004T114519Z/`.
- `repository-originals.tar.zst`: source, docs, existing local edits and affected
  disposable files; SHA-256
  `73b0c43b8bccd7a28752f79e3784d7cf80fd11eabb036d33f298c98ccf34afc5`.
- `uiux-originals.tar.zst`: the ignored `.ideation` evidence directory;
  SHA-256 `2ebbda564a5e18ef16e259cdeb04ffbeb0e867725976c13edf40eea3bd29ba4c`.
- Archive comparison and representative extraction were verified before cleanup.
  [Relocation manifest](repository-organization-2026-10-04.json) records old/new
  paths, hashes and whether only navigation was updated.

Frozen reports/manifests retain their original root-relative links, absolute
workstation paths and recorded hashes. Interpret them using the relocation
manifest rather than rewriting evidence. The Strata audit companion provides
clickable relocated evidence links. Generated OpenWiki references will converge
on regeneration; generated pages were intentionally not edited.

Removed from the active tree: generated `.reversa` host cache, temporary
`renameprofile` binary and `llama.log`, nonvendored Python bytecode, five obsolete
Qwen3.6 benchmark helpers, and three obsolete Cloudflare polling assets. The
current Cloudflare installer still removes legacy installed watcher units;
active lifecycle templates, backend installations, V6/V7 releases, model weights,
current profiles and dataset-curation tools were not removed.
