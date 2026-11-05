---
name: bts-falsify
description: >
  Turn each invariant in a build recipe's domain.md into a test against
  the skeleton's public surface that fails now, for the invariant's own
  reason, and record each with `bts falsifier run`; a [carried] invariant
  is recorded with its existing test, which passes now. The same tests
  must pass when implementation is done.
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
  whether it is `[carried]`, and the illegal cells.
- The skeleton files, for the public surface each test calls.

## Write one test per invariant

In the project's test framework, next to its other tests:

- Name it by the behaviour it checks, under the project's naming rules:
  `test_one_issue_per_pr`, `RefusesASecondIssueForOnePr`.  Its row in
  `final.md` § 2 ties it to the invariant; put the id in the name only
  where the project allows bts ids in committed code.
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

## Carried invariants

A row tagged `[carried]` in `domain.md` § 2 is an invariant the code
already keeps; this change must not break it. Its test passes now, and
that is the point: do not write it to fail. Use the existing test its
Enforcement point names (often the earlier recipe's falsifier). When none
exists, write one now that asserts the invariant's outcome and passes on
the current code. Record it the same way, with the id alone (no tag).

If a test of a new invariant passes on the skeleton because the committed
code already kept that invariant before this change, it is carried, not
new: tag its row `[carried]` in `domain.md`. If it passes only because a
skeleton body is inert, it is new and the test does not assert its
outcome yet; never tag a row to get a test recorded.

## Record

Run each test alone and record it:

```bash
bts falsifier run {id} --inv INV-003 --cmd "<command that runs only this test>"
```

The command must fail now, except for a carried invariant's, which must
pass. bts checks which: a new invariant's test that passes on the skeleton
is refused and not recorded (make it assert the outcome), and so is a
carried invariant's test that fails (point at the right test, or restore
the existing behaviour the skeleton broke; the skeleton leaves existing
bodies alone). When the skeleton changes later, rerun them all with
`bts falsifier check {id} --red`, which expects the new ones red and the
carried ones green.

The tests you wrote or changed are often File Structure rows too, so they
are part of the skeleton's code hash.  After the last one, rerun the
skeleton build (`bts check build {id} --cmd "<the same build command>"`)
so `skeleton_builds` sees the code as it now is; otherwise the stop hook
refuses DONE with "skeleton changed after its last build".

Add each row to `final.md` § 2 (invariant, test, command), carried ones
included. A row whose
test proves an illegal cell from `domain.md` § 4 refused carries the
cell's label as a tag, `INV-003 [illegal-cell: second issue for one PR]`:
that tag, not a simulated scenario, is what `bts validate` counts as the
cell's coverage in a build recipe. Every illegal cell needs a tagged row.

The command selects the test alone, by a name or path nothing else
matches.  Some runners pass when a filter matches no test (`dotnet test
--filter`, `go test -run`), so a command that drifts from its test can
read green.

A test renamed or moved after it is recorded (a review, the project's
naming rules) keeps its record through
`bts falsifier repoint {id} --inv INV-003 --cmd "<new command>" --expect "<new test id>"`,
then `bts falsifier mutate {id} --rerun`.  The new command must give the
recorded result, and the mutation rerun under it is what shows it selects
a test that can fail.  Update the `final.md` § 2 row to match.

`bts falsifier run` records each run in the changelog itself; there is
nothing else to log for this step.
