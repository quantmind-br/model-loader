# Changelog

All notable user-facing changes are documented here. This project follows
[Semantic Versioning](https://semver.org/) before and after the `1.0.0` release.

## [Unreleased]

### Added

- Reproducible Cloudflare Tunnel deployment for an externally authenticated,
  loopback-origin model-loader API. The Tunnel and Caddy gateway follow the
  local proxy listener on `127.0.0.1:4321` instead of remaining up at login.
- LM Studio backend kind (`lmstudio`): launched through the `lms` CLI + daemon
  via `backends/lms/lmstudio-serve.sh`.
- FreeToken backend kind (`freetoken`): FlashML edge-native MoE offload engine
  launched via `backends/freetoken/freetoken-serve.sh`; readiness waits on the
  service-ready marker because uvicorn's `/health` answers before weights load.
- systemd notify lifecycle for the proxy: `Type=notify` user unit signaling
  `READY=1` only after a healthy `/_status` probe, `WATCHDOG=1` while HTTP stays
  healthy, and `STOPPING=1` on shutdown; the TUI drives the unit through
  `systemctl --user` (the previous detached-process supervision remains the
  fallback when no unit is installed).

### Changed

- Local `syv-qwen38` backend re-synced to upstream HyperQwen `da8a8e9` (new
  `spec-attn-smem-fit` patch applied, `verify.sh --install` clean), and the
  eleven linked dual-RTX-3090 profiles re-tuned at 270 W/card: custom
  all-reduce is now on with `PYTORCH_CUDA_ALLOC_CONF=expandable_segments:False`
  (+4-15% decode across Qwen3.8-27B, MiMo, Ornith and Nex). The earlier
  "custom all-reduce crashes on SM86" failure was the expandable-segments
  allocator, which cannot export the CUDA-graph buffer over IPC; the backend
  wrapper now defaults the allocator off at TP>1 like the upstream launcher.
  The dead `VLLM_V2_CUDAGRAPH_MEM_MIB` was removed, MiMo gained probabilistic
  draft sampling with `QMAX 8`, and Nex gained a float16 GDN state. Report:
  `docs/reports/hyperqwen-parity-dual-rtx3090-2026-09-27.md`.
- Local dual-RTX-3090 Syv Qwen3.8-27B provisioning audited against upstream
  `0e951951`: vLLM 0.28.0 with verified patches and optional KVarN support.
  Six workstation profiles use BF16 KV, split-KV verification and seven-token
  DFlash2 lookup; symmetric checkpoints use INT8 activations only in MLP.
  The asymmetric AWQ variant retains its body and uses separately requantized
  INT8 heads. Original weights and the previous installation are retained.
  These profiles/backends are local, gitignored installation state, not bundled
  release defaults. Conclusions, measurements and limitations are written up in
  `QWEN38_27B_AUDIT_REPORT.md`, with the raw arms under
  `~/.local/state/model-loader/benchmark/qwen38-audit-20260906/`.
- Local dual-RTX-3090 Ornith-1.5 35B-A3B AutoRound W4A16 provisioning calibrated
  under single-sequence workload with 262,144 (256k) context: DFlash2 speculative
  decoding with 7 drafts and lookup enabled (`VLLM_DFLASH2_LOOKUP=1`), `max-num-batched-tokens 4096`,
  and `performance-mode balanced`. Verified via proxy with prefix caching active and
  real context prefill/decode.

### Removed

- TokenSpeed backend kind. It was added and removed before this release: its
  upstream runtime requires Hopper-or-newer CUDA and does not compile for this
  workstation's RTX 3090 GPUs (`sm_86`).

## [0.1.0] - 2026-08-02

### Added

- Five-tab terminal UI and matching headless CLI for profiles, processes,
  models, backends, and benchmarks.
- Catalog support for nine local inference backend kinds.
- OpenAI-compatible proxy with Anthropic Messages, OpenAI Responses, and Gemini
  translation plus profile-driven hot swapping.
- Process recovery, health monitoring, GPU metrics, Hugging Face downloads, and
  twelve benchmark modes.
- Public contributor, security, governance, CI, and release documentation.

[Unreleased]: https://github.com/quantmind-br/model-loader/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/quantmind-br/model-loader/releases/tag/v0.1.0
