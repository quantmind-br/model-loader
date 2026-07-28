# Model research & calibration protocol — run BEFORE writing any profile

Every new model gets this research pass **before** the profile is written. Sampling defaults,
chat template, tool-call parser and speculative assets are per-model facts that live on the web
(Hugging Face first) — never guess them from family resemblance, and never trust a GGUF's
embedded defaults. Re-run §4 (template verification) after any backend upgrade or GGUF
re-download. Facts below verified 2026-07-02; llama.cpp runtime capabilities re-checked
2026-07-21 (stable b9934 / nightly b10083). Other installed pins: beellama
v0.4.1 build b10856 (re-pinned 2026-07-27; no flag change vs b10829), vLLM 0.24.0, SGLang 0.5.9,
TabbyAPI 3cf468c, lucebox 1b11c50.

## 1. The lookup protocol (start at Hugging Face)

Check in order; higher entries override lower:

1. **Model card** `https://huggingface.co/<org>/<repo>` — the "Best Practices" / "Recommended
   Inference Parameters" / "Usage Recommendations" section. Richest source: distinguishes
   thinking vs non-thinking vs coding presets, `presence_penalty` advice, greedy-decoding
   warnings, and "modified chat template required" language (Ornith-class). Also read the
   **Community tab of the quantizer repo** (unsloth/bartowski GGUF) — earliest place template
   fixes and family pathologies surface.
2. **`generation_config.json`** — fetch raw: `https://huggingface.co/<org>/<repo>/raw/main/generation_config.json`.
   Only non-default keys matter (`temperature`, `top_p`, `top_k`, `min_p`, `repetition_penalty`);
   absent keys = "no recommendation" (Kimi-K2 and gpt-oss ship none). vLLM and SGLang apply this
   file as server defaults automatically; **llama.cpp ignores it entirely** — everything must
   become profile `args`.
3. **Unsloth docs page + unsloth GGUF mirror card** — restates official numbers with ready
   llama.cpp command lines, and is the ungated way to read gated repos' configs
   (`unsloth/Llama-3.3-70B-Instruct/raw/main/generation_config.json` works where `meta-llama/…` 401s).
4. **Web search** (`"<model> recommended temperature"`, `"<model> tool calling llama.cpp"`,
   `"<model> chat template fix"`) for pathologies newer than the card.
5. **The source-model card's "Benchmarks" / "Evaluation methodology" section** (follow `base_model:`
   links up to the original trainer repo — e.g. Ornith AEON's GGUF/base cards point to
   `deepreinforce-ai/Ornith-1.0-35B`). This section often lists the **exact temp/top_p the official
   benchmark scores were produced at**, and it is the model's *true calibrated operating point* —
   more authoritative for an agentic profile than the usage-example commands in the same card.

**⚠ Usage-example sampling ≠ calibrated operating point.** A card's ready-to-paste command lines
(often `--temp 0.6 …`) are usually the conservative **"precise / single-shot coding" preset**, and
frequently disagree with both `generation_config.json` and the benchmark methodology. The Ornith
case is canonical: its GGUF + base cards' examples all say `temp 0.6 / top_p 0.95` (= the ClawEval
preset), but `generation_config.json` ships `temperature 1.0` and the headline agentic-coding
scores (Terminal-Bench 2.1, SWE-Bench Verified/Pro, SWE-Atlas) were all measured at **temp 1.0,
top_p 0.95–1.0**. For an *agentic* profile, prefer the benchmark/gen-config temp; reserve the
usage-example temp for single-shot code completion. Card > **benchmark methodology** > config >
unsloth > community, where "card" now means the *source-model* card (deepest `base_model` link).

**Also inventory speculative + template assets while you're on HF** (see references/speculative.md):
- MTP/nextn head: does the GGUF/quant actually ship the tensors, or is it a separate `mtp-*`
  sidecar? Record the exact repo and filename/prefix; config presence is not proof of loading.
