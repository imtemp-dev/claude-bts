---
name: bts-defend
description: >
  Adversarial defense of a round's open critical/major findings, run on
  the findings ledger after the round is logged. Returns CONFIRM or
  CHALLENGE-with-evidence per finding; the orchestrator dismisses the
  challenges it accepts with `bts recipe findings dismiss`. Replaces the
  validator and rebuttal agents that used to run inside /bts-simulate.
user-invocable: true
allowed-tools: Read Grep Glob Bash
argument-hint: "[recipe-id] [--doc draft.md]"
effort: high
context: fork
agent: defender
---

# Defend the findings

Argue against the open findings of: $ARGUMENTS

## Who runs this

This skill runs **inside the `defender` agent** (frontmatter
`agent: defender`, sonnet). You read the sources and return verdicts.
You do not dismiss anything yourself, do not write files, and do not
spawn agents.

## Where this sits in the loop

```
verify + audit + simulate (one batch) → bts recipe log --merge (ONE round)
        → /bts-defend on the round's open critical/major → orchestrator dismisses accepted challenges
        → IMPROVE the findings that stand → verify again
```

Defense used to run inside the simulation as two agents, a validator and
a rebuttal, on every finding the walk produced. Measured over seven
rounds: 17–30 minutes added to the round's critical path, validators
handed 10–28 findings writing 80–110K output tokens each, and a
dismissal rate of 5–30%. The rebuttal round changed the outcome in two of
nine challenges. Here the defense runs once per round, after the round
is recorded, on the findings that actually block completion, and it
argues in eight lines or fewer.

## Protocol

1. Resolve the recipe and draw the batch:
   ```bash
   bts recipe status
   bts recipe findings defend-batch {id} --doc {doc}
   ```
   The command prints the open **critical** and **major** findings for
   this pass — highest severity first, then most rounds open — capped at
   `2 × simulate.finding_batch` (default 12), and names the rest as
   `Undefended`. Defend exactly what it prints. Do not run
   `findings list` to widen the set: minor findings are not defended (a
   `[resolvable]` minor is cheaper to fix than to argue, a `[deferred]`
   one is a runtime watch-item), and the cap is in Go rather than in
   your reading of it because the measured failure was validators handed
   10–28 findings and abandoned mid-reply at the output-token limit.

2. Copy the command's `Undefended` line into your report verbatim; the
   orchestrator runs you again for those after recording this pass.

3. For each finding: read its anchor, then look for a defense in the
   places the finder may not have looked — other sections, `domain.md`,
   `wireframe.md`, `.bts/specs/project-map.md`, `.bts/specs/layers/`,
   the codebase, the tests. The document under review is not sufficient
   authority for a finding about that document.

4. Verdict per finding:
   - **CONFIRM** — no defense holds. One line.
   - **CHALLENGE** — source-based evidence that the finding is not a
     practical defect: a citation (`file:line` or `document § heading`),
     what actually happens, why the finding does not hold. Eight lines
     maximum. A scope challenge must cite the section that excludes the
     case; a bare "out of scope" is a CONFIRM.

5. Return the report below as your final message. Nothing else.

## Output

```markdown
## Defense: {doc} — round {N}

| ID | Sev | Verdict | Evidence (one line) |
| --- | --- | --- | --- |
| F-8e79a246 | critical | CONFIRM | no section names the 13-arg overload's removal |
| F-140cd8f7 | major | CHALLENGE | domain.md §3 owns the axis; see below |

### F-140cd8f7 — {title}
Source: {file:line | document § heading}
What actually happens: {two or three lines}
Why the finding does not hold: {two or three lines}

## Undefended
- F-…: over the batch budget (rank 13 of 17)
```

## What the orchestrator does with it

For each CHALLENGE, the orchestrator reads the cited source. If the
evidence resolves the finding:

```bash
bts recipe findings dismiss {id} F-140cd8f7 --reason "domain.md §3: axis owned by CoverOrigin; the projection never reads it"
```

The reason must carry the citation — a dismissal is a recorded
adjudication the next verifier is told not to re-raise. A CHALLENGE
whose evidence does not resolve the finding is kept, and the finding is
IMPROVE'd like any other. The orchestrator then logs:

```bash
bts recipe log {id} --action defend --result "{n} defended, {c} challenged, {d} dismissed"
```

The orchestrator wrote the document and is not a neutral party: it
dismisses on the defender's evidence, never on its own reading that the
finding "seems fine".
