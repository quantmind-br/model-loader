# Strata fork

The `strata` kind launches the fork's Python HTTP server, which owns the native
CUDA engine. Register the server, not the native `strata` stdin executable:

```sh
RELEASE_DIR='/absolute/path/to/strata-fork/releases/<release-id>'
BACKEND_ID='strata-perf-v<N>-<YYYYMMDD>'
model-loader backend add "$BACKEND_ID" --kind strata \
  --executable "$(command -v python3) $RELEASE_DIR/serve/server.py"
model-loader backend schema refresh "$BACKEND_ID"
```

Substitute real values for both placeholders (real ids contain no angle
brackets). This recipe requires the Python executable and `RELEASE_DIR` paths
to contain no whitespace or shell quote characters: the catalog stores a
compound command and parses it again at launch, so outer shell quotes alone
do not preserve whitespace inside either path.

Strata schemas are embedded curated rows; no `--help` parsing is involved.
Never register the mutable working-tree `serve/server.py`: releases under
`backends/strata-fork/releases/` are immutable snapshots (source hashes,
toolchain and test provenance in `BUILD.json`), so a release entry keeps its
server and native binary together for rollback.

Requires [quantmind-br/Strata](https://github.com/quantmind-br/Strata) with the
managed `--model` and `--max-context` server options. The local v7 port branch
`sync/upstream-0.1.38` starts from upstream `v0.1.38` (`99f3dbd`) and preserves
the fork's managed controls, stage-dense loading, helper affinity and thermal
gate. A published release is not a profile-promotion or agent-qualification
claim.

Local state (2026-10-04): catalog entries `strata-perf-v6-20261002`
(release `20261002T100952Z-2eed88f1f51d`, fork commit `97cb786` plus the
recorded working tree on Engine 0.1.30) and `strata-perf-v7-20261004`
(release `20261004T050705Z-868e757a424d`, upstream base `v0.1.38` (`99f3dbd`)
plus the local port at `1ee1445`). Both canonical profiles run on V6. V7 was evaluated
and **not** promoted: it decodes faster than V6 on both sizes (+7.8% at 256k,
+7.6% at 500k, exact retrieval) but failed the tool-quality gates — the 256k
integrity harness failed 3/3 on reissued tool-call continuation and the 500k
auto harness failed 3/3. The V7 entry and release are retained for reference;
no public call has been served from a V7 profile.
Model Loader does not build Strata, download weights or prepare expert packs.

The performance implementation uses immutable candidate releases under
`backends/strata-fork/releases/`. `backend-build.sh` performs a clean CUDA SM86
build and records hashes, toolchain and test provenance in `BUILD.json`; it does
not replace an active executable. Register a candidate's `serve/server.py` in a
separate catalog entry, and point its prepared config at that release's native
executable. Keep the old server and native binary together for rollback.

Request controls depend on the registered release. The v7 port validates finite
sampling values, preserves explicit seed zero, applies incremental stop
sequences, supports forced/named tool selection and a single-call limit, and
retains upstream structured JSON output and streaming tool events. Primitive
argument checks are not full tool JSON Schema validation. Model-level tool
selection and continuation must pass the harness independently of HTTP support.
See the [implementation record](reports/strata-implementation-2026-10-01/IMPLEMENTATION.md)
for measured results, failed guards, remaining validation and rollback.

Select the native GGUF's first shard as the profile model. Profile arguments:

```json
{
  "config": "/absolute/path/to/prepared-strata.json",
  "max-context": 32768,
  "host": "127.0.0.1"
}
```

Set `launch.backendId` to the registered release entry (currently
`strata-perf-v6-20261002` for the canonical profiles). The manager adds `--engine strata`,
`--model` and an allocated `--port`. The server checks that the profile model
matches the config's `--native` file: changing the model alone cannot silently
load a different prepared pack. `max-context` overrides the config in memory
and is advertised by `/v1/models` and used by context-aware benchmarks.

The prepared JSON owns `exe` (the compiled engine), `args` (pack, native/PLE
weights, expert profile/cache, MTP, KV and CPU settings), `cwd`, `tokenizer`,
`gpu`, `layer_split`, `model_name` and `sampling`. Prefer absolute paths. Keep
its adjacent `<config-stem>.shared-settings.json` to preserve the reasoning
default. Native engine flags are not HTTP server flags and must stay in the
config. Optional `gpu` in the profile overrides config GPU selection.

The HTTP listener starts after engine initialization, so normal `/health`
readiness applies. The manager terminates the server and its engine process
group together. Leave Strata's own idle-unload disabled for predictable
Model Loader lifecycle management. Backend authentication must be disabled on
the internal loopback listener; Model Loader does not inject a Strata API key.

For external requests reaching a v7 server without its own API key, add only
the intended hostname to `allowed_hosts` in the prepared JSON. The Model Loader
proxy preserves the client's Host header; the authenticated Cloudflare gateway
does not bypass Strata's DNS-rebinding check.

The performance probe records immutable proxy/server/native process identities
and native request totals. Every probe and bundled harness generation is
accounted through response completion; unaccounted requests, replacements or
failed identity observations invalidate the attempt. With `--wait-harness`,
the loaded instance and resource sampler remain alive until `harness-done.json`
is terminal, even when the harness exceeds `--linger`. Tool-quality failures
are reported separately from valid performance medians and still block
promotion. Candidate runs use `--baseline v7` to avoid historical snapshot
reuse; contaminated attempts are never included in comparison medians.

The two canonical IQ2_XS profiles use both cards, mmap experts, 15 workers,
int8 KV, MTP spec 4, `spec-min-p=0.7` and a 270 W cap per RTX 3090:

- `qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-256k`: native 262144
  tokens, no YaRN. See the
  [calibration report](reports/strata-iq2-xs-256k-calibration-2026-10-01.md).
- `qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-500k`: 512000 tokens
  (`500k` label) with static YaRN factor 2 (`--rope-scaling yarn
  --rope-scale 2 --yarn-orig-ctx 262144`, per the official
  Qwen3.8-Flash-Next card) plus `STRATA_PREFILL_TEMP_PAUSE_C=80`. It replaced
  the retired `976k` profile by operator decision on 2026-10-02.

Q2_0 and both Coder IQ1_M profiles, their source weights and exclusive
prepared packs were removed at the operator's request on 2026-10-01.
Historical benchmark evidence is retained.

Retired: the separately tuned IQ2_XS maximum-context profile ending in `976k`
(1,000,000 tokens, YaRN factor 4) was deleted on 2026-10-02 and replaced by
the 500k profile above. Its 989136-token retrieval pass is a historical
record; see the [context report](reports/strata-iq2-xs-context-2026-10-01.md)
for measurements, latency and tool-use limitations.

The native 256k IQ2_XS profile has a separate
[calibration report](reports/strata-iq2-xs-256k-calibration-2026-10-01.md).

Historical record (checkpoint, packs and profiles retired 2026-10-03): the
uncensored `mradermacher/Qwen3.8-Flash-Next-Uncensored-i1-GGUF`
`i1-IQ4_XS` checkpoint loaded through this backend after a packer fix preserving
native IQ4_XS PLE keys. This only changed newly prepared packs; the native
inference binary and existing IQ2_XS packs were unchanged. Its short-prompt
compatibility was established, but near-30k tests were stopped by the swap
guard. The 32k profile stayed experimental, with context calibration incomplete.
Unsloth's mixed `UD-IQ4_XS` is not supported by the preparation/native
expert path. See the [compatibility report](reports/strata-iq4-xs-uncensored-2026-10-01.md).

The local uncensored **mixed IQ2_XS requant** and its 32k/256k profiles were
removed at the operator's request on 2026-10-01, reclaiming 116.79 GiB. The
[requantization report](reports/strata-uncensored-iq2-xs-requant-2026-10-01.md)
retains historical results; the base IQ2_XS weights and the shared MTP directory
(`/home/diogo/models/strata/mtp/rt`) remain in use by the canonical profiles.

Historical record (weights, pack and live profiles retired 2026-10-03):
Upstream explicitly documents OrcaRouter's published **IQ3_XXS** with manual
`iq_pack.py --compat-bf16` preparation. It requires both shards, a separate pack
and its own tokenizer; `--native` and `--ple-gguf` both use shard 1. The
[setup analysis](reports/strata-orca-iq3-xxs-profile-2026-10-01.md) adapted
this to dual 3090s with 32k, equal 24/24 placement, mmap and prefill 512.
The dual mmap arm hit the swap-growth guard; the stable 32k arm used GPU1 and
`--resident-experts` (33.14 GiB page-locked CPU complement), with 81.3 tok/s
median warm code and a successful fresh 29831-token retrieval test. Automatic
tools and tool-result continuation failed qualification. The frozen example
configs are preserved at
[reports/strata-orca-iq3-xxs-examples-2026-10-01](reports/strata-orca-iq3-xxs-examples-2026-10-01);
see also the [local calibration report](reports/strata-orca-iq3-xxs-calibration-2026-10-01.md).
