---
name: huggingface-download
description: |
  Download Hugging Face models/datasets the fast, reliable way on this machine.
  Use when fetching weights for vLLM/SGLang/llama.cpp, pulling a GGUF quant, or any `hf download` / HF Hub fetch.
  Keywords: huggingface, hf download, gguf, safetensors, vllm, sglang, aria2c, xet, hf_transfer, model download, weights, hub.
compatibility: Linux + huggingface_hub `hf` CLI (>=1.x), optional aria2c
---

# huggingface-download

Throughput here is bound by the **per-connection CDN ceiling (~60 MiB/s single-stream)**, not by bandwidth. The only lever with upside is **adding connections** — run repos/files in parallel or use `aria2c -x16`.

## Setup (always)

Disable Xet and set short timeouts before any download:

```bash
export HF_HUB_DISABLE_XET=1 HF_HUB_DOWNLOAD_TIMEOUT=30 HF_HUB_ETAG_TIMEOUT=30
```

Xet can intermittently stall at 0 MiB/s on this link; disabling is a zero-cost safe default. Never set `HF_XET_HIGH_PERFORMANCE=1` (hangs). `HF_HUB_ENABLE_HF_TRANSFER` / `hf_transfer` is legacy since hub v1.0 — ignored, do not use.

## Core commands

Download to `/home/diogo/models/huggingface/<org>/<repo>/` so model-loader profile paths resolve.

```bash
# Whole repo (safetensors, for vLLM/SGLang) — parallelizes shards, resumable:
hf download <org/repo> --local-dir /home/diogo/models/huggingface/<org>/<repo> --max-workers 4

# Single GGUF file (tokenizer is embedded — pass the exact filename, never pull every quant):
hf download <org/repo> <file.gguf> --local-dir /home/diogo/models/huggingface/<org>/<repo>
```

`hf download` is resumable — re-running skips cached blobs.

## Going faster

Measured throughput on this link (higher = faster):

| Strategy | Throughput |
|---|---|
| `hf` whole-repo / ~3 repos in parallel | **~75 MiB/s** (aggregate) |
| `aria2c -x16` (single file) | ~59 MiB/s |
| `hf download` single stream | ~46 MiB/s |
| `aria2c -x1` (single stream) | ~38 MiB/s |

- **Whole repo or ~3 repos in parallel** is the simplest fast path — `--max-workers` spreads shards across connections.
- **Max single-file speed:** `aria2c -x16` against the resolve URL (the CDN supports range requests):

```bash
aria2c -x16 -s16 -k1M -d /home/diogo/models/huggingface/<org>/<repo> -o <file> \
  "https://huggingface.co/<org/repo>/resolve/main/<file>"
```

## Pitfalls

- Kill stalled `hf` by explicit PID — **never** `pkill -f 'hf download'` (matches your own shell).
- Measure throughput over `~/.cache/huggingface` + `--local-dir` summed; in-flight bytes live in the cache, not the dest.
- Skip `hf-mirror.com` from Brazil (China-hosted; the direct CDN already saturates the per-connection ceiling).
