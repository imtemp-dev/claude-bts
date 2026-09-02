---
name: bts-simulate
description: >
  Walk through scenarios to find gaps and incorrect assumptions.
  Document mode: test a spec document. Code mode: test implemented code
  against its spec. Runs as the simulator agent itself — one context,
  no nested agents; adversarial defense happens later in /bts-defend.
user-invocable: true
allowed-tools: Read Write Grep Glob Bash
argument-hint: "[file-path] or code"
effort: max
context: fork
agent: simulator
---

# Simulation

Run scenarios to find what's missing or wrong: $ARGUMENTS

## Who runs this

This skill runs **inside the `simulator` agent** (frontmatter
`agent: simulator`). You are the walker. There is no orchestrator layer
above you in this fork and no agent below you: you design the scenarios,
walk every one of them yourself, write the report, and return.

**Do not spawn a subagent** — not to walk, not to validate, not to
rebut. **Do not poll or wait** — nothing is running for you.

Why the fork and the walker are one context now. Seven measured document
rounds (2026-08-27..31): the agent that walked the scenarios cost $2.5
and 5 minutes; the fork wrapped around it cost $12–30 and 40–56 minutes,
because it read the code before spawning, wrote the 600–950-line report
itself, edited it eight times, and polled its background children with
`echo standby` while it waited. That layer was 60% of the simulate cost
and all of its wall-clock lead over verify and audit. It is gone; do not
rebuild it inside this agent.

Adversarial defense of findings moved to `/bts-defend`, which the
orchestrator runs on the ledger after the round is logged — off the
round's critical path, on critical/major findings only. Report what you
find here; do not defend it.

Bash is for read-only commands: `bts recipe status`,
`bts recipe verify-focus`, `bts recipe findings carry-forward`,
`bts validate`. Never `bts recipe log`; never write outside the recipe's
`simulations/` directory.

## Settings

Read `.bts/config/settings.yaml`:

| Key | Default | Meaning |
|---|---|---|
| `simulate.min_scenarios` | 5 | Below this a round is too easy to pass |
| `simulate.max_scenarios` | 12 | Ceiling per round; surplus goes to Uncovered |
| `simulate.cross_boundary_ratio` | 0.30 | Share of scenarios that must cross 2+ state axes or sit on an illegal cell (`bts validate` raises critical below it) |

`simulate.scenario_batch` and `simulate.finding_batch` are not read by
this skill any more (see settings.yaml).

## Mode Detection

Parse $ARGUMENTS:
- If first word is `code` → **Code Simulation**
- Otherwise → **Document Simulation** (spec walkthrough)

Resolve the recipe: `bts recipe status` gives `{id}`; the recipe
directory is `.bts/specs/recipes/{id}/`.

---

## Shared protocol (both modes)

### 1. Settle what changed before designing anything

If `simulations/` already holds a round for this target, this is a
**re-simulation** and it is scoped to what changed:

```bash
bts recipe verify-focus {target-path}
bts recipe findings carry-forward {id} --doc {target-basename}
```

- Re-walk only scenarios whose steps touch a changed section, scenarios
  covering an illegal cell that is new or was re-classified, and
  anything the previous report listed as `Uncovered` that now fits the
  budget. Cap new walks at `max_scenarios / 2`.
- Carry every other scenario forward **by ID with its previous result**,
  in a `## Carried forward` section. Re-deriving an unchanged scenario's
  walk costs a full reasoning pass and cannot produce new information —
  a measured second round walked 19 scenarios against a first round's 15
  under this rule.
- **A carried scenario's open findings are still findings.** The
  `<bts-findings>` block of a delta round lists every finding that is
  still open on this target — walked or carried — under its previous
  title. The round is recorded with all three dimensions, so the ledger
  reads a finding this block omits as *unreported*, and two silent
  rounds close it as fixed with nobody having fixed it. Only findings
  the carry-forward block marks FIXED or DISMISSED are left out.
- If `verify-focus` reports no change since the last simulated revision
  (the confirming rounds before completion are exactly this case), walk
  nothing: re-issue the previous scenario set and its open findings as
  carried, and say so. The verifier and auditor re-read the document;
  the walk has nothing new to read.
- The carry-forward block lists adjudicated findings. Never re-raise a
  DISMISSED one; reuse the exact title of a STILL OPEN one; report a
  FIXED one only if the defect is back.

