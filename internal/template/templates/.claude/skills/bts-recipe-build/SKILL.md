---
name: bts-recipe-build
description: >
  Build a feature by landing in code early. Direction comes from intent,
  scope, a domain model, research and two independent architect runners;
  then the design is stated as a skeleton that compiles and one failing
  test per invariant, reviewed once, and implemented until those tests
  pass. Truth comes from the compiler and the tests, not from rounds of
  verification over prose.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent AskUserQuestion mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"feature description\""
effort: high
---

# Recipe: Build

Build a feature: $ARGUMENTS

## Why this recipe

The blueprint recipe converges a prose spec through rounds of verify,
audit and simulate, then a replicated clean pass. Measured on one recipe,
each round took about 25 minutes, and bts's own record says why more
rounds do not settle more: two rounds over a byte-identical document
disagreed by two criticals and nine majors, and nine of one recipe's
seventeen criticals were questions a single execution answers.

This recipe keeps the part that sets direction (intent, scope, domain,
research, architect with independent runners, wireframe) and replaces the
loop with things that run: a skeleton that compiles, a failing test per
invariant, and throwaway runs for empirical questions. One review reads
what only judgment can see. Then implementation turns the tests green.

## Settings

Read `.bts/config/settings.yaml`. Use its values where present.

## Resume

```bash
bts recipe status
```

| Phase | Resume at |
| --- | --- |
| `discovery`, `scoping` | Step 2 |
| `research` | Step 3 |
| `domain-model`, `architect`, `wireframe` | the step for that phase |
| `skeleton` | Step 7; `bts check build` tells you whether it compiles |
| `falsify` | Step 8; `bts falsifier check {id} --red` tells you what is recorded |
| `finalize` | Step 13 |
| implement phases | `/bts-implement {id}` |

## Steps

Run them in order, without stopping between them to summarize. Stop only
for a question only the user can answer (record it with `bts recipe
decision hold`).

1. **Create.** `bts recipe create --type build --topic "<feature>"`.
2. **Intent and scope.** Follow `.claude/skills/bts-recipe-blueprint/SKILL.md`
   § Intent Check, § Vision & Roadmap Check and § Scoping exactly. Scope
   ends CONFIRMED.
3. **Research.** `/bts-research`, scoped by `scope.md`, including its
   `## Official Guidance`.
4. **Domain.** `/bts-domain-model`: entities, invariants with one owner
   each, illegal cells. Every invariant in § 2 will need a test, so state
   each as an outcome someone can observe.
5. **Architect.** `/bts-architect`: two runners, debate, adjudicate, the
   decision block.
6. **Wireframe.** `/bts-wireframe`, then `bts verify wireframe.md` with no
   critical or major left. Its File Structure rows are the files the
   skeleton writes and, later, the tasks.
7. **Skeleton.** `bts recipe log {id} --phase skeleton`, then
   `Skill("bts-skeleton")` with `{id}`. It ends with `bts check build`
   passing.
8. **Falsify.** `bts recipe log {id} --phase falsify`, then
   `Skill("bts-falsify")` with `{id}`. It ends with every invariant's test
   recorded red.
9. **Empirical questions.** For each open question that running something
   would answer (how a library or API behaves, a timing, a format), write
   the smallest throwaway script in a scratch directory outside the
   project, run it, and record the answer in `final.md` as a Known
   Uncertainty with `Resolved:` and the command. Do not argue it in prose
   and do not defer it to implementation.
10. **Build spec.** Write `final.md` in the shape below. It points at the
    skeleton, `domain.md` and the wireframe instead of copying them.
11. **One review.** `/bts-verify` on `final.md` at full scope, recorded with
    `bts recipe log {id} --doc final.md --dimension verify --scope full --from-verification .bts/specs/recipes/{id}/verification.md`.
    A critical or major is fixed where it lives (the skeleton, a test, or
    `final.md`); rerun what the fix touched (`bts check build {id} --cmd ...`,
    `bts falsifier check {id} --red`) and review `final.md` once more.
    A minor is fixed or declared as a Known Uncertainty. There is no audit
    round, no simulate round and no replicated pass.
12. **Status.** `Skill("bts-status")` with `{id}`.
13. **Finalize.** Output `<bts>DONE</bts>`. The stop hook checks
    `skeleton_builds`, `falsifiers_red` and `build_review_clean`, and moves
    the recipe to `finalize`.
14. **Implement.** `/bts-implement {id}`. The skeleton is the starting
    point: a `create` task fills the file the skeleton wrote and keeps its
    public signatures; changing one is a deviation, recorded by sync.
    Before `<bts>IMPLEMENT DONE</bts>`, run `bts falsifier check {id}`; the
    stop hook adds `falsifiers_green` to the implement gates.

## Build spec (`final.md`)

Short: under about 120 lines. It carries what the code and the tests do
not already say.

```markdown
# Build: {topic}

## 1. What ships
{Two to five sentences: the behaviour a user gets, and what is out of scope.}

## 2. Invariants and their falsifiers
The invariants and owners are `domain.md` § 2.

| Invariant | Test | Command |
| --- | --- | --- |
| INV-001 | `tests/test_relay.py::test_inv_001_one_issue_per_pr` | `pytest -q tests/test_relay.py -k inv_001` |
| INV-004 [illegal-cell: second issue for one PR] | `tests/test_relay.py::test_inv_004_refuses_second_issue` | `pytest -q tests/test_relay.py -k inv_004` |

A row that proves an illegal cell from `domain.md` § 4 refused carries
its `[illegal-cell: <label>]` tag; every illegal cell needs one.

## 3. Boundary contracts
{Per boundary: the skeleton file that declares it, and what crosses it
that the types do not show: units, encodings, ordering, idempotency,
error meaning.}

## 4. Units and order
The units are the wireframe's File Structure rows. {Only what the rows do
not say: an order that matters and why.}

## 5. Irreversible order and rollback
{Each step that cannot be undone, what must precede it, and how to roll
back. "None" is an answer.}

## Known Uncertainties

### U-001: {question}
Resolved: {answer} (`{command that showed it}`)
```

## When to use the blueprint instead

When the work is a design document someone else will build from, or when
nothing can be compiled or run yet (a protocol between teams, a schema
another service owns). Then a prose spec is the deliverable, and the
blueprint's loop is what checks it.
