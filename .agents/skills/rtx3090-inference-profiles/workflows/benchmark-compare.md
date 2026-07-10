# Benchmark & compare profiles

**Entry:** "which profile is best / how fast is it" — one profile to characterise, or two+ to rank.
**Exit:** a per-profile results table + a one-paragraph recommendation delivered; if the numbers change the winner's recorded verdict, its `description` is updated with them.

This workflow wraps `model-loader benchmark`. It measures existing, launchable profiles — it never
writes or tunes one (that is [full-tuning.md](full-tuning.md)) and never fixes one
([troubleshoot.md](troubleshoot.md)). Exact CLI flags: AGENTS.md §4.

## 0. Prep — you are about to evict the loaded model

`benchmark` loads each profile through the proxy's `/_admin/load`, so it **swaps out whatever is
currently serving**. Before starting:

1. `curl -s 127.0.0.1:4321/_status` — record `loaded_profile_id`. Restore it at the end.
2. Proxy must be up (`model-loader serve`, or the TUI's proxy). An open **TUI holds the
   single-instance flock** and blocks write-path CLI — if a command hangs on the lock, find the
   holder with `fuser ~/.local/state/model-loader/model-loader.lock` and ask the user to close it.
3. Judge-graded quality modes (`judge`, `ragas-bench`, `summary-bench`, `instruction-bench`) call an
   LLM grader → export `$QUANTMIND_API_KEY` first. `codegen-bench` runs code in a bwrap sandbox and
   `math-bench`/`mmlu-bench` grade locally — no key needed.

## 1. Pick the mode → know its headline metric

Metric mapping is fixed in code (`benchmark_metrics.go::primaryMetric`): **`solve-rate` is the
primary metric for every quality/knowledge mode**; only `llama-bench` and `longctx` differ.

| Mode (aliases) | Category | Primary metric | Answers |
|---|---|---|---|
| `llama-bench` (`llamabench`, `throughput`) | Speed | **tok/s** (+ **TTFT**) | how fast does it decode / how snappy is first token |
| `judge` | Quality | **solve-rate** | SWE-bench-Lite coding, reference-guided judge |
| `math-bench` | Quality | **solve-rate** | GSM8K arithmetic reasoning |
| `codegen-bench` | Quality | **solve-rate** | HumanEval, executed in a sandbox |
| `mmlu-bench` | Knowledge | **solve-rate** | multiple-choice general knowledge |
| `instruction-bench` | Quality | **solve-rate** | instruction-following / format / refusal |
| `ragas-bench` | Quality | **solve-rate** | RAG faithfulness / relevancy |
| `summary-bench` | Quality | **solve-rate** | summarization coherence |
| `longctx` (`long-context`) | Long-context | **recall (AvgScore)** | needle-in-haystack; KV-quant recall at depth |
| `terminal-bench`, `swe-bench-pro`, `deep-swe` | Agentic | (harness) | multi-turn agentic — **need external harnesses (`tb`, SWE-bench_Pro-os, `pier`) + Docker; name them, don't run them here** |

Rule of thumb: **"how fast" → `llama-bench`; "how good at coding" → `codegen-bench` (or `judge`);
"does KV quant hurt recall" → `longctx`.** Pick the one mode that answers the actual question, plus
`llama-bench` if speed is part of it.

## 2. Fair A/B — an unfair comparison is worse than none

- **Same ctx tier for both profiles.** Never rank a 64k profile against a 256k one — KV budget and
  placement differ. If they disagree, benchmark each at a common tier or state the caveat.
  ([SKILL.md §Context adjustment rule](../SKILL.md).)
- **Warm-measure.** The **first vLLM/SGLang request runs ~half speed** (cold torch.compile / cold
  KV pool) — discard it. The benchmark sequences many items so later ones are warm, but a
  single-shot hand check must throw away request #1. Runs that reuse a hot instance carry
  `ReusedInstance` — their tok/s may include other traffic. (Red flag: *"the first request's tok/s
  is the number"* — [SKILL.md §Red flags](../SKILL.md); warm-measure detail in
  [references/vllm-sglang.md](../references/vllm-sglang.md).)
- **The pid changes between runs — that is normal.** `benchmark` reloads the instance each time; a
  new pid is the swap mechanism, not a crash.
- **Record acceptance % for speculative profiles.** A spec profile's tok/s is meaningless without
  its acceptance rate — confirm the MTP head / drafter actually loaded from the launch log and read
  the acceptance number, then report it beside tok/s.
  ([references/speculative.md](../references/speculative.md).)
- **SNDR caveat:** short "capital of X" prompts hide the TurboQuant large-prefill trap — use
  `llama-bench` (large prefills) to characterise `-sndr` profiles, not a chat round trip.
  ([SKILL.md §Common mistakes](../SKILL.md), [references/sndr.md](../references/sndr.md).)
- Same mode **and** same `--limit` for both sides.

## 3. Run it

```
# one profile, one mode (full item set)
model-loader benchmark --profile <id> --mode <mode>

# quick pass — cap items on reducible modes (judge, math/codegen/ragas/summary/instruction/mmlu,
# llama-bench presets). longctx is a single probe and ignores --limit; agentic modes cap via
# --tb-n-tasks / --deepswe-n-tasks. 0 = full set.
model-loader benchmark --profile <id> --mode <mode> --limit 20

# CI-style gate: exit code 2 if solve-rate < threshold
model-loader benchmark --profile <id> --mode judge --min-solve 0.5

# review stored runs / cross-profile view / raw transcript
model-loader benchmark --list
model-loader benchmark --compare
model-loader benchmark --transcript <run-id>
```

To compare, run the **same `--mode` (+ same `--limit`) on each profile**, then read the
cross-profile view with `--compare` (latest run per profile, grouped by mode). Add `--json` to any
of these for machine-readable output.

**Interactive view:** TUI **Tab 5 — Benchmark** → **Dashboard** (mode-focused leaderboard, Δ vs
previous), **Compare** (`c`; `m` cycles ranking metric across primary / tok/s / TTFT / VRAM), and
**History** (`h`; one profile's runs over time). Same numbers, live.

Slow modes (`codegen-bench`/`judge` on two 35B profiles can run ~1 h) → cap with `--limit` and note
the cap in the output table so the numbers are read as a sample.

## 4. Output contract

Deliver **a per-profile table + a one-paragraph recommendation** — nothing less.

| Profile | Mode | Primary metric | tok/s | TTFT | Peak VRAM (GiB) |
|---|---|---|---|---|---|
| `ornith-aeon-…` | codegen-bench | solve 62% | 94 | 0.7 s | 22.1 |
| `ravenx-…` | codegen-bench | solve 55% | 88 | 0.8 s | 22.4 |

- **Primary metric** column carries the mode's headline (solve-rate / tok/s / recall) — the same
  four scorecards the RunDetail view shows (primary · tok/s · TTFT · VRAM).
- For speculative profiles add the **acceptance %** next to tok/s (or a footnote).
- Note `--limit` caps and the ctx tier the comparison was run at.

Then one paragraph: which profile wins **for the stated purpose**, by how much, and the trade-off
(e.g. "faster but −7 pp solve-rate"). If a profile can't be judged on the axis asked, say so.

**Persist the verdict.** If the numbers **change the recommendation recorded in the winning
profile's `description`**, update that description's first sentence + headline metrics to match —
descriptions must track measured reality (the honesty the audit checks:
[workflows/audit-profile.md](audit-profile.md); how full-tuning records them:
[workflows/full-tuning.md](full-tuning.md)). If nothing changes, leave descriptions untouched.
Benchmark only measures and reports — it does not tune or fix.
