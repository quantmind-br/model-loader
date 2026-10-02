# Strata IQ2_XS: managed backend and maximum documented context

The Strata fork is registered as `strata-fork` (kind `strata`) through
`/usr/bin/python /home/diogo/dev/Strata/serve/server.py`. The installed Model
Loader supports its schema, argument builder, Python logging and context
metadata. Its native engine is 0.1.30, sm_86, commit `97cb786`; the working
tree adds managed `--model` verification and `--max-context` override.

Pinned profile:
`qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-976k`.

Configured context is **1,000,000 tokens** (`976k` is the floored binary-k
name). This reaches the base model card's documented extended limit, not an
unbounded hardware maximum. YaRN uses factor 4 and original context 262144.
The original 32k profile remains available. No new weights were downloaded.

## Configuration

- Dual RTX 3090 24 GiB, 270 W/card, driver 610.57.04, no NVLink; Ryzen
  9950X3D, 60 GiB RAM. Sequential layer split: 24/24.
- Prepared native IQ2_XS pack, mmap experts, int8 KV, 15 pool workers;
  expert cache/prefill auto, 1536 MiB VRAM reserve. Auto prefill chose 8192.
- At 1M, 15506/24576 experts are GPU resident: 8523 on GPU0, 6983 on GPU1.
  The larger context reduces expert-cache capacity compared to the 32k baseline.
- Q2_0 MTP head, spec 4, spec-min-p 0.7. Startup logs and response draft
  counters confirm execution.
- Official Qwen template revision `de4b8e4d43b917e7706784d8bb445c9af86a3540`.
  Non-thinking defaults: temperature .7, top_p .8, top_k 20, min_p 0,
  presence penalty 1.5, repetition penalty 1; speed projection disabled.
- Durable engine JSON and shared reasoning settings:
  `~/.config/model-loader/strata/`; tokenizers: `~/models/strata/tokenizers/`.
  Native pack/MTP settings remain in engine JSON. Profile `max-context`
  overrides the engine context; `/v1/models` advertises 1000000.

## Measurements

All requests went through Model Loader's loopback proxy. Fresh system prefixes
prevented long-prompt cache reuse. Per-GPU telemetry was sampled every 0.5 s.

| Workload | Input tokens | TTFT | Decode tok/s | Peak GPU0/1 GiB | Result |
|---|---:|---:|---:|---|---|
| Warm short code, 256 output tokens | 54 | 0.334 s | 126.5 | 22.34 / 22.42 | completed |
| Long reference document | 249673 | 112.68 s | 83.5 | 22.40 / 22.48 | 3/3 records recovered |
| Near-full reference document | 989136 | 744.86 s | 57.0 | 22.58 / 22.48 | 3/3 records recovered |
| Model Loader llama-bench, 5% preset, 3 repetitions | 47572 | 0.142 s cached | 131.6 ± 1.8 | aggregate single-card peak 23089 MiB | passed |

The first short code sample was cold: 99.0 tok/s; it is excluded from the warm
number. Retrieval used temperature 0; normal serving retains the defaults
above. The near-full output fenced its JSON, so exact record recovery passed
but strict JSON-only formatting did not. These retrieval results are smoke
tests, not comprehensive quality evaluation across a million tokens.

MTP acceptance: 179/193 (92.7%) in warm short code; 53/71 (74.6%) near full.
A four-paragraph Portuguese explanation also completed coherently. An
oversized generation budget on the near-full prompt returned HTTP 400.
No OOM or kernel Xid was observed. Core temperatures peaked at 85/74 °C;
available RAM stayed above 40 GiB. The driver did not expose VRAM-junction
temperature, so it was not measured.

The native benchmark reused the loaded instance. Its 142 ms TTFT uses cached
prefill and must not be compared with the fresh-document timings above.
Unlike a prior 32k benchmark aggregate, this run recorded 256 completion
tokens correctly. Run ID:
`qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-976k-1790846161514253888`.

## Tools and limitations

The bundled strict harness passed 4/4 integrity cases including SSE and one
tool-result continuation. Autonomous selection: 10/10 positive cases, with
1/10 spurious calls in no-tool controls; statistical verdict **inconclusive**.
The earlier 32k configuration repeatedly called the tool again after success.
A clean continuation in this one 1M-configured run does not establish a fix.
Tool tests used short inputs; tool behavior at filled 1M context remains
unvalidated. The profile is not certified for coding-agent use.

The 32k Q2_0, IQ2_XS, dual-GPU Coder IQ1_M and GPU1-only Coder profiles were
also promoted from the isolated benchmark catalog to the normal catalog.
Only IQ2_XS was launched during this integration; the other three retain their
prior measured configurations and passed profile validation.

## Reproduction and evidence

```sh
model-loader instance start qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-976k
model-loader benchmark run \
  --profile qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-976k \
  --mode llama-bench --limit 1
```

Full local evidence: `~/models/strata/context-20261001/` contains raw
request/SSE/telemetry JSONL, per-context checks, budget check, tool probe,
native benchmark output, engine config, reproduction scripts and artifact
hashes. The code integration passed `make build`, `go test ./...`, `go vet
./...`, and 58 Python managed-config/server tests. Existing schemas and the
default backend were preserved; the original catalog/binary backup is under
`~/.local/state/model-loader/backups/strata-integration-20261001-055625/`.

Sources: [base model card](https://huggingface.co/Qwen/Qwen3.8-Flash-Next),
[quantized checkpoint](https://huggingface.co/ISTA-DASLab/Qwen3.8-Flash-Next-GSQ-RCO-GGUF),
[Strata fork](https://github.com/quantmind-br/Strata), local source and logs.
