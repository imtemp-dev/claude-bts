---
name: bts-falsify
description: >
  Turn each invariant in a build recipe's domain.md into a test against
  the skeleton's public surface that fails now, for the invariant's own
  reason, and record each with `bts falsifier run`. The same tests must
  pass when implementation is done.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash
argument-hint: "[recipe-id]"
effort: high
---

# Falsify

An invariant is a claim about code that does not exist yet. It has no
truth value until something runs. Here each invariant becomes the test
that would prove it false, written before the code, so implementation has
a target a run can check and the spec has nothing left to argue about.

## Read

- `domain.md` § 2: each invariant's id, statement, owner and enforcement,
  and the illegal cells.
- The skeleton files, for the public surface each test calls.

## Write one test per invariant

In the project's test framework, next to its other tests:

- Name it with the id: `test_inv_003_one_issue_per_pr`,
  `TestInv003OneIssuePerPR`.
- Call the owner's public surface, as a caller would.
- Assert the invariant's observable outcome: the state after the
  operation, what is returned or written, or that a forbidden transition
  is refused. An illegal cell becomes a test that the transition is
  refused or cannot be expressed.
- Write the assertion first. A test that only calls a function fails now
  because the body is missing, and would pass on any implementation that
  returns. The test must fail on a wrong implementation too.
- Reach external services through a fake at the boundary the skeleton
  declares. An ordering invariant becomes a scripted sequence of calls
  with the state checked after each.

An invariant no test can reach is stated badly. Restate it as an outcome
someone can observe, in `domain.md`, or ask the user with `bts recipe
decision hold`.

## Record

Run each test alone and record it:

```bash
bts falsifier run {id} --inv INV-003 --cmd "<command that runs only this test>"
```

The command must fail now. A test that passes on the skeleton is refused
and not recorded: make it assert the outcome. When the skeleton changes
later, rerun them all with `bts falsifier check {id} --red`.

Add each row to `final.md` § 2 (invariant, test, command). A row whose
test proves an illegal cell from `domain.md` § 4 refused carries the
cell's label as a tag, `INV-003 [illegal-cell: second issue for one PR]`:
that tag, not a simulated scenario, is what `bts validate` counts as the
cell's coverage in a build recipe. Every illegal cell needs a tagged row.

`bts falsifier run` records each run in the changelog itself; there is
nothing else to log for this step.
