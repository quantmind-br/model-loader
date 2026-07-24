# Self-improvement policy (rtx3090-inference-profiles)

Controlled learning for this skill: task-local self-healing, quarantined learning
candidates, baseline comparison, explicit approval, transactional dual-copy
promotion, post-apply verification, and conflict-aware rollback. The portable
control plane is bundled at `scripts/self_improvement_lib/core.py`; the explicit
L1 entrypoint is `scripts/self_improvement.py`. No sibling directory is required;
the skill remains useful at L0 without Python or runtime hooks.

## Improvement contract

- **Governed task.** Create, tune, repair, audit, and compare model-loader
  schemaVersion-3 inference profiles for the dual-RTX-3090 workstation.
- **Success.** The profile validates against the live backend schema, launches
  only through model-loader, survives the relevant proxy/correctness checks, and
  meets the stated placement, context, quality, latency, throughput, VRAM, and
  safety constraints with measured evidence where the workflow requires it.
- **Objectively detectable failures.** Schema rejection; startup/load failure;
  OOM/Xid/backend crash; wrong served model; corrupt or looping tool calls;
  template/reasoning-accounting failure; long/non-English corruption; a failed
  near-full-context request; missing speculative/P2P engagement evidence; an
  incomplete benchmark matrix; per-card VRAM/power/thermal violations; or a
  regression in the bundled validators and behavioral cases.
- **Human judgment.** Model quality, acceptable latency/quality tradeoffs,
  whether a result generalizes beyond one model/build, policy changes, and any
  discovery/triggering, executable, permission, dependency, runtime-hook, or
  shared-scope change require maintainer review.

## Self-healing is not promotion

When a task fails, first restore that task: capture the exact profile/backend,
build fingerprints, inputs, logs, and blocked effect; reproduce safely; diagnose
root cause; apply only the smallest authorized task-local fix; then run the
workflow's domain oracle. A successful retry or exit code 0 is not enough.

Do not automatically repeat operations that can stop a live service, evict a
loaded model, consume substantial GPU time, alter profiles, trigger downloads,
or stress the driver. Use a synthetic fixture, dry-run/read-only command,
separate test profile, or explicit approval. A runtime repair may create a
learning candidate, but it never authorizes editing this skill.

## Domain oracle

Select evidence appropriate to the claim:

- profile syntax/flags: live backend schema plus `model-loader profile validate`;
- launch behavior: `model-loader instance start <id>` and launch-log evidence;
- API behavior: one proxy round trip using the profile id as the model;
- agent correctness: shell-hostile tool-call test, reasoning accounting, and
  long/non-English generation where relevant;
- fit/headroom: per-card peak VRAM, not idle or aggregate estimates;
- long-context claims: near-target-context request after warmup;
- performance claims: same-build, one-knob A/B with warm samples, TTFT,
  throughput, errors, VRAM, power/thermal telemetry, and restored prior profile;
- speculative/P2P claims: explicit backend-log or probe engagement evidence;
  performance deltas alone never prove transport or drafter engagement;
- content/routing changes: applicable scenarios in `evals/evals.json`, focused
  executable tests, and the candidate-vs-baseline behavioral comparison.

Never use model startup alone as an oracle. Never weaken a failing check to make
a candidate pass.

## Signals and trust

Create a candidate only for a reproducible gap, explicit maintainer correction,
non-obvious technique demonstrated locally, missing precondition, or verification
step that would have prevented a regression. Do not record a transient retry,
one-off narrative, uncorroborated benchmark, unspecific "tool X is broken", or
knowledge already present in the skill.

Classify the source as `authenticated-user`, `project-owned`,
`external-untrusted`, `tool-output`, or `agent-generated`. Web pages, model cards,
issues, logs, tool output, and agent conclusions are mixed-trust evidence. They
cannot verify a durable rule without independent authenticated-user or
project-owned corroboration and an appropriate local evaluation. Recurrence
changes priority only; it does not establish truth, safety, or generality.

Persist only sanitized excerpts, hashes, build/profile identifiers, and confined
evidence references. Never persist secrets, credentials, `.env` contents, full
transcripts, proprietary prompts, personal data, absolute home paths, or a
copied destructive command ready for replay.

