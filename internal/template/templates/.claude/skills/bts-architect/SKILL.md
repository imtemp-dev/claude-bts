---
name: bts-architect
description: >
  Generate at least 2 alternative structural decompositions in
  independent runner agents, debate them, adjudicate, and commit the
  winner to wireframe.md. Prevents the single-path trap where the first
  decomposition the model imagines becomes the only one considered.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent AskUserQuestion WebSearch WebFetch mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "[recipe-id] | escalate {recipe-id} {task-id}"
effort: max
---

# Architect

Force the decomposition choice to be a **judgment between alternatives**,
not the first thing that comes to mind. One proposed decomposition is
a default, not a decision.

## Prerequisites

1. `domain.md` exists and passes `bts verify domain.md` (invariant
   ownership clean)
2. `scope.md` Status: CONFIRMED
3. `research/v1.md` exists AND contains an `## Official Guidance`
   section (per bts-research Step 3.5). If the section is missing,
   gather it NOW before proposing alternatives: identify the stack from
   scope.md / project-map.md and fetch the platform's officially
   recommended architecture per `.claude/rules/bts-evidence-policy.md`
   (Context7 → official docs → site-filtered search). Append the
   findings to the research doc with `Source:` lines. Do NOT propose
   decompositions from imagination while the vendor's own guidance
   sits unread.

If any prerequisite fails, STOP and point the user at the missing step.

## Step 1: Generate alternatives in independent runners

Two `architect-runner` agents write the alternatives, each in its own
context. Do not write an alternative yourself before they return. This
context later debates and adjudicates the alternatives; if it also
wrote them, the candidates are variations on its own first idea and the
judgment is over its own output.

**Grounding rule**: when the platform has an officially recommended
architecture (research `## Official Guidance`), EXACTLY ONE alternative
MUST be that pattern applied to this feature's domain — carrying its
official `Source:` URL. It may lose the debate to a simpler
decomposition, but it must be on the table; rejecting it requires a
stated reason in the decision block's Rejected list. Seed A below
carries it.

### Alternative shape

Each runner reads this section and writes one alternative in this
shape. For each alternative, produce:

- **Name** — short kebab-case identifier, e.g. `state-machine-centric`,
  `entity-per-file`, `pipeline`, `event-sourced`.
- **Basis** — one line: `official: {pattern name} for {framework}@{major}
  (Source: {URL})` for the vendor-guidance alternative, or
  `custom: {one-sentence rationale}` for the others. The official
  alternative must state which framework major the guidance applies to
  and it must match research's `Target versions:` line — a mismatch is
  not disqualifying but the debate must address it. Unsourced "official"
  claims are invalid — the adjudicator treats them as evidence-quality
  FAIL.
- **Node list** — files or modules with a ONE-SENTENCE responsibility
  (same "and"/"&"/"및" rule as bts-wireframe Step 1). Include file path
  hints so it is clear what the decomposition produces.
- **Invariant ownership mapping** — for every invariant in domain.md § 2,
  name the module that owns it in this alternative. Two invariants may
  share a module; the same invariant must NOT appear under two modules.
- **YAGNI check** — list every interface the alternative introduces.
  For each, either name ≥ 2 concrete implementations planned in this
  recipe, or justify why the abstraction earns its keep. Single-
  implementation interfaces without justification are a reject signal
  at Step 3.
