# Lessons from pstack for bts

Status: proposal v2, revised 2026-11-04 after an independent review.  Only the items under Decisions are decided.

Sources: pstack from `cursor/plugins` at `180939f` (2026-11-03), bts at `v0.17.0` (`1235217`).  The review, with `file:line` evidence for every claim, is [`pstack-lessons-review.md`](pstack-lessons-review.md).  It was run by a separate Fable session at max effort that wrote its own map from the sources before reading v1 of this page.

## Constraints

- No other model family.  There is no GPT, Grok or Gemini subscription, and the Claude Code `Agent` tool takes only `opus`, `sonnet`, `haiku` and `fable`.  pstack's own Claude Code adapter (`docs/harness/claude-code.md`) runs every non-Claude seat on the session model, and its arena rule that the judge comes from another family cannot be met with Claude models alone.
- Opus and Sonnet carry the work.
- Fable is rate-limited: one role, at the most important moment, sparingly.

## Decisions

Taken by the operator on 2026-11-04.

- The persona debate stays in the architect path for the first slice.  `bts-adjudicate` checks scope compliance (2b) and engagement with the official pattern (2e) against the debate's artefacts, and those checks have no pstack equivalent.
- Fable's one role is the architect tie-break behind an Opus judge, added only after the first slice proves its worth.  It never runs inside the verify loop, which can run `verify.max_rounds` (6) rounds of three instruments per document.

## What v1 got wrong

Each was checked against the code after the review.

- `agents:` in `settings.yaml` is applied by no Go code.  The only reader is the key whitelist in `internal/cli/doctor_config.go:179`.  For Agent-tool spawns it is advisory, because a skill reads it and passes `model`.  For fork-as-agent skills it is inert, because the agent file's `model:` wins and nothing rewrites it.  `settings.yaml` lines 165-166 still promise an override that nothing performs.
- The pre-tool-use hook does not count spawns.  It counts scenario and finding ids inside `simulator` and `defender` prompts, a path that is dormant now that both run as forks (`internal/hook/pre_tool_use.go:175-181`).  It does see every Agent-tool spawn's `subagent_type`, `prompt` and `model`, and it can deny one.
- A `budget:` preset would be a dead key.  Effort lives in skill frontmatter, templates are deployed verbatim, and a hook cannot rewrite a tool call's input.
- Capping spec-phase critical and major findings at minor without a probe or a `Source:` line contradicts `bts-verification-protocol.md:101`.  A gap whose answer changes what gets built stays MAJOR even when only a run can settle it.  The noise v1 cited (two criticals and nine majors apart on a byte-identical document; finding count tracking subagents at r=+0.69) is sampling variance, which bts already answers with measurement class, `confirm_passes` and the defender.

## Why the architect arena

The case is context isolation, not model diversity.  `bts-architect`, `bts-debate` and `bts-adjudicate` carry no `context: fork`, so one context proposes the alternatives, plays all three personas and rules on the result.  bts's own rule is never to verify your own output in the same context (`bts-verification-protocol.md:19`).

Under the constraints, independence comes from three sources, strongest first.

1. Independent contexts.  No runner sees another's output, the conversation, or the rubric.
2. Different seeds.  One runner must apply the official pattern from research `## Official Guidance`, which bts already mandates.  The other must minimise the public surface.  Both are told not to hedge toward a middle.
3. Different models.  The weakest source here: bts measured that same-model contexts already diverge widely, and under bts's tiering a Sonnet candidate mostly loses the base pick.  Sonnet is an optional third runner, not the default second.

## First slice

Change `bts-architect` Step 1 only.

- Add `templates/.claude/agents/bts-architect-runner.md`: tools `Read, Grep, Glob, Write`, `model: opus`, a bounded `maxTurns`.
- Step 1 spawns two runners in one message, the shape `bts-review` already uses, and waits for both notifications without polling.  Each gets the same brief and its own seed, and writes `alternatives/{name}.md` in the existing shape (name, basis, node list, invariant ownership map, YAGNI check) plus a short "Usage (caller's view)" section with two or three call sites.
- Steps 2 to 4 run unchanged: debate, adjudicate, decision block.
- No judge, no Fable, no new settings keys.
- Runner output never enters `draft.md`.  bts excludes signatures and type sketches from the blueprint on measured grounds.
- Optional Go check: `bts verify wireframe.md` reports a major when `Selected:` is not `single-path` and fewer than two `alternatives/*.md` exist.

Measure on the next three to five blueprint recipes.

| Signal | Source | Read as |
|---|---|---|
| Architect-phase cost and wall clock | `metrics.jsonl` events with `phase: architect`; `bts stats` | The price of the slice. |
| Node-list overlap between the two alternatives | Jaccard over node names in `alternatives/*.md` | High overlap means the seeds are not producing diversity. |
| Adjudicate ACCEPT vs EXTEND, and extension count | `changelog.jsonl` adjudicate entries; `debates/*-followup` | Each avoided EXTEND saves a research pass and a debate. |
| Indicator 2, re-decomposition count | `bts stats --indicators` | The outcome the architect step exists for. |
| Indicator 14, tier-5 architect escalations | `bts stats --indicators` | The same outcome, seen from implement. |

