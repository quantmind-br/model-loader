# Tools gate for the IQ2 256k promotion (pre-registered 2026-10-02, before any v5 tool run)

Candidate B = `qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-candidate-stage-dense-256k` on release v5
`20261002T095307Z-13018fd9fe4e` (server honors forced `tool_choice` by a forced call opening and
`parallel_tool_calls=false` by returning only the first complete call). Canonical C =
`qwen3-8-flash-next-iq2-xs-mtp-strata-dual-mmap-w15-256k` as installed (baseline server/binary), run read-only.
Both use `reasoning_effort: none` from their adjacent `.shared-settings.json`.

Each arm: one start via `strata-performance-probe.py --controls --linger 480`; during the linger the bundled
harness runs `--mode integrity --stream --continuation` and `--mode auto --promotion` (30 positive + 30 controls)
against the proxy (`tools/harness_window.py`). Guards unchanged.

B passes only if all hold:

1. Integrity: every forced trial (3 non-stream + 1 stream) `ok`, decoded command bytes exact; no HTTP errors.
2. Auto (30+30): zero transport/HTTP/decode errors; correct calls >= C's minus 2; spurious calls <= C's plus 2.
   The absolute harness SLO is reported, but the canonical profile is not agent-qualified either, so the gate is
   non-inferiority against C, not agent qualification.
3. Probe controls: exact copy, JSON text, stop Unicode, tool bytes and no-tool control pass; the tool-result
   continuation is not worse than C (a repeat where C stops is a regression).
4. No guard trip and complete requests.

A failure keeps the canonical profile on the baseline.
