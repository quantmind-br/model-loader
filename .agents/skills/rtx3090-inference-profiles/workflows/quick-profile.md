# Quick profile — the fast path

**Entry:** "just run this model" / a new profile, fast. **Exit:** a profile that
`validate`s, `instance start`s healthy, and answers one proxy chat round trip; its description
ends `calibration pending — run full-tuning to promote`.

This is the *fast* path: sensible defaults, no benchmark, no calibration loop, no deep web
research. Everything here leans on `SKILL.md` (rig, placement, hard rules, backend dispatch, quant,
context) and `references/model-research.md` — link, don't re-derive. When the user wants measured
tok/s / max performance, use `workflows/full-tuning.md` instead.

## 1. Ask at most three things

Ask only these, then proceed — do **not** interrogate:

1. **Model** — GGUF/dir path, or HF `repo` (+ `file` for GGUF).
2. **Target context** — a band, not a contract (see step 4).
3. **Purpose** — `agent` | `chat` | `batch`.

Purpose steers three things and nothing else here: `agent` → the family's *agentic* sampling preset
and KV `q8_0` are mandatory (tool-calling corrupts on `q4_0`); `chat` → the family's *general*
preset; `batch`/high-concurrency → prefer a capacity backend (vLLM/SGLang) per
`SKILL.md §Backend dispatch`, anti-loop via client-side penalties.

## 2. Sampling preset (seed table only)

Pull the family's row from **`references/model-research.md §2`** (the seed table) and take its
`temp / top_p / top_k / min_p` for the matching mode (agentic vs precise-coding vs instruct).
Do **not** run the deep HF/web pass or the §4 template verification — that is full-tuning's job.

- Family **absent** from the seed table → do **only** the `references/model-research.md §1` lookup,
  and only for **sampling** (model card → `generation_config.json` → unsloth). Nothing else.
- llama.cpp ignores `generation_config.json` — sampling MUST land in `args` (its built-in defaults
  are wrong for most models). vLLM/SGLang read it automatically via `generation-config auto`.

## 3. Backend and placement

- **Backend** → `SKILL.md §Backend dispatch`. Honor an explicit backend after the format check
  (GGUF → llama-family; safetensors/AWQ → vllm; EXL3 → tabby; …). Never invent one.
- **Placement** → `SKILL.md §Placement decision`. Quick-profile handles **only step 1**:
  weights + target-ctx KV fit ≤23 GiB → **single-GPU, pinned to GPU1**. Anything that needs a split
  is a real decision → escalate (edge case below), do not guess a split here.

## 4. Conservative defaults

Quant tier for the size → `SKILL.md §Quant choice` (single-card column, since we pin one GPU).
Context = the user's target, kept inside the `SKILL.md §Context adjustment rule` band and not
oversized past what the tier fits on one card.

**llama-family (llama.cpp / beellama / buun / ik-llama-cpp):**
- `cache-type-k=q8_0`, `cache-type-v=q8_0` — symmetric, never `q4_0` (tool-calling + `#20866`).
- Anti-loop: `repeat-penalty=1.05`, `repeat-last-n=256`. **No `dry-*`** (banned by user policy).
- Sampling from step 2 in `args` (llama.cpp own defaults are wrong).
- `ctx-size=<target>`.
- Single-GPU pin in `launch.env`: `CUDA_DEVICE_ORDER=PCI_BUS_ID` + `CUDA_VISIBLE_DEVICES=1`
  (both cards visible → llama.cpp auto-splits; the pin prevents it).
- **ik-llama-cpp only:** agent prompt caching is `cache-ram` (raise it, e.g. `16384`), NOT `cache-reuse` (which does not exist on ik); split is `layer`/`graph` only (no `tensor`/`row`); MLA models set `mla-use`; flags outside the hand-curated set go in `extraArgs`. See `references/ik-llama.md`.

**vLLM / SGLang:**
- `served-model-name=<profile-id>` — the proxy forwards `model=<profile-id>`; without it vLLM 404s.
- `max-model-len=<target ctx>`; rely on `generation-config auto` for sampling; anti-loop is
  client-side only. Same GPU1 pin in `launch.env`. Leave KV default (fp8 KV is a separate,
  measured decision — not for the quick path).

Follow `SKILL.md §Hard rules`: never set `"port"`; unknown args are a hard ERROR; a binary-real but
schema-absent flag goes in `extraArgs`. Name the profile per `AGENTS.md §5` — ctx label = binary-k
floored from `ctx-size`/`max-model-len`; a capability segment (`vision`/`mtp`/`dflash`) only if the
matching `args` is actually set.

## 5. Create → validate → start → one round trip

```sh
# create (llama-family example; adjust args/env per step 4)
model-loader profile create \
  --id <profile-id> --name <profile-id> --backend <backend-id> \
  --model <model-path-or-repo> \
  --description "…what+quant+ctx+placement… — calibration pending — run full-tuning to promote" \
  --arg ctx-size=65536 --arg cache-type-k=q8_0 --arg cache-type-v=q8_0 \
  --arg temperature=1.0 --arg top-p=0.95 --arg top-k=20 --arg min-p=0 \
  --arg repeat-penalty=1.05 --arg repeat-last-n=256 \
  --env CUDA_DEVICE_ORDER=PCI_BUS_ID --env CUDA_VISIBLE_DEVICES=1

model-loader profile validate <profile-id>     # must exit 0 (0=ok, 2=blocking errors)
model-loader instance start <profile-id>        # loads via the proxy, waits for /health
```

`instance start` swaps the single-active proxy model (it evicts whatever was loaded). If you must
restore it after, `curl -s 127.0.0.1:4321/_status` first to record the loaded profile.

**One proxy chat round trip** — the only verification the quick path runs (coherence, not calibration):

```sh
curl -s http://127.0.0.1:4321/v1/chat/completions \
  -H 'content-type: application/json' \
  -d '{"model":"<profile-id>","messages":[{"role":"user","content":"In one sentence, what is a compiler?"}],"max_tokens":128}'
```

Pass = a coherent, on-topic completion. No benchmark, no calibration loop, no shell-hostile
tool-call test (that is done when the profile is promoted via `workflows/full-tuning.md` — the
decisive round trip is `references/model-research.md §4`).

## 6. Record

The description MUST end with **`calibration pending — run full-tuning to promote`** — this is the
promotion hook and the honest signal that no tok/s / VRAM / agent-readiness numbers were measured.
State the placement, quant, KV types, and ctx; do not claim any measured figure.

## Edge cases

- **Model file missing** → offer the fetch and STOP until it lands. GGUF:
  `model-loader model download <repo> <file> --wait`. safetensors/dir:
  `hf download <repo>` (see `skill://huggingface-download`). Do not create the profile against a
  path that does not exist (`os.Stat` fails; llama-family rejects a bare HF repo id).
- **Validation enum error** → re-read the live schema
  `~/.config/model-loader/backends/schemas/<backend-id>.json` and use a real value. Never invent a
  flag or an enum member. (Chained spec / sglang parsers are **not** blocked — `spec-type` is a
  list-valued enum and the sglang parser enums match the installed detector maps; they validate in
  `args`. See `SKILL.md §Common mistakes`.)
- **Doesn't fit ≤23 GiB on one card** → this is a real placement decision, not a quick default.
  Escalate to `workflows/full-tuning.md` (which owns `SKILL.md §Placement decision` steps 2–5:
  pin-per-GPU, layer/TP capacity split, or Spark). Do not guess a `tensor-split` here.