First round on a target → full design, no carry-forward.

### 2. Read the sources once

Document mode: the document, `domain.md` (§3 state partitioning, §4
combinatorial state space), `wireframe.md` (state machine, component
diagram). Code mode: the spec plus every file it lists (see below). Read
each once. Do not read the codebase in document mode unless a scenario
step cannot be traced without it.

### 3. Design the scenario set

Between `min_scenarios` and `max_scenarios`. If the target has mermaid
diagrams, read the state machine first: every edge and every transition
should be reachable by at least one scenario. Cover the risk surface for
THIS target — what could go wrong, be misused, break at boundaries or
under load — rather than a fixed checklist.

**The ceiling is a budget, spent in this order:**
1. One `[illegal-cell: ...]` scenario per ILLEGAL cell in `domain.md § 4`
   — the cells the spec claims it prevents; an unprobed claim is the
   point of simulating. Document the enforcement mechanism, or flag
   `INV-GAP` (critical) when nothing prevents the transition.
2. `cross_boundary_ratio` worth of `[cross-boundary: axes=A,B]`
   scenarios — trigger in one module, effect in another (wireframe
   component diagram), state change spanning 2+ axes from `domain.md
   § 3`. Per-module scenarios cannot surface interaction failures.
3. The remaining budget on the riskiest uncovered edges: irreversible
   steps, boundaries with an external system, paths a recent revision
   changed.

Whatever the budget does not reach goes in `## Uncovered`, named, with
why it ranked below the line. A round that quietly covers 12 of 40 edges
reads as "simulated" and is not.

### 4. Canonical format and tags (REQUIRED)

`bts validate` parses every `simulations/*.md` file. Untagged scenarios
raise `untagged_scenarios` (major); a cross-boundary ratio below the
threshold raises `insufficient_cross_boundary_coverage` (critical); a
count outside the budget raises `scenario_floor_not_met` (major) or
`scenario_budget_exceeded` (minor).

Use the **Scenario Index table** (Form C) — it is the report's spine and
the parser reads it directly:

```markdown
| ID  | Title                         | Tag                               | Result | Findings         |
| --- | ----------------------------- | --------------------------------- | ------ | ---------------- |
| S01 | Happy path                    | [single-axis: Auth]               | PASS   | —                |
| S02 | Key rotation mid-flight       | [cross-boundary: axes=Auth,Cache] | GAP    | GAP-001          |
| S03 | Reach C8 via legacy import    | [illegal-cell: C8]                | ISSUE  | ISS-001, GAP-002 |
```

- First cell MUST be `S\d+` or `sim-<label>`; the tag sits in the row.
- Exactly one tag per scenario: `[cross-boundary: axes=A,B]`,
  `[single-axis: A]`, or `[illegal-cell: <label>]`. Axes come from
  `domain.md § 3`; the illegal-cell label must match `domain.md § 4`
  exactly (`uncovered_illegal_cell` is critical).
- Heading forms (`### S01 — Title [tag]`, `### Scenario sim-001.s1: Title
  [tag]`) are also accepted, but do not mix a heading per scenario with
  the table — one shape per file.

### 5. Walk — here

For each scenario, trace the steps against the source under test:

```
Step N: [action] → specified/implemented as [X] ✓
                 → nothing specified / no code path       → GAP
                 → specified as [Y] but [problem]         → ISSUE
```

Classify each finding and assign a stable ID:
`[GAP-001]`, `[ISS-001]`, and in code mode `[COV-001]` for a path no
test exercises.

Severity (`bts-verification-protocol.md § Severity Classification` is
authoritative): critical or major ONLY for a load-bearing item — an
invariant or its owner, a boundary contract, an irreversible order, a
scope decision. Everything else is minor: `[resolvable]` when the spec
can fix it, `[deferred]` with a `Why-deferred:` line when only a run
can. A finding is one defect; do not split one silence into five
findings by section.

### 6. Write the compact report

Save to `.bts/specs/recipes/{id}/simulations/NNN-{category}.md`
(document mode; NNN increments) or `NNN-code.md` (code mode — implement
and fix detect code simulation by that suffix).

