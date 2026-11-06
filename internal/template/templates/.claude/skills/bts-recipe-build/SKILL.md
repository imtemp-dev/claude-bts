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

If the project's build formats its own sources (a formatter wired into
the build, such as CSharpier.MsBuild), set `implement.format_cmd` to the
formatter's command before the skeleton step.  bts then runs it before
every recorded command, so a build that reformats files does not make the
build, falsifier and test records go stale one after another.

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
   ends CONFIRMED through `bts recipe confirm {id} scope --answer "..."`,
   and a scope revised later (after research) is confirmed again the same
   way, once the user has seen the revision.
3. **Research.** `/bts-research`, scoped by `scope.md`, including its
   `## Official Guidance` and its `## Open questions that decide an owner`.
4. **Domain.** `/bts-domain-model`: entities, invariants with one owner
   each, illegal cells. Every invariant in § 2 will need a test, so state
   each as an outcome someone can observe. An open question that decides
   an owner or an enforcement point (how a framework binds a value,
   whether a library surfaces an error) is answered here with a throwaway
   run and recorded in `domain.md` as a Known Uncertainty with
   `Resolved [L4]:` and the command, before the owner is written: steps 5
   and 6 build on that answer. An invariant this change keeps from earlier
   work, rather than introduces, is a `[carried]` row.
5. **Architect.** `/bts-architect`: two runners, debate, adjudicate, the
   decision block.
6. **Wireframe.** `/bts-wireframe`, then `bts verify wireframe.md` with no
   critical or major left. Its File Structure rows are the files the
   skeleton writes and, later, the tasks.
7. **Skeleton.** `bts recipe log {id} --phase skeleton`, then
   `Skill("bts-skeleton")` with `{id}`. It ends with `bts check build`
   passing. The skeleton is the recipe's first product code: when
   `.bts/config/settings.yaml` sets `implement.require_edit_go: true`,
   the phase change is refused until the user's go-ahead is recorded.
   Ask the user, then `bts recipe decision hold {id} --key code-edit-go
   --question "Start writing product code for {id}?"` and, with their
   answer, `bts recipe decision resolve {id} code-edit-go --answer
   "<their words>"`. `--force` does not pass it.
8. **Falsify.** `bts recipe log {id} --phase falsify`, then
   `Skill("bts-falsify")` with `{id}`. It ends with every introduced
   invariant's test recorded red and every `[carried]` one's recorded
   green.
9. **Empirical questions.** For each open question still left that running
   something would answer (how a library or API behaves, a timing, a
   format), write the smallest throwaway script in the recipe's `probes/`
   directory (not a session scratchpad, which is gone when the session
   ends), run it, and record the answer in `final.md` as a
   Known Uncertainty with `Resolved [L4]:` and the command. Do not argue it
   in prose and do not defer it to implementation. A question that decided
   an owner was answered at step 4, not here.
10. **Build spec.** Write `final.md` in the shape below. It points at the
    skeleton, `domain.md` and the wireframe instead of copying them.
11. **One review.** `/bts-verify` on `final.md` at full scope.  The fork
    returns its findings and writes nothing; when it returns, write its
    output to `.bts/specs/recipes/{id}/verification.md` verbatim, block
    first (`bts-verify` § Division of labor), then record the round with
    `bts recipe log {id} --doc final.md --dimension verify --scope full --from-verification .bts/specs/recipes/{id}/verification.md`.
    While the fork runs you may end the turn and wait for its
    notification; the stop hook allows it.
    A critical or major is fixed where it lives (the skeleton, a test, or
    `final.md`); rerun what the fix touched (`bts check build {id} --cmd ...`,
    `bts falsifier check {id} --red`). A minor is fixed or declared as a
    Known Uncertainty. Then review only what changed: `/bts-verify` on
    `final.md` at delta scope, which reads the sections changed since the
    full pass and the sections that cite them, and checks the full pass's
    criticals and majors against the fix; record it with `--scope delta`.
    A delta review with no critical and no major completes the one review;
    a delta review that finds one sends `final.md` back to a full review.
    Once a round has left no critical or major, fix its minors (or declare
    them Known Uncertainties) and check the fixes with a **confirm** round:
    `/bts-verify` on `final.md` at confirm scope, recorded with
    `--scope confirm`. It reads only those minors and raises nothing new,
    so the review ends there instead of each delta finding new minors.
    Any other later edit to `final.md`, `domain.md` or the wireframe needs
    a delta review. Nothing changed after a clean full pass needs no
    delta. There is no audit round, no simulate round and no replicated
    pass.
12. **Status.** `Skill("bts-status")` with `{id}`.
13. **Finalize.** Output `<bts>DONE</bts>`. The stop hook checks
    `skeleton_builds`, `falsifiers_red` (introduced invariants red,
    carried ones green) and `build_review_clean`, and moves the recipe to
    `finalize`.
14. **Implement.** `/bts-implement {id}`. The skeleton is the starting
    point: a `create` task fills the file the skeleton wrote and keeps its
    public signatures; changing one is a deviation, recorded by sync.
    Before `<bts>IMPLEMENT DONE</bts>`, run `bts falsifier check {id}`; the
    stop hook adds `falsifiers_green` (carried invariants included) to the
    implement gates.

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
| INV-001 | `tests/test_relay.py::test_one_issue_per_pr` | `pytest -q tests/test_relay.py -k test_one_issue_per_pr` |
| INV-004 [illegal-cell: second issue for one PR] | `tests/test_relay.py::test_refuses_second_issue` | `pytest -q tests/test_relay.py -k test_refuses_second_issue` |
| INV-005 [carried] | `tests/test_relay.py::test_old_token_still_pages` | `pytest -q tests/test_relay.py -k old_token` |

A row that proves an illegal cell from `domain.md` § 4 refused carries
its `[illegal-cell: <label>]` tag; every illegal cell needs one. A
`[carried]` invariant's row names the existing test that keeps it.

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

## 6. Scope coverage
| Scope item | Held by |
| --- | --- |
| 1 | INV-001, INV-004 |
| 2 | eval `HandoffRcmClaimEval` (unrelated turns before and inside the topic) |
| 3 | narrowed to turns before the topic: decision `scope-3-before-only` |

One row per `## In Scope` item of `scope.md`, numbered in order (bullets
count too). A row names what holds the item, or the resolved decision
that changed it: a row that holds less than the item asks needs one.
`bts verify` and the DONE gate report an item with no row.

## Known Uncertainties

### U-001: {question}
Resolved [L4]: {answer} (`{command that showed it}`)
```

## When to use the blueprint instead

When the work is a design document someone else will build from, or when
nothing can be compiled or run yet (a protocol between teams, a schema
another service owns). Then a prose spec is the deliverable, and the
blueprint's loop is what checks it.