- DFlash drafter: search `<family> DFlash` and record whether the GGUF is named `dflash-*` and uses
  upstream schema (Anbeeld/*-DFlash-GGUF, Lucebox/*, z-lab/*, turboderp/*-DFlash-exl3).
- EAGLE-3 head: search `<family> eagle3` / SpecForge and record whether a pre-converted
  `eagle3-*` GGUF exists; official heads favor vanilla Qwen/Llama, not merges.
- Delivery decision: separate HF sidecar repo via `spec-draft-hf` only on llama.cpp nightly b10083;
  stable b9934 uses a downloaded local GGUF in `spec-draft-model`. Main `model` stays local.
- Official template: repo's `chat_template.jinja` or `tokenizer_config.json` `chat_template` key.

## 2. Official sampling presets per family (seed table — fetched 2026-07-02, RE-VERIFY on HF)

| Family | Mode | temp | top_p | top_k | min_p | other | Src |
|---|---|---|---|---|---|---|---|
| Qwen3 hybrid (Qwen3-32B) | thinking | 0.6 | 0.95 | 20 | 0 | presence 0–2 vs loops | card |
| | non-thinking | 0.7 | 0.8 | 20 | 0 | | card |
| Qwen3-Coder | instruct | 0.7 | 0.8 | 20 | — | rep_pen 1.05 | gc+card |
| **Qwen3.6** (27B, 35B-A3B) | thinking general | 1.0 | 0.95 | 20 | 0 | | gc+card |
| | thinking **precise coding** | **0.6** | 0.95 | 20 | 0 | | card |
| | instruct (no-think) | 0.7 | 0.8 | 20 | 0 | presence 1.5 | card |
| **Ornith** (Qwen3.6-35B-A3B agentic-coding finetune) | agentic-coding | **1.0** | 0.95 | 20 | 0 | gen_config=1.0; +top-nsigma 1.0 holds MTP accept | src card §Benchmarks |
| | single-shot coding (ClawEval) | 0.6 | 0.95 | 20 | 0 | the card's usage-example preset | card examples |
| Gemma 3 / Gemma 4 it | all | 1.0 | 0.95 | **64** | 0 | do NOT lower temp for code (0.8→0.3 degrades) | gc+unsloth |
| GLM-4.7-Flash (30B-A3B) | general | 1.0 | 0.95 | — | — | | card |
| | terminal/code | 0.7 | 1.0 | — | — | | card |
| GLM-4.6 / GLM-5.2 | coding | 1.0 | 0.95–1.0 | 40 (4.6) | — | | card |
| DeepSeek-R1/R1-0528 | thinking | 0.6 (0.5–0.7) | 0.95 | — | — | never greedy | card |
| DeepSeek-V3.2 / V4 | hybrid | 1.0 | 0.95–1.0 | — | — | | card |
| Llama 3.3 / Llama 4 | instruct | 0.6 | 0.9 | — | — | | gc |
| Devstral-Small-2507 | agentic coding | **0.15** | — | — | — | | card |
| Kimi K2-Instruct | instruct | 0.6 | — | — | — | | card |
| Kimi K2-Thinking / K2.7-Code | thinking | 1.0 | 0.95 | — | — | | card |
| MiniMax-M2 | thinking | 1.0 | 0.95 | 40 | — | | card |

Pattern: 2025 models wanted 0.6–0.7; the 2026 generation (Qwen3.6, GLM-4.7+, DeepSeek V3.2+,
Kimi thinking, Gemma 4) converged on **temp 1.0 + top_p 0.95** — trained-in entropy calibration.
Lowering temp on these *degrades* output; Qwen3.6 reserves 0.6 for "precise coding" only.

## 3. Coding-agent calibration rules

- **Never temp 0 / greedy.** Official warnings (Qwen3: "DO NOT use greedy decoding … endless
  repetitions"; DeepSeek-R1: 0.5–0.7 "to prevent endless repetitions") + this rig's measured
  omp-client loops. Tool-call *format* correctness comes from grammars/parsers, not low temp.
- **Thinking models keep their family temp** (0.6 R1-class, 1.0 Qwen3.6/GLM/K2-class). For a
  coding profile on a family with a documented "precise" preset AND no calibrated-temp recovery,
  use it (Qwen3.6 single-shot → 0.6). But see the spec rule below — `top-n-sigma 1.0` lets you run
  the *calibrated* temp on a speculative profile without paying the acceptance cost.
- **Speculative profiles: run the model's CALIBRATED temp, not the low end — when `top-n-sigma 1.0`
  is available.** MTP/DFlash acceptance normally falls as sampling entropy rises (dflash_server has
  an argmax fast path at temp 0 and switches to CPU sampling under logit processing), which is why
  the old rule was "Qwen3.6 + MTP → 0.6". **Measured 2026-07-02 on Ornith AEON 35B-A3B (MTP, temp
  1.0 vs 0.6): adding `--top-n-sigma 1.0` held MTP acceptance flat — 0.62 @ temp 1.0 vs 0.63 @ temp
  0.6 (no regression).** `top-n-sigma` keeps the high-temp distribution on-distribution, which is
  exactly what stops the entropy-driven acceptance collapse. So: on **llama.cpp/beellama**, a
  2026-gen agentic-coding model trained/eval'd at temp 1.0 (Qwen3.6, Ornith, GLM-4.7+) runs at
  **temp 1.0 + `top-n-sigma 1.0`**, not artificially lowered to 0.6 — you get the calibrated
  operating point at zero speed cost. Reserve temp 0.6 for (a) single-shot code completion, or
  (b) speculative profiles on engines WITHOUT `top-n-sigma` (vLLM/SGLang/dflash) where the
  acceptance-vs-entropy trade-off still bites.
- **Never in a coding profile:** XTC (`xtc-probability` stays 0 — it deliberately suppresses the
  top token; PR thread documents logic errors), mirostat (obsolete, bypasses top-k/top-p),
  typical-p. `--top-n-sigma 1.0` (llama.cpp-only) is the one useful extra for temp-1.0 thinking
  models — and on speculative profiles it's the lever that makes temp 1.0 free (above).
- **min-p**: cheap tail-cutter, near-inert at temp ≤0.7. llama.cpp default is 0.05 — set the
  model's official value explicitly (Qwen: 0); don't treat it as a quality lever.

### Anti-loop policy (standing user decision, 2026-07-02)

- **Baseline in every llama-family profile: `repeat-penalty 1.05`, `repeat-last-n 256`.** That
  plus the family temp is the whole anti-loop stack.
- **DRY is banned from profiles by user decision** (all 13 profiles that had it were stripped
  2026-07-02). Do NOT re-add `dry-*` args unless the user explicitly asks in the current
  conversation. If they do ask (e.g. a model still collapses into "deep deep deep…"):
  `dry-multiplier 0.5, dry-base 1.75, dry-allowed-length 4` — **allowed-length ≥4 always**;
  aggressive DRY (`allowed-length ≤2`) corrodes legitimate repeated shell/code tokens
  (`head -5 … head -5`, repeated flags/paths) and reintroduces tool-call corruption (measured).
  If a model loops without DRY, first lower temp (~0.7) or use beellama's loop guard (below).
- **vLLM / SGLang / dflash_server have no DRY and no launch-side sampling flags** — anti-loop
  there is per-request only: `frequency_penalty`/`presence_penalty` (OpenAI-standard, any client)
  or the family's official `presence_penalty` via `--override-generation-config` (vLLM).
- **beellama deterministic alternative:** `reasoning-loop-guard force-close` (+ v0.4.0 defaults:
  min-tokens 512, window 1024, max-period 128) kills periodic loops inside think blocks without
  touching sampling — safe for tool calls, per-request overridable.

### Who actually controls sampling at runtime (verified in proxy + engine sources)

- The proxy's **Anthropic `/v1/messages` route forwards only `temperature`, `top_p`, `top_k`**.
  Claude Code always sends its own `temperature` (~1.0) → it **overrides the profile temp on
  every backend** (user > server-default precedence everywhere).
- Consequence: for Anthropic-API agents, the reliable levers are the fields clients never send —
  `repeat-penalty`, `min-p`, `top-nsigma` (llama.cpp family launch flags always apply), and
  server-side presets (TabbyAPI `sampler_overrides/*.yml`). On vLLM/SGLang, a client-sent temp
  wins over `generation_config.json`; plan accordingly.
- OpenAI-API clients CAN pass `min_p`/`top_k`/`repetition_penalty` per-request via `extra_body`
  on vLLM/SGLang/llama.cpp (llama.cpp additionally accepts `dry_*`, `top_n_sigma`).

### Where defaults live, per backend

| Backend | Server-side defaults | Notes |
|---|---|---|
| llama.cpp / beellama / buun | **profile `args` only** (flags) | ignores generation_config.json; llama.cpp own defaults: temp 0.8, top-k 40, top-p 0.95, min-p 0.05, repeat-penalty OFF — always set the model's numbers explicitly |
| vLLM 0.24 | `generation-config auto` (default) reads the repo's json; `--override-generation-config '{"temperature":0.6}'` to adjust | request values win |
| SGLang 0.5.9 | `sampling-defaults model` (default) reads repo json; `--preferred-sampling-params '<json>'` | request values win |
| tabby | `sampler_overrides/*.yml` preset (the only backend with first-class server presets; supports dry_*, xtc, dynatemp) | |
| lucebox dflash | none — per-request only (temp/top_p/top_k/rep_pen[window 256]/freq/presence) | temp 0 = argmax fast path |

## 4. Chat template & tool-calling verification (mandatory for agent profiles)

Background (introduced by b9847 and still current on b9934/b10083): `--jinja` is **default-on**;
the **autoparser** derives the reasoning/tool-call parser + GBNF grammar *from the template itself*
(log: `Chat format: peg-native`). A wrong/stale template silently mis-generates the output parser.
`--reasoning-format` values: `none|auto|deepseek|deepseek-legacy` (auto = deepseek = extract to
`reasoning_content`, the default; the proxy maps that to Anthropic thinking blocks). Thinking
toggle: `--reasoning on|off|auto`; setting `enable_thinking` through chat-template kwargs is
deprecated. Escape hatch: `--skip-chat-parsing` (raw content, client parses).

Checklist for a NEW model profile (steps 1–3 pre-launch, 4–8 post-launch):

1. **Diff the template.** Extract the GGUF's embedded template —
   `python -c "from gguf import GGUFReader; print(GGUFReader('m.gguf').fields['tokenizer.chat_template'].contents())"`
   (gguf-py is vendored in `backends/llama.cpp-stable/gguf-py`) — and diff against the HF repo's
   current `chat_template.jinja`. Drift in think tags, tool markers or stop tokens → save the
   official template locally → `--chat-template-file /abs/path.jinja` in `args` (the flag is
   present in the live llama.cpp-stable/beellama schemas at
   `~/.config/model-loader/backends/schemas/`; `extraArgs` is only the raw passthrough for flags a
   schema omits, never a workaround for enum-blocked values — nothing valid is enum-blocked). Check
   the card for "modified template" requirements —
   rig-proven case: Ornith/AEON needs `deepreinforce-ai/Ornith-1.0-397B/chat_template.jinja`;
   the stock-Qwen GGUF template corrupted Claude Code Bash calls (spaces inside paths, `&&`→`and`)
   even at temp 0. llama.cpp also ships curated fixes in `models/templates/` (e.g. DeepSeek-R1)
   and `scripts/get_chat_template.py <repo>` to fetch one.
2. **Static analysis:** `llama-debug-template-parser template.jinja` — confirm detected reasoning
   markers + tool format (JSON_NATIVE / TAG_WITH_JSON / TAG_WITH_TAGGED) and a non-empty grammar.
   On a running server `curl :PORT/props | jq .chat_template_caps`. `Chat format: Generic` in the
   launch log = no tool syntax recognized (weak, token-hungry).
3. **Render test:** `POST /apply-template` with system + user + assistant-with-tool_calls +
   `role:"tool"` turns. Must not `raise_exception` (Gemma 1/2 reject system roles), and thinking
   models must end the generation prompt with `<think>\n` — a missing newline alone broke
   `reasoning_content` extraction here (Nex-N2-mini).
4. **Shell-hostile tool-call round trip** (the decisive test, through the proxy at temp 0 —
   temporarily, for the test only):
   ask the model to call a `bash` tool with EXACTLY
   `mkdir -p "/tmp/My Project/sub dir" && echo "a && b > c" > "/tmp/My Project/sub dir/x y.txt" 2>&1`.
   Pass: `finish_reason:"tool_calls"`; `arguments` is a JSON **string**; command survives
   byte-for-byte (no spaces injected in paths, `&&` intact, `2>&1` intact); nothing leaked into
   `content`; no `<think>` fragments in `content`.
5. **Tool-result continuation:** append the tool result, confirm a sane next turn (no immediate
   EOS, no re-issuing the same call in a loop) — repeat once with ~50k tokens of filler.
6. **Reasoning accounting** (thinking models): non-stream request → `reasoning_content` non-empty
   AND `content` non-empty AND no `</think>` in `content`; compare `usage.completion_tokens` vs
   what surfaced — a large deficit means the parser is eating tokens. **vLLM trap:** its
   `qwen3`/`deepseek_r1` reasoning parsers silently dropped 1224/1692 tokens here — run vLLM
   reasoning profiles with NO `--reasoning-parser` until this test passes on the current build
   (tags stay in `content`; the proxy/client parses them). Repeat with `stream:true`.
7. **Stop behavior:** plain completion ends `finish_reason:"stop"` with no trailing
   `<|im_end|>`/`<end_of_turn>` in content; check launch log for EOS/EOG warnings; fix GGUF
   metadata via `gguf_set_metadata.py` / `--override-kv tokenizer.ggml.eos_token_id=int:N`.
8. **Re-test with the real sampling config** — over-aggressive anti-repetition mimics template
   corruption (step 4 at the profile's temp).

Known llama.cpp traps (open issues, 2026-07): Qwen3-**Instruct**-2507 GGUFs misdetected as
thinking → tool calls land in `reasoning_content` (fix: `--reasoning off`) (#20809); tools with
many optional params loop at long ctx (#20164); `arguments` sometimes returned as object not
string (#20198). **KV `q4_0` degrades tool-calling** (official docs caution + measured here) —
tool-call profiles use KV q8_0.

### Tool/reasoning parser quick matrix (set in profile args)

| Model family | vLLM 0.24 `tool-call-parser` | SGLang 0.5.9 `tool-call-parser` | tabby `tool-format` |
|---|---|---|---|
| Qwen2.5 / Qwen3 / Qwen3.5 (non-coder) | `hermes` (there is NO `qwen` parser in vLLM) | `qwen` | — (no Hermes-JSON parser: tool calls unparsed!) |
| Qwen3-Coder / Qwen3.5+ coder XML | `qwen3_xml` (or `qwen3_coder`) | `qwen3_coder` | `qwen3_coder` (alias `qwen3_5`) |
| GLM-4.6 / 4.7 | `glm45` / `glm47` | `glm` / `glm47` | `glm4_6` |
| DeepSeek V3.x | `deepseek_v3/v31/v32` (+ example chat template) | `deepseekv3/v31/v32` | — |
| Kimi K2 | `kimi_k2` | `kimi_k2` | — |
| Gemma 4 | `gemma4` | — | `gemma4` |
| Ornith (Qwen3.5 base) | `qwen3_xml` + reasoning `qwen3` (per card) | `qwen3_coder` | — |

vLLM tool parsing needs `--enable-auto-tool-choice` too. SGLang reasoning: `qwen3` (self-closing)
vs `qwen3-thinking` (forced-open templates). Blank tabby `tool-format` = tool calls never parsed
(silent failure — watch the tabby log). Schema note (S1 fixed): the curated sglang
`tool-call-parser`/`reasoning-parser` enums were widened to the installed sglang 0.5.9
detector-map union (tool-call: 30 values incl. `qwen3_coder`/`glm47`; reasoning: 23 values incl.
`qwen3-thinking`), so any of those parser values now **validates directly in a profile's `args`**
— set it there, no workaround. Source: the live schema
`~/.config/model-loader/backends/schemas/sglang-stable.json` (widened per BUGS.md S2 in
`internal/service/backendschema/curated_sglang.go`), which mirrors the installed
`backends/sglang-stable/.venv/.../sglang/srt/parser/reasoning_parser.py` +
`.../function_call/function_call_parser.py` `DetectorMap`.

## 5. Ready presets (this library's common cases)

- **Qwen3.6 thinking, coding agent (llama.cpp/beellama) — AGENTIC default:**
  `temperature 1.0, top-p 0.95, top-k 20, min-p 0, top-n-sigma 1.0, repeat-penalty 1.05,
  repeat-last-n 256` (the calibrated gen_config temp; `top-n-sigma 1.0` holds MTP/DFlash
  acceptance flat — measured). For **single-shot** code completion where you want the "precise"
  preset: `temperature 0.6` (drop `top-n-sigma`). (+ beellama: `reasoning-loop-guard force-close`.)
- **Ornith AEON 35B-A3B (agentic-coding, llama.cpp, tensor-split 2x3090):** same as Qwen3.6
  agentic above - temp 1.0 / top-p 0.95 / top-k 20 / min-p 0 / top-n-sigma 1.0 / repeat-penalty
  1.05 / repeat-last-n 256. Source: `deepreinforce-ai/Ornith-1.0-35B` benchmark methodology
  (Terminal-Bench 2.1, SWE-Bench Verified/Pro, SWE-Atlas all at temp 1.0) + gen_config=1.0; the
  card's temp-0.6 examples are the ClawEval/single-shot preset. Official Ornith chat template +
  `--reasoning-format deepseek` mandatory (stock-Qwen GGUF template corrupts Claude Code Bash
  calls). Profile `ornith-aeon-35b-a3b-q4km-mtp-vision-tensor-256k` is the anchor.
- **Qwen3.6 instruct (no-think):** `temperature 0.7, top-p 0.8, top-k 20, min-p 0,
  presence-penalty 1.5` + `--reasoning off`.
- **Gemma 4 (llama.cpp):** `temperature 1.0, top-k 64, top-p 0.95, min-p 0` — never lower temp for code.
- **vLLM profiles:** rely on `generation-config auto`; coding bias → add
  `"override-generation-config": "{\"temperature\":0.6}"`. Anti-loop: client-side penalties.
- **dflash profiles:** sampling is per-request; document in the profile description that clients
  should send `temperature 0.6, top_p 0.95, top_k 20` (+ `frequency_penalty 0.2–0.5` if looping).
- **tabby profiles:** create `sampler_overrides/<profile>.yml` with the family preset — the only
  backend where server presets survive any client.