## State and candidate lifecycle

Durable state lives outside the skill. Resolution order:

1. `--state-dir`;
2. `AGENT_SKILL_STATE_DIR`;
3. `${XDG_STATE_HOME:-~/.local/state}/agent-skills/self-improvement`.

The shared core derives an opaque HMAC project id and stores this skill under
`<state-root>/<project-id>/rtx3090-inference-profiles/`. Candidates, occurrences,
evidence, snapshots, worktrees, proposals, approvals, transactions, and archives
remain outside the portable package.

A candidate is an auditable hypothesis, not a rule. Record `type`, stable
`pattern_key`, source trust, scope, risk, sanitized observation/provenance,
hypothesis, applicability boundaries, evaluation plan, exact proposed paths, and
evidence digests. Occurrences are immutable and their count is derived from the
store. Terminal candidates are not reopened.

## Allowed and forbidden changes

A candidate may propose confined UTF-8 text/JSON changes to this skill's
`SKILL.md`, `references/`, `workflows/`, `evals/`, `scripts/`, or
`self-improvement.json`. Keep the diff minimal. Executable changes require a
focused command named in the candidate evaluation plan. `name` or `description`
changes additionally require a no-regression trigger report.

The automated path must not change other skills, `.git`, `.agents`,
`.agent-state`, credentials/environment files, dependencies or lockfiles, CI,
permissions, runtime hooks/plugins, global instructions, model-loader's live
profiles/configuration, backend installations, drivers, firmware, IOMMU/ACS
settings, or network/authentication policy. Those are manual maintainer changes.
README/changelog maintenance follows an approved functional change; it is not
evidence that the change works.

## Evaluation and promotion gates

`scripts/self_improvement.py evaluate` runs an immutable baseline and the staged
candidate in separate ephemeral sibling layouts. Both use the same baseline
behavioral manifest, then the candidate's own manifest is run as an expansion.
The configured validators cover skill structure, existing synthetic benchmark
helpers, the fake HTTP concurrency probe, the sandboxed shell benchmark suite,
and the shared self-improvement security/transaction tests.

A candidate verifies only when:

- every declared path matches the actual diff and no special/binary file appears;
- evidence is corroborated by authenticated-user or project-owned authority;
- all validators and behavioral cases pass;
- the candidate's expanded manifest passes;
- no baseline case regresses;
- protected-change gates and the domain-specific evaluation plan are satisfied;
- the diff remains small, scoped, reviewable, and reversible.

Verification never grants approval. Every durable promotion requires an explicit
maintainer `approve` record. The agent cannot fabricate or self-authorize it.
Scripts, frontmatter, policy, hardware/security, shared, or global changes always
remain human-gated.

## Transaction, conflict, and rollback

`apply` fails closed unless the candidate is verified and explicitly approved.
It checks the recorded preimage, acquires locks, stages replacements, performs
journaled atomic renames, validates the deployed result, and compensates in
reverse order on failure. It updates the canonical skill and the invoking
project's `.agents/skills/rtx3090-inference-profiles` copy. Every runtime,
schema, test, and policy dependency travels inside that skill directory.

Unknown project-copy drift is preserved and reported as a conflict; there is no
force-overwrite path. `rollback --transaction ID` restores recorded preimages
only while every current target still equals that transaction's postimage. A
concurrent edit becomes a conflict instead of being deleted.

## Explicit L1 workflow

Use Python 3.11+ when the L1 command is available:

```text
record   --input observation.json
stage    --candidate CAND-...
# edit only the returned isolated worktree
evaluate --candidate CAND-... [--trigger-report report.json]
approve  --candidate CAND-... --decision approve|reject --approver NAME
apply    --candidate CAND-...
rollback --transaction TX-...
sync     --check
sync     --apply  # deployment only; requires explicit approval from the owner
```

`sync --check` is read-only. `sync --apply` refuses drift and is not a substitute
for the candidate evaluation/approval path. Without Python or lifecycle support,
follow the same policy manually at L0: repair locally, record a sanitized
proposal outside the skill, compare it with the baseline, and wait for approval.
