---
name: bts-interrogate
description: >
  Adversaries try to break a finished change, each in a private copy of the
  project; only findings a rerun proves survive. Prove stage P3, run by
  /bts-implement Step 5.4 before review.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent AskUserQuestion
argument-hint: "[recipe-id]"
---

# Interrogate: try to break the change

Recipe: $ARGUMENTS (if empty, use the active recipe from `bts recipe status`).

pstack's rule: a finding survives only when a run proves it. bts makes the
adversaries hand over a command that fails while the code is wrong, reruns
it itself in a fresh copy, and keeps only what fails again.

## Step 1: Prepare the seats

```bash
bts interrogate prepare {id}            # two adversaries
bts interrogate prepare {id} --adversaries 3   # a contested or security-sensitive change
```

It prints the scope (the recipe's code files) and, for each seat, its
angle, its private copy and its report path. It also records, in
`interrogate.json`, the code the copies hold: each code file's content
hash, or for a recipe that names no code files (a fix recipe) the git
commit and the files that differ from it. That record is what tells a
small change after the run from a large one (After a large change,
below).

## Step 2: Write the intent

From `final.md`, write a short intent: what the change must do and must
never do, in the user's terms, with the invariants from `domain.md` by id.
Every adversary gets the same intent.

## Step 3: Spawn the adversaries

Spawn every seat **in a single message**, one `Agent` call each, with
`subagent_type: "adversary"` and `model: "opus"`. Give each:

- its seat name, angle, `copy` path and `report` path, as printed;
- the scope and the intent;
- this brief: "Break this change from your angle. Prove each finding with
  a command that exits non-zero while the code is wrong, run in your copy.
  List the files you wrote in repro_files. Leave out anything you did not
  run. No findings is a valid result."

Wait for every notification. Do not poll and do not read their copies.

## Step 4: Verify

```bash
bts interrogate verify {id}
```

bts reruns each finding's command in a fresh copy of the current code with
its repro files and marks it `reproduced` (failed again) or `dropped` (no
command, passed, timed out, or a repro that replaced code under test).
Read the dropped reasons; do not argue them back in.

## Step 5: Fix what reproduced

For each `reproduced` finding:

1. Fix the code.
2. Make the finding's check part of the project's own tests (a regression
   test that failed before the fix), and rerun the tests with
   `Skill("bts-test")`.
3. Recheck all findings on the current code:

```bash
bts interrogate verify {id} --recheck
```

A finding is `fixed` when its command now passes. A later code change makes
a recheck stale; run `--recheck` again before completion.

A fix may refuse the input another way than the finding's command asserts
(a 400 where the repro wants a narrowed result or another error code), so
the command keeps failing though the defect is gone.  Close it with the
regression test from item 2 and a patch that reverts only the fix in the
code, never a test:

```bash
bts interrogate pin {id} --finding F-00N --cmd "<the regression test alone>" --revert <patch> --expect "<its test id>"
```

bts runs the test on the current code (it must pass) and in a copy with the
patch applied (it must fail and print its id), then marks the finding
`fixed`; later rechecks rerun the pinned test.  This is a fix, not a waiver:
it needs no agreement, but the test must really pin the fix.

A finding may stay unfixed only if the user agrees. Ask with
`AskUserQuestion`, then record the user's reason:

```bash
bts interrogate waive {id} --finding F-00N --reason "<the user's reason>"
```

## After a large change: a scoped round

A clean run does not go stale by itself, so code reworked after it would
never be attacked. bts compares the code with what the adversaries
copied. A small change keeps the cheap path: rerun the tests, the
falsifiers and the drives on the current code. The change is large when
either holds, counting code files only (tests and documentation files
such as `.md` do not count):

- a code file the adversaries did not see exists now (a new file), or
- more than two of the code files they saw changed (a deleted one
  counts).

Then `interrogate_clean` blocks IMPLEMENT DONE and FIX DONE until a scoped
round has attacked the change, and `bts interrogate verify {id}
--recheck` says so. Run it on the same record:

```bash
bts interrogate prepare {id} --rescope        # one adversary on the changed files
```

It refuses when the current round is not verified yet or nothing
changed. It prints the scope (the files changed since the last round)
and one seat whose angle is that change. Write the intent as in Step 2,
spawn the seat as in Step 3 with the scope it printed, then run
`bts interrogate verify {id}`: it reads only the new seat's report, and
its findings are numbered after the earlier ones, which keep their
status. Fix and recheck as in Step 5. Add `--adversaries 2` or `3` for a
rework of a contested or security-sensitive part.

## Done

`bts interrogate verify {id} --recheck` reports no open finding and does
not ask for a scoped round. The stop hook checks the same at IMPLEMENT
DONE and FIX DONE (`interrogate_clean`).
