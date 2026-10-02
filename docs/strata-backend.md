# Strata fork

The `strata` kind launches the fork's Python HTTP server, which owns the native
CUDA engine. Register the server, not the native `strata` stdin executable:

```sh
model-loader backend add strata-fork --kind strata \
  --executable '/usr/bin/python /home/diogo/dev/model-loader/backends/strata-fork/serve/server.py'
```

Requires [quantmind-br/Strata](https://github.com/quantmind-br/Strata), branch
`fix/linux-pinning-request-race`, with the managed `--model` and `--max-context`
server options. Check `serve/server.py --help` before using another revision.
The native engine tested here is 0.1.30, built for sm_86 at commit `97cb786`.
Model Loader does not build Strata, download weights or prepare expert packs.

The performance implementation uses immutable candidate releases under
`backends/strata-fork/releases/`. `backend-build.sh` performs a clean CUDA SM86
build and records hashes, toolchain and test provenance in `BUILD.json`; it does
not replace an active executable. Register a candidate's `serve/server.py` in a
separate catalog entry, and point its prepared config at that release's native
executable. Keep the old server and native binary together for rollback.

Candidate API behavior is stricter: stop sequences are applied incrementally,
seed zero is preserved, and unsupported constrained controls return HTTP 400
before streaming. Forced/named tool selection, `tool_choice=none` with tools,
`parallel_tool_calls=false` with tools, strict tools and constrained JSON formats
are not implemented. Automatic tool calls are buffered until their arguments
can be checked; primitive type checks do not provide full JSON Schema guarantees.
These candidates are not agent-qualified or promoted to every existing profile.
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

Set `launch.backendId` to `strata-fork`. The manager adds `--engine strata`,
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

Local IQ2_XS profiles use both cards, mmap experts, 15 workers, int8 KV,
MTP spec 4, `spec-min-p=0.7` and a 270 W cap per RTX 3090. Q2_0 and both
Coder IQ1_M profiles, their source weights and exclusive prepared packs were
removed at the operator's request on 2026-10-01. Historical benchmark evidence
is retained. The prepared Q2_0 MTP is a shared dependency of IQ2_XS and IQ4_XS
and remains installed.

The separately tuned IQ2_XS maximum-context profile ends in `976k` and uses
1,000,000 tokens with YaRN factor 4. It passed a 989136-token retrieval test;
see the [context report](reports/strata-iq2-xs-context-2026-10-01.md) for
measurements, latency and tool-use limitations.

The native 256k IQ2_XS profile has a separate
[calibration report](reports/strata-iq2-xs-256k-calibration-2026-10-01.md).

The uncensored `mradermacher/Qwen3.8-Flash-Next-Uncensored-i1-GGUF`
`i1-IQ4_XS` checkpoint loads through this backend after a packer fix preserving
native IQ4_XS PLE keys. This only changes newly prepared packs; the native
inference binary and existing IQ2_XS packs are unchanged. Its short-prompt
compatibility is established, but near-30k tests were stopped by the swap
guard. The 32k profile remains experimental, with context calibration incomplete.
Unsloth's mixed `UD-IQ4_XS` is not supported by the current preparation/native
expert path. See the [compatibility report](reports/strata-iq4-xs-uncensored-2026-10-01.md).

The local uncensored **mixed IQ2_XS requant** and its 32k/256k profiles were
removed at the operator's request on 2026-10-01, reclaiming 116.79 GiB. The
[requantization report](reports/strata-uncensored-iq2-xs-requant-2026-10-01.md)
retains historical results; the base IQ2_XS, source IQ4_XS and shared MTP remain.

Upstream explicitly documents OrcaRouter's published **IQ3_XXS** with manual
`iq_pack.py --compat-bf16` preparation. It requires both shards, a separate pack
and its own tokenizer; `--native` and `--ple-gguf` both use shard 1. The
[setup analysis and profile examples](reports/strata-orca-iq3-xxs-profile-2026-10-01.md)
adapt this to dual 3090s with 32k, equal 24/24 placement, mmap and prefill 512.
The model is now installed. The dual mmap arm hit the swap-growth guard;
the stable 32k arm uses GPU1 and `--resident-experts` (33.14 GiB page-locked
CPU complement), with 81.3 tok/s median warm code and a successful fresh
29831-token retrieval test. Automatic tools and tool-result continuation failed
qualification. See the [local calibration report](reports/strata-orca-iq3-xxs-calibration-2026-10-01.md).