```markdown
# Simulation: {target} — round {N}

Generated: {ISO8601}
Recipe: {id}
Mode: document | code
Scope: full | delta (re-walked S02, S05; carried 9)
Scenarios: {walked} walked, {carried} carried, {uncovered} uncovered

<bts-findings>
{
  "critical": 0, "major": 1, "minor_resolvable": 1, "minor_deferred": 0, "info": 0,
  "paths_total": 0, "paths_unspecified": 0,
  "findings": [
    {"severity": "major", "title": "restore path never reaches the projection", "anchor": "S02 / §2.1 INV-005"},
    {"severity": "minor_resolvable", "title": "preview failure copy is not in Localizable.xcstrings", "anchor": "S07 / §3.1"}
  ]
}
</bts-findings>

## Scenario Index
| ID | Title | Tag | Result | Findings |
| --- | --- | --- | --- | --- |
...

## Findings
### [GAP-001] restore path never reaches the projection — major
Where: S02 step 3
Trigger: {the action}
Source says: {what the document/code says, or "nothing"}
Consequence: {why two implementors would diverge / what breaks}

## Uncovered
- {edge or cell} — {why below the line}

## Carried forward
- S01 PASS (round 1) · S04 GAP-003 still open (round 1) · …
```

Rules for the file:
- The `<bts-findings>` block comes first and its `findings` array has
  one entry per finding with a stable title and an `anchor` naming the
  scenario and section (document) or `file:line` (code). Counts and
  array must agree — `bts recipe log` refuses a block where they differ.
- **No step-by-step transcript for a PASS.** A passing scenario is its
  table row; the walk lives in your reasoning. Findings get at most six
  lines each. A 12-scenario report should land well under 200 lines.
  Measured reports of 590–951 lines were re-read by every later verifier
  and defender, which is where a round's cost compounds.
- Keep the DEVIATION list (code mode) in exactly the form below;
  `/bts-sync` consumes it.

### 7. Return

Your final message is the `<bts-findings>` block verbatim, then one
line: `{walked} scenarios, {findings} findings ({critical} critical,
{major} major), report: simulations/NNN-{category}.md`. The orchestrator
logs the action (`bts recipe log {id} --action simulate --output
simulations/NNN-{category}.md …` — the completion gate looks for that
changelog entry) and records the round with `bts recipe log … --merge
<this file>`. Do not log either yourself.

If a read fails or a rate-limit / usage-limit message appears in any
tool result, stop and return `[HALT] <reason>` instead of a report — a
round that silently continues past a dead tool records a measurement
that never happened.

---

## Document Simulation — specifics

Target: `$ARGUMENTS` (default: the active recipe's `draft.md`). Walk the
document's described flow. A step the document does not specify is a
GAP; a step it specifies wrongly, or in two incompatible places, is an
ISSUE. The bar for a finding in document mode: two reasonable
implementors reading only the text would build different things, or the
text contradicts `domain.md` / `wireframe.md`.

After the round, the recipe loop will run `/bts-defend` on the open
critical/major findings, IMPROVE the document for the ones that stand,
and `/bts-verify` again. Do not anticipate that here.

---

## Code Simulation — specifics

Target: the implemented code, against `final.md` (implement recipes:
file list from `tasks.json`) or `fix-spec.md` (fix recipes: § Changes).

- Read every implemented file completely once. Build the call graph, the
  branches, the error paths, the external calls (DB, API, file I/O).
- Design scenarios from the spec's mermaid diagrams first (every edge,
  every error/recovery path), then the budget order above.
- For every step, trace the actual code path (`function:line`). A
  scenario the spec expects but no test exercises is `[COV-nnn]`
  **COVERAGE GAP**; check the test files listed in `test-results.json`.
- **Flow comparison** (when the spec has mermaid): compare the spec's
  edges with the code's actual flow.
  - Edge in spec, not in code → GAP
  - Edge in code, not in spec → **DEVIATION** (undocumented behaviour)
  - State in spec unreachable in code → GAP
  DEVIATIONs are not findings; they go to `/bts-sync` as undocumented
  behaviour. List them at the end of the report exactly as:

```markdown
## Flow Comparison
{mermaid of the actual code flow, only if the spec had one}

### DEVIATIONs (for bts-sync)
- [DEVIATION-001] {file:line} — {description}
```

After code simulation the implement/fix flow fixes GAPs and ISSUEs, adds
tests for COVERAGE GAPs, re-runs `/bts-test`, routes DEVIATIONs to
`/bts-sync`, and does **not** re-run the simulation.
