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
angle, its private copy and its report path.

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

A finding may stay unfixed only if the user agrees. Ask with
`AskUserQuestion`, then record the user's reason:

```bash
bts interrogate waive {id} --finding F-00N --reason "<the user's reason>"
```

## Done

`bts interrogate verify {id} --recheck` reports no open finding. The stop
hook checks the same at IMPLEMENT DONE (`interrogate_clean`).