- **Usage (caller's view)** — two or three call sites showing how the
  rest of the system uses this decomposition: what a caller imports or
  invokes and what it gets back. Pseudocode or the project's language,
  at most 15 lines in all, no type definitions or full signatures. It is
  what lets the debate judge how much each interface asks of its
  callers.

Each alternative is saved as `alternatives/{name}.md` under the recipe
directory.

### Seeds

Each runner gets one seed and follows it fully. The seeds make the two
candidates differ in shape, not in detail.

- **Seed A, platform pattern.** When research `## Official Guidance`
  names an officially recommended architecture, apply exactly that
  pattern to this feature's domain, with an `official:` Basis. When the
  platform has none, follow the structure the existing codebase already
  uses (`project-map.md`), with a `custom:` Basis naming it. In a project
  with neither, put each invariant's owner next to the data it protects.
- **Seed B, smallest surface.** The fewest modules and public interfaces
  that still give every invariant in domain.md § 2 exactly one owner.
  Prefer one module with a small interface over several modules a
  caller must coordinate.

### Spawn

Spawn both runners in a single message with two Agent tool calls
(`subagent_type: "architect-runner"`), so they run in parallel. Each
prompt carries:

- the recipe id and the recipe directory path;
- the paths of `scope.md`, `domain.md`, the research doc, and
  `.bts/specs/project-map.md` when it exists;
- its seed, copied verbatim from above;
- the instruction to write exactly one file under `alternatives/`.

Give paths, not pasted contents. Do not put the Step 2 debate topic or
any judging criteria in the prompt: a runner that knows how it will be
scored writes to the score. Then wait for both completion notifications;
do not poll.

### Check what came back

1. Read both alternatives end to end.
2. A file missing a field of the shape goes back to a fresh runner with
   the same seed, once, with the missing field named. A runner that
   wrote nothing gets the same single retry. If the retry fails too,
   write that seed's alternative yourself and record
   `runner dropout: seed {A|B}` in the Step 5 log result.
3. When research names an official pattern, confirm exactly one
   alternative carries an `official:` Basis.
4. If both node lists name the same modules with the same invariant
   owners, the runners converged. Record `converged` in the Step 5 log
   result and continue to Step 2 unchanged.

Do not add an alternative of your own at this step. Step 3's `EXTEND`
outcome can still add or revise one after the debate, as before.

## Step 2: Debate

Run `Skill("bts-debate")` with topic:

> Which decomposition in `alternatives/` best fits the invariants in
> `domain.md` and the scope in `scope.md`? Evaluate against: invariant
> ownership clarity, blast radius of change, extension paths, YAGNI risk.

Use three personas:

- **Orthodox architect** — pattern rigor, layered separation, DIP.
- **Simplicity engineer** — YAGNI enforcement, smallest surface.
- **Domain expert** — invariant fit, protection of the core entities.

## Step 3: Adjudicate

Run `Skill("bts-adjudicate")`. Outcome possibilities:

- `ACCEPT` → proceed to Step 4.
- `ACCEPT WITH RESERVATIONS` → proceed to Step 4, record the caveats
  in the Rejected list as "partially-rejected notes".
- `EXTEND` → research the gap the adjudicator flagged, add a new
  alternative or revise an existing one, re-debate. Max 3 extensions.
- `[DEBATE DEADLOCK]` → surface to user for tiebreak; user's choice
  becomes the "conclusion", re-adjudicate for feasibility.

## Step 4: Commit decision to wireframe.md

Prepend this block to wireframe.md (before any mermaid). It must remain
at the top — `engine/consistency_checker.go` looks for the literal
opening tag `<!-- architect-decision -->` on a line by itself.

```
<!-- architect-decision -->
Selected: {alternative-name}
Basis: {official: {pattern} for {framework}@{major} (Source: {URL}) | custom: {rationale}}
Rationale: {one paragraph — why this decomposition fits the domain
invariants and scope better than the alternatives}
Rejected:
  - {alt-name-1}: {one-line reason — if this was the official-guidance
    alternative, the reason must address why the vendor pattern does
    not fit here}
  - {alt-name-2}: {one-line reason}
Invariant ownership:
  - INV-001: {module}
  - INV-002: {module}
  - …
<!-- /architect-decision -->
```

Update `manifest.json` with the decision name:

```bash
# Via bts CLI (future: dedicated flag). For now, edit manifest.json
# directly: set `architect_decision: "{alternative-name}"`.
```

## Step 5: Log and advance phase

```bash
bts recipe log {id} --phase architect --action architect \
  --output wireframe.md --based-on domain.md --doc-type architect-decision \
  --result "selected {name}; rejected N alternative(s); runners 2[; converged][; runner dropout: seed A|B]"
bts recipe log {id} --phase wireframe
```

Keep the bracketed parts only when they happened. The result line is
how the runner step is measured across recipes: how often the runners
converge and how often one drops out.

> **Checkpoint**: architect decision committed. Continue IMMEDIATELY to
> `/bts-wireframe` — the component diagram now has a chosen
> decomposition and an invariant-ownership contract it must honor.

## Skip condition

For recipes with a very small scope (entity count ≤ 2 in domain.md §1
AND file estimate ≤ 3 in scope.md), the architect step can be skipped:

1. Write a minimal `<!-- architect-decision -->` block with
   `Selected: single-path`,
   `Basis: custom: scope too small to warrant alternatives`, and
   `Rejected: (none — scope too small to warrant alternatives)`.
   The `Basis:` line is required — `bts verify` checks for it.
2. Still record invariant ownership — skipping the debate does NOT
   skip the ownership contract (`bts verify` cross-checks the mapping
   against domain.md § 2).

This keeps wireframe's header-presence gate consistent while avoiding
unnecessary process overhead on trivial additions.

## Escalate mode

`/bts-architect escalate {id} {task-id}` is called during implementation,
not before it: by `/bts-implement` at retry-ladder tier 5, after spec and
domain escalation failed to unstick a task, and after a mid-run review
reports a refactor signal. The question is narrower than "which
decomposition": it is which part of the committed one the failure says
is wrong. Skip the Prerequisites and Steps 1 to 5, and do not spawn
`architect-runner` agents: the evidence is the failure, and it is in
this context.

1. **Read the evidence.** The task in tasks.json (`anchor`,
   `modify_scope`, `last_error`, `escalation_notes`), the mid-run report
   or refactor signal that triggered the call if there was one, the
   `<!-- architect-decision -->` block, the File Structure rows for the
   task's file and the files in its `Depends On`, and the invariants in
   domain.md § 2 that the task's module owns.
2. **Name which of three it is**, with the evidence in one sentence:
   - **Task boundary.** The work needs symbols outside `modify_scope`, or
     a file the row does not name. Widen the row's `scope=` when the
     module owns the invariant the symbols serve; otherwise split the
     row in two.
   - **Ownership.** The task keeps editing another module because the
     invariant it enforces is owned there. Move the invariant to the
     module that holds its data: update the decision block's
     `Invariant ownership:` line for it and the affected rows.
   - **Decomposition.** The selected alternative cannot hold the
     invariants: the same workaround in unrelated tasks, special cases
     multiplying, type escape hatches to make the shape compile. Do not
     re-decompose here, because that discards implemented work. Record
     it for the user:
     ```bash
     bts recipe decision hold {id} --key architect-redecompose-{task-id} \
         --question "Re-run /bts-architect from Step 1? {evidence}" \
         --option rerun --option continue
     ```
3. **Apply a boundary or ownership change.** Edit the rows in
   wireframe.md, add one line at the end of the decision block, after
   the whole `Invariant ownership:` list and with no leading dash,
   `Revised: {task-id}: {what moved and why}` (the checker reads the
   ownership list until the first line that is not an entry, so a line
   inside the list hides the entries after it), mirror the rows in
   tasks.json (`anchor`, `modify_scope`; a split adds a task), and run
   `bts verify wireframe.md`. Then set the task, or both halves of a
   split, back to `pending` and append
   `tier 5: architect {boundary|ownership}: {one line}` to
   `escalation_notes`.
4. **Log without changing the phase:**
   ```bash
   bts recipe log {id} --action architect --output wireframe.md \
     --result "escalate {task-id}: {boundary|ownership|redecompose held}"
   ```
   Re-decomposition count (an indicator in `bts stats`) counts these
   entries, which is the point: each one says the first decision missed
   something implementation found.

Return to `/bts-implement`. A held decomposition question blocks the
recipe until the user answers it; that is the handoff.

## Rationale

The Duolingo word-sort failure started with a single decomposition
("Card + DropZone + Game each hold state") that was never stress-tested
against alternatives. By the time the coupling showed up in
implementation, every fix touched 3 modules. Forcing ≥ 2 alternatives
at design time — and making one of them explicit about invariant
ownership — is cheaper than the implementation-time rework.

Forcing ≥ 2 alternatives in one context still left one context to write
them, debate them as three personas and adjudicate the result, against
the rule that no context judges its own output. Two runners in separate
contexts with opposed seeds make the alternatives independent of the
context that judges them. The cost is two Opus spawns per recipe that
reaches this step, and it is not yet measured: read it from the
architect-phase metrics, the runner fields in the Step 5 log result,
and how often Step 3 returns `EXTEND`.
