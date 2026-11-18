---
paths:
  - ".bts/**"
authoritative_for:
  - phase_lifecycle
  - completion_markers
  - mandatory_rules
---

# BTS Recipe Protocol

## Development Lifecycle

bts maps to a standard development process:

```
Requirements → Planning → Design → Implementation → Verification → Release
     ↓            ↓         ↓           ↓                ↓           ↓
  discover     vision    blueprint   implement        test+review   sync+status
  (intent)    roadmap    (spec)      (code)          prove         (complete)
               scope
```

### Phase Flow

```
discovery → scoping → research → wireframe → draft ←→ adaptive loop → finalize
                                                          ↓
                                              implement → test → prove (able to fail, interrogate, drive the app) → review → sync → status → complete
```

### Recipe Types and Their Lifecycle

| Recipe | Phases Used | Output |
|--------|-------------|--------|
| build | scoping → research → domain → architect → wireframe → skeleton → falsify → one review → finalize → implement → test → prove → review → sync → complete | build spec (final.md) + skeleton + falsifiers + code |
| blueprint | discovery → scoping → adaptive loop → finalize → implement → complete | Level 3 spec + code |
| design | research → adaptive loop → finalize | Level 2 design doc |
| analyze | research → adaptive loop → finalize | Level 1 analysis doc |
| fix | research → draft → verify → implement → test → review → complete | fix-spec.md + code |
| debug | research → perspectives → draft → verify → finalize → implement → complete | debug spec + code |

### Choosing a recipe type

Pick by what the work must deliver, top row first; the first that fits wins.

| The work is | Type |
|---|---|
| behaviour that is wrong against what was intended, with a symptom you can reproduce | fix |
| a bug whose cause one reading of the code does not show (intermittent, spread across modules) | debug |
| a feature or a behaviour change built here, where the code can be compiled and tested: new code, or a change to committed code | build |
| a prose spec someone else builds from, or a subject nothing here can compile or run yet (a protocol between teams, a schema another service owns) | blueprint |
| a design document, with no implementation in this recipe | design |
| understanding an existing system, with no change | analyze |

A change that adds or alters intended behaviour is build, not fix, even when it touches only committed code.  Say the type you picked and the row that decided it.

### Project-Level Documents

These persist across recipes and provide continuity:

| Document | Created | Updated | Purpose |
|----------|---------|---------|---------|
| intent.md | /bts-discover | per-recipe | Why this feature exists |
| vision.md | blueprint scoping | direction changes | Final product vision |
| roadmap.md | blueprint scoping | recipe completion | Ordered recipe sequence |
| project-map.md | /bts-status | recipe completion | Layer overview |
| project-status.md | /bts-status | recipe completion | All recipes status |

## Adaptive Loop

Recipes use an adaptive loop, NOT a fixed sequence:

```
ASSESS → decide action → execute → VERIFY (mandatory) → ASSESS → ...
```

/assess determines what to do next based on the document's current state and level.

## Mandatory Rules

Each rule below carries a `{gate: ...}` tag indicating how it is enforced.
- `hard` — a deterministic mechanism (stop hook, validator, CLI gate) blocks
  progress when violated. Listed in `engine/gate_registry.go`.
- `invariant` — a domain-level check fails and surfaces as a critical
  finding in `bts verify`.
- `practice` — a heuristic. Not machine-enforced; violations are surfaced
  as major/minor findings but do not block completion.

When context or time is tight, `hard` gates MUST be upheld first;
`practice` items are negotiable.

1. **Check for resume first**: `bts recipe status` before starting any recipe. {gate: practice}
2. **Edit draft.md in place**: Use Edit tool for incremental modifications, not full rewrites. {gate: practice}
3. **VERIFY after every modification** of a spec document during the spec phases: no exceptions there.  This includes post-debate fixes.  After implementation starts, sync's edits to final.md record what the tested code does and get `bts validate`, not another review round. {gate: hard}
4. **Log every action**: `bts recipe log {id}` after every step. {gate: hard}
5. **Prove by runs, not by simulation**: simulation was removed.  Behaviour is shown after implementation by the prove stage: every falsifier fails with its guard broken (`falsifiers_able_to_fail`), adversaries must reproduce what they find (`interrogate_clean`), and user journeys are driven through the real app (`app_proven`). {gate: hard}
6. **Debate uncertain choices**: Don't guess. Use /debate for technology decisions. {gate: practice}
7. **Adjudicate every debate**: After /debate, ALWAYS run /adjudicate to evaluate the conclusion. Never accept a debate result without adjudication. {gate: hard}
8. **Sync-check before finalizing**: All debates reflected, all gaps resolved, all drafts verified. {gate: hard}
9. **Run /bts-status at finalization**: Every recipe calls bts-status before completion to update roadmap, project-status, and project-map; a feature item's recipe reports in chat and leaves those project-wide files to the base branch (the PreToolUse hook refuses its writes to them, bts #173). {gate: hard}
10. **Domain model precedes wireframe** (blueprint/design types): `domain.md` must exist and pass the invariant-owner check before `/bts-wireframe` runs. {gate: invariant}
11. **One batch, one round**: verify and audit run in one concurrent batch and are recorded as ONE verify-log entry with `bts recipe log --merge`. {gate: practice — `bts recipe log` notes a partial-dimension round, and completion refuses it}
12. **Defend before IMPROVE** (blueprint type): when a round records critical or major findings, run `/bts-defend` on the ledger and dismiss only on its cited evidence before improving. {gate: practice}

## Human Intervention

The loop runs automatically. It pauses ONLY for:
- **[DECISION REQUIRED]**: Human must choose between alternatives
- **[CONVERGENCE FAILED]**: Same issues after N iterations
- **[DEBATE DEADLOCK]**: Experts can't agree after 3 rounds

## Completion

### Spec Completion
Output `<bts>DONE</bts>` only when:
1. /assess declares Level 3
2. /sync-check passes
3. Last verify-log entry shows critical=0, major=0

### Implementation Completion
Output `<bts>IMPLEMENT DONE</bts>` only when:
1. All tasks in tasks.json are `done` or `skipped` (no `blocked` or `pending`)
2. test-results.json shows status=pass
3. /review has run (review.md exists)
4. /sync has run (deviation.md exists)

deviation.md is a **report**, not a gate. Deviations and not-implemented items
are recorded for follow-up but do NOT block completion.

### Fix Completion
Output `<bts>FIX DONE</bts>` only when:
1. fix-spec.md exists (fix documented)
2. test-results.json shows status=pass

Fix recipes implement code directly (not via /bts-implement).
fix-spec.md is the authoritative document, not final.md.

### Follow-up After Completion
After a recipe reaches `complete`, its deviation.md feeds the next cycle:
- Not-implemented items → new recipe or manual fix
- Deviations → bug fix or spec update
- The spec (final.md) stays alive — it evolves through follow-up work

### Roadmap Progression
If `.bts/specs/roadmap.md` exists:
- The recipe's roadmap item is marked done (stop hook + /bts-status)
- Session-start hint shows the next pending item
- The next /bts-recipe-blueprint scoping reads roadmap context automatically