Baseline: this machine has one recipe with same-context alternatives, `specialist-as-subject` `r-001-docsspecscontact` (three alternatives, `Selected: one-label-parse-ref`).  The snap-voca project measured in `docs/bts-flow-metrics.md` is not on this machine.

Gate for the next slice: add the judge only if candidate overlap is low and at least one EXTEND round is avoided.

## Later slices, gated on the first

- A fresh-context Opus judge scores every candidate per rubric item under blind labels.  The rubric lives only in the judge's prompt, or is written after the runners return, because runners have Read.  Blinding helps the judge more than the orchestrator, which assigned the official-pattern seat.
- Fable breaks the tie when the Opus judge and the orchestrator pick different bases.  Record the disagreement rate before settling whether Fable sits behind the judge or in its seat.  A refused or throttled Fable spawn falls back to Opus and says so.
- The decision block gains an `Adopted from:` line that accepts `none (converged)`.  The checker in `internal/engine/architect_decision_checker.go` regexes only `Selected:`, `Basis:` and `Invariant ownership:`, so the added line is safe.
- Convergence shortcut: when the runners converge, the debate collapses to one round, as the fix recipe already runs.  Measure it rather than assume it.
- Enforced model keys: `architect_runners`, `architect_judge`, `architect_tiebreak` under the existing `agents:` map, read by the pre-tool-use hook.  The hook sees Agent-tool spawns only, so any role it polices must be spawned through the Agent tool, never through `context: fork` or a `model:` frontmatter line.  A Fable spawn outside its role is refused, except for the documented fallback.

## Other proposals, revised

- Falsifier for code-phase criticals.  In `bts-review` full mode a critical finding carries a command and its output, or is downgraded.  This needs two changes first: reviewers today have only `Read, Grep, Glob`, and `review.md` has no findings block for Go to check.  A security critical may use a concrete scenario (input, path, outcome) instead, because security is recall-bound.  Majors stay evidence-or-downgrade; minor and info are unchanged.
- Design red flags.  Add shallow module, information leakage, temporal decomposition, pass-through method, two ways to do one task and importable internals to the judge rubric and to adjudicate's over-engineering (2d) and blind-spot (2f) checks.  Split ownership and hand-synced lists are already covered by the invariant single-owner check and the derived-state rule.
- Scrap signals.  Run `bts refactor-signal` (`test_fix_cascade`, `cross_module_churn`) inside the mid-run review and treat a signal as an architect-escalation trigger.  Add the two patterns the detector cannot see: type escape hatches, and a lock where the sketch said nothing was shared.
- Debug perspectives.  If ported, two or three Sonnet explorers grouped by angle (data and dependencies; design and runtime; history and impact), with synthesis left in the orchestrator, which already is the synthesizer.
- Tree hash.  Record the tree hash in `review.md` and `test-results.json`, and have the implement stop gate compare it to the working tree, as spec gates compare `doc_hash`.
- Decision ledger.  One line in its guidance pointing at `Opens-with:`: a question that a run can answer is not the operator's to decide.
- Developer-side loop.  pstack's blinded eval and `reflect` belong in the claude-bts repository's own `.claude/skills/`, never in `internal/template/`.  Opus for judgment and synthesis, Sonnet for the tooling lens.
- `settings.yaml` lines 165-166.  Stop promising an override for `agents:`, or make the hook read it.

## Not taken

- pstack's "the best spec is code, no planning" stance.  It contradicts bts's spec-first identity.
- Fan-out by default, swarm, and the ten-lane live verification.  bts measured and removed that shape.
- A validator and rebuttal per finding batch.  bts replaced it with one defender pass.
- The twenty-four principle skills, and the hand-written decision TSV.  bts encodes the same lessons structurally, in ledgers the binary writes.
- The PR lifecycle playbooks.  Outside bts's spec-to-code scope, though it is the largest workflow gap between the two.
- Type sketches in `draft.md`, model-slug fallback ladders, and per-role effort tiers.
- Cross-family panels and Cursor-only features.

## Open questions

1. Rate-limit behaviour of a Fable subagent.  A probe on 2026-11-04 confirmed that `Agent` with `model: "fable"` runs on `claude-fable-5-1`, and that one spawn carried about 49K subagent tokens for a one-word reply.  A refusal was not observed, so the fallback cannot be scripted against a known error yet.
2. Whether `SubagentStop` fires for `context: fork` skills.  bts records this as unconfirmed.  It matters for measuring fork costs, not for the arena, which runs on the Agent path.
3. Per-agent model in metrics.  Model is recorded per session today, so a runner-versus-judge cost split needs a new field.
