# Design: prove the change by running it

Status: built 2026-11-05 (six decisions below; slices 1-6 done), nothing implemented; implementation starts after r-002's completion marker is accepted.  Direction decided by the operator on 2026-11-05: after implementation, take truth from runs, as pstack does, instead of from a reasoning walk over the code.  That means checking the real artifact, adversaries who must run what they find, a confidence level on every claim, and driving the app the way a user does.

Sources: pstack from `cursor/plugins` at `bb68f23` (2026-11-04), bts on `feat/code-first-recipe` at `d23a572`, and the AGEN-35 trial (`r-002-agen-35-slack-pr` in `slack-pr-relay`).

## Why

Measured on AGEN-35's build recipe, after implementation:

- 136 of 136 tests and 12 of 12 falsifiers pass on the final code.  Every one runs against fixtures and a fake `multica`.  Nothing has started the relay itself.
- Each falsifier's red run was recorded on the skeleton, where it failed at `NotImplementedError`.  No falsifier has been seen failing for its invariant's own reason.
- `bts-implement` Step 5.3 then ran `bts-simulate code`: a model reading the code paths that those tests had just run.  It is one pass, not a loop, but five minutes in it had used 171K subagent tokens, against about 541K for the whole spec side.
- That pass found 7 findings in 12 scenarios.  One was major: COV-004, "the boot identity check that refuses a non-bot token in the bot-token slot, INV-007's only guard against a Keychain mix-up, and the rest of `main()` are never executed by a test".  So INV-007's falsifier was green without running its guard.  P1 shows this by a run: remove the check, and the falsifier still passes.  P2 shows it too, because a boot journey runs `main()`.  The deferred GAP-001 (a run Multica enqueues asynchronously reads as missing right after the create) needs the real `multica`, which P2's local journey and P3's boundary adversary use.
- Slice 1, run on a snapshot of the same code after that pass: deleting INV-007's bot-token guard leaves INV-007's falsifier (`pytest -k inv_007`, 2 tests) passing, in 2.6 s.  Of the full suite (166 tests), only the test simulate added for COV-004 fails, and it is not part of the falsifier.  A run finds the same gap, and that the invariant's own falsifier still misses it.
- The review reviewers have `Read, Grep, Glob` only, so no reviewer can run what it finds.  `review_criticals_proven` asks for run or scenario evidence on criticals alone.

## What pstack does at this stage

| Skill | Rule |
| --- | --- |
| `principle-prove-it-works` | Check the real artifact: run the feature, read the actual value.  A proxy, a self-report or "it compiles" proves nothing.  Script the check and keep its output as an artifact a reviewer can re-run. |
| `create-formal-verification-skill`, "Able to fail" | A check never seen failing proves nothing.  Show each property failing on a broken variant of the code it guards, such as a removed check or a reordered step. |
| `principle-test-behavior-not-implementation`, "Break the behavior" | Make the code return a wrong value, skip an effect or do it twice.  A test that still passes proves nothing. |
| `interrogate` | Adversaries, each in its own worktree, try to break the change.  A finding survives only when a run proves it (a failing test, a repro script, command output), and the verifier reruns each command.  Style, naming, structure and risks that need a future change are left out.  No findings is a valid result. |
| `blast-radius`, "How sure are you" | Each fact the change's safety rests on is placed on a ladder: 1 said so, 2 pointed at the line, 3 walked the failure, 4 ran it, 5 reproduced it in the running app.  The report says where each fact stopped. |
| `create-verification-skill`, `maintain-verification-skill` | A project-local skill drives the real app as a user does: launch, doctor, drive, evidence, cleanup, and a feature map with one file per user-facing feature.  Mocks only at a production boundary; a dry run is checked by observing what it skips.  The generated skill is run once end to end before it is handed over. |

pstack has no step that walks code by reasoning.  Every check at this stage ends in a run.

## What bts does today

- Step 5, test: `bts-test` writes `test-results.json`, stamped with `impl_hash`.
- Step 5.3, simulate: `bts-simulate code`, once per implementation (`bts-implement/SKILL.md:399-418`).
- Step 5.5, review: `bts-review` full mode, once.  Actionable criticals and majors are fixed and tests rerun (`:420-443`).
- Step 5.7, Known Uncertainties: each gets `Resolved:`, `Diverged:` or `Still-unknown:`.
- Step 6, sync, then the completion marker.  Its gates are `tests_cover_current_code`, `review_criticals_proven` and, for a build recipe, `falsifiers_green` (`internal/hook/stop.go:583`, `:723`, `:759`; `internal/hook/stop_build.go:115`).

## The prove stage

It runs after Step 5 (test) and before Step 5.5 (review), in place of Step 5.3, for every recipe type.  Each part runs once per implementation.  Its reruns are deterministic commands, not model rounds.

### P1. Able to fail

For each `INV-NNN`, the agent writes a mutation as a patch.  The patch breaks the guard the invariant names: it removes the check, reorders the step, skips the effect or does it twice.

`bts falsifier mutate <id> --inv INV-NNN --patch <file>` applies the patch in an isolated copy of the tree and runs that invariant's recorded falsifier command.  It records the patch hash, the exit code and the output tail.  The run must fail, and it must fail in that invariant's own test, not at import or collection.

Gate `falsifiers_able_to_fail`: every invariant has a recorded mutation that turned its falsifier red, on the current code hash.  An invariant with no code guard (one held by a type or by configuration) is recorded as not mutable, with the reason, and the review sees the list.

### P2. Drive the app

Once per project, `/bts-verification-skill` generates `.claude/skills/verify-<app>/`, in the shape of pstack's `create-verification-skill`: launch, doctor, drive, evidence, cleanup and `features/`.  It proves the generated skill on one feature before handing it over.

Per recipe, the user-facing rows of the wireframe map to journeys in the feature map.  `bts prove run <id> --journey <name> --cmd "<drive>"` records the exit code, the evidence paths and the code hash.

Gate `app_proven`: each journey the recipe touches has passed on the current code hash, or is deferred.  A deferral is recorded with its reason and the owner who will run it on the real target: `bts prove defer <id> --journey <name> --reason "..." --owner "..."`.  Deferrals are listed at completion and never count as passes.

On AGEN-35, the Slack-in journey needs the installed Slack app, so it defers to the live-check child.  A local journey (`DRY_RUN` on, a recorded event, the real entry point if the app has one, the real `multica` read path) can run now.

As built (2026-11-05, slice 3):

- `bts prove plan <id> --journey <name>... [--feature <file>]`, or `--no-surface "<reason>"` for a change with nothing a user drives; `bts prove run <id> --journey <name> --cmd "<drive>" --evidence <path>...` runs the drive at the project root and records the run with the code hash; it passes only with exit 0 and every evidence file present and non-empty; `bts prove defer <id> --journey <name> --reason --owner`; `bts prove status`.  Record: `prove.json`.
- Gate `app_proven` at IMPLEMENT DONE for every recipe type: every planned journey driven on the current code, or deferred; or a no-surface reason.
- Skills: `bts-verification-skill` (pstack's create-verification-skill, writing `.claude/skills/verify-<app>/` with a feature map, proven once) and `bts-prove` (plan, drive, defer); `bts-implement` Step 5.45.
- For the AGEN-35 relay, the Slack-in journey defers to the live-check child (it needs the installed Slack app and real tokens).

### P3. Interrogate

Two adversaries run on Opus, three when the change is contested or touches security.  Each works in its own isolated copy with Bash.  Each gets the scope (the code files under `CodeHash`), the intent (from `final.md`) and pstack's brief: break the change, prove each finding with a run, report the title, the exact command, the output it printed and the output the intent requires, and leave out anything not run.

With one model family, independence comes from separate contexts and from seeds by angle:

- invariants and state: order, idempotence, retries;
- inputs and boundaries: malformed events, auth, errors from external CLIs;
- optionally, operations: restart, a crash mid-step, configuration.

`bts interrogate verify <id>` reruns each finding's command in a fresh copy and records whether it reproduced.  The orchestrator merges reproduced findings that share a root cause.  Each one is fixed, and its command becomes a regression test that is red before the fix and green after.

Gate `interrogate_clean`: every reproduced finding has a regression command that passes on the current code hash, or an open decision held for the user.  Dropped findings are counted per adversary, with the reason.

As built (2026-11-05, slice 2, the four points the operator approved):

- A finding's command must exit non-zero while the code is wrong, as a falsifier does.  `bts interrogate verify` reruns it in a fresh copy of the current code with the repro files the adversary listed, and keeps it (`reproduced`) only if it fails again; it drops a finding with no command, one that passes or times out, and one whose repro files would replace the recipe's code.
- After a fix, `bts interrogate verify --recheck` reruns open and fixed findings on the current code; a passing command is `fixed`, stamped with the code hash.  `bts interrogate waive --finding --reason` leaves one unfixed with the user's reason; the skill asks the user first.
- Gate `interrogate_clean` holds IMPLEMENT DONE for every recipe type.  Until slice 5, blueprint recipes run both Step 5.3 (simulate) and Step 5.4 (interrogate).
- Pieces: agent `adversary` (`bts-adversary.md`, Opus, with Bash), skill `bts-interrogate`, `bts interrogate prepare|verify|waive`, `interrogate.json`, `bts-implement` Step 5.4.

### P4. Confidence levels

Every claim the stage reports carries a level from pstack's ladder: `L1` said, `L2` pointed (`file:line`), `L3` walked, `L4` ran (a command and its output), `L5` drove the app.  That covers review findings, Known Uncertainty resolutions, `deviation.md` items and the "works" lines of the completion summary.  Go parses the levels in the `review.md` findings block and in `deviation.md`.

- A critical needs `L4` or higher; this extends `review_criticals_proven`.
- A `Resolved:` uncertainty needs `L4` or higher.
- The completion summary states the highest level reached for each journey and each invariant.

A code-simulate pass, if kept, yields at most `L3`.

As built (2026-11-05, slice 4):

- `review.md`'s findings block takes `level` (`L1` to `L5`).  A finding that states none is read at the level its evidence shows (a run L4, a security scenario L3, an anchor L2, else L1).  `ParseReviewFindingsBlock`, used by `recipe log --action review` and `review_criticals_proven`, refuses a level outside L1-L5 and an `L4` or `L5` without `run` evidence, so a level cannot say more than the evidence.
- Known Uncertainties: `Resolved` counts only as `Resolved [L4]:` or `[L5]:` (with a qualifier: `Resolved (reads) [L4]:`).  A reasoned or unlevelled `Resolved` leaves the entry unresolved at the gate.  `Diverged` and `Still-unknown` need no level.
- The completion summary states the highest level each invariant and journey reached (skill text; no Go check).
- Not done: levels on `deviation.md` rows.

## Records and gates

| Part | Record | Gate at the completion marker |
| --- | --- | --- |
| P1 | `build-evidence.json`: a `mutations` entry per invariant | `falsifiers_able_to_fail` |
| P2 | `prove.json`: journeys, runs, deferrals | `app_proven` |
| P3 | `interrogate/<adversary>.json`, plus rerun results | `interrogate_clean` |
| P4 | levels in the `review.md` block and `deviation.md` | `review_criticals_proven` (extended) |

## Constraints and costs

- **Models.**  Adversaries run on Opus.  Go reruns every command, so no model judges reproduction.  The orchestrator writes the mutations.  No Fable.
- **Not a loop.**  Each part runs once.  Fixes are proven by their own regression commands, not by another round.
- **Isolation.**  Each adversary and each mutation needs its own copy of the tree: the files `git ls-files -co --exclude-standard` lists (decision 4).  Environment setup per copy (`uv sync`, `npm ci`) is timed and recorded, with a shared package cache.
- **Bash for adversaries.**  A new agent, `bts-adversary`, gets `Read, Grep, Glob, Bash, Write`.  The pre-tool-use hook cannot tell which agent made a Bash call, so confinement comes from the working directory and from Go's rerun in a fresh copy: a finding that only reproduces against the main tree does not survive.
- **External writes.**  Drives and adversaries run in the app's dry-run or test mode.  The verification skill checks by observation what that mode skips.  No posts or writes to real services unless the app's own isolation covers them.

## Risks

- **A mutation that breaks something else proves nothing.**  The falsifier must fail in the invariant's own test, so the record keeps the failing test's name and checks it.
- **Flaky reruns.**  A finding that reproduces in one of two reruns is reported as flaky, not dropped.
- **Deferral as the default.**  Count deferrals per recipe in metrics, and show them on the hub.
- **Cost.**  Interrogate is the expensive part.  Measure it against Step 5.3's cost and what it found.

## Slices

1. Able to fail: `bts falsifier mutate`, the gate and a skill step.  Smallest, and it can run on r-002's final code.
2. Interrogate: `bts-adversary`, the skill, `bts interrogate verify` and the gate.
3. Verification skill: the generator, `bts prove run` and `bts prove defer`, and the gate.
4. Confidence levels in the review and deviation blocks, with a Go parser.
5. Step 5.3 removed from `/bts-implement` for every type, the prove stage in its place, and the resume table (`bts-implement/SKILL.md:54-60`, which keys on `simulations/*-code.md`) rekeyed to the prove records.
6. Simulation removed from the blueprint loop and the fix recipe, the Go wiring retired along the map in "Removing simulation", and the skill, agent, rules and docs updated.

## Measuring it on AGEN-35

bts stays at `d23a572` until r-002's completion marker is accepted.  After that, on r-002's final code:

- slice 1 on its 12 invariants: how many falsifiers turn red for the invariant's reason, and how many still pass, which marks a weak test;
- slice 2 on the r-002 diff: findings reproduced and dropped, tokens and wall clock, compared with Step 5.3's pass and with `review.md`.

## Decisions

Taken by the operator on 2026-11-05.

1. Step 5.3 (code simulate) is removed from a build recipe's implementation; the prove stage replaces it completely.
2. Adversaries: two by default, three for a contested or security change.
3. `app_proven` allows a deferral recorded with its reason and owner; a deferral never counts as a pass.
4. Isolation: always a copy of what `git ls-files -co --exclude-standard` lists (a tree walk outside git), never `.git` or `.bts`.  Revised the same day before slice 1: a `git worktree` checks out HEAD and would leave out the uncommitted implementation, which is the code being proven.  `git ls-files` works in a repository with no commits and keeps virtualenvs and build output out.

5. Every recipe type: the prove stage replaces Step 5.3 of the shared `/bts-implement` for all types, with no type branch.  P1 runs where falsifiers exist; P2 to P4 run for every type.  The operator's reason: code simulation no longer adds anything once the change is proven by runs.

6. Simulation is removed from bts entirely: from the blueprint recipe's spec loop (verify, audit and simulate each round) and from the fix recipe's Step 4, which simulates the fix spec (`bts-recipe-fix/SKILL.md:220`), as well as from `/bts-implement`.  The operator's reason: simulation adds little next to checks that run.  The blueprint is no longer an unchanged comparison baseline; r-001's measurements stay in its records and in `code-first-recipe-design.md`.

## Removing simulation

What each removed step did, and what takes its place:

| Where | What simulation did | In its place |
| --- | --- | --- |
| `/bts-implement` Step 5.3 | a model walked the code paths after the tests | the prove stage (P1 to P4) |
| Blueprint spec loop | scenario walks over the spec each round, and illegal-cell coverage at DONE | verify and audit stay; illegal-cell coverage moves to a tagged falsifier row, as the build recipe already does (`CheckIllegalCellFalsifiers`) |
| Fix recipe Step 4 | three scenarios on the fix spec: the fix, a regression, a side effect | pstack's `tdd` and `blast-radius`: the reproduction becomes a test that fails before the fix, and the one fact the fix's safety rests on is shown by a run; the prove stage covers the rest after implementation |

The `bts-simulate` skill and the `bts-simulator` agent retire.  Records from past recipes stay readable.

Done 2026-11-05 (slices 5-6), along the map: verify and audit are the instruments (a recorded `simulate` is read and ignored); the `simulate_at_least_once` gate is gone; illegal cells are covered by tagged rows in final.md for every recipe type; validator checks 7c, 7d and 8, `bts sim-deviations`, `bts check test-coverage`, the simulation migrations, scenario stats and sync-check gaps are removed; deviation drivers gained `interrogate:` and `prove:`; `defend.finding_batch` replaces `simulate.finding_batch` (the old key is still read); `bts-implement` Step 5.3 is gone and its resume keys on the prove records; the blueprint loop is verify and audit; fix Step 4 reproduces first and Step 9 runs the prove stage, and FIX DONE checks interrogate_clean and app_proven; debug Step 5 runs the leading hypothesis; the skill and agent are retired (`bts update` removes them from projects).  The end-to-end script passes its 49 cases.

The full map is [`simulation-removal-map.md`](simulation-removal-map.md): 12 gate or check rows, 29 non-test Go files, 25 template files, 29 Go test files and 10 consumers of simulation output.  It found two more simulate steps, both inside decision 6: the fix recipe's Step 9, a code simulate (`bts-recipe-fix/SKILL.md:319`), and the debug recipe's Step 5 (`bts-recipe-debug/SKILL.md:284`).

### Replacements for the consumers (approved by the operator 2026-11-05)

| Consumer | Proposal |
| --- | --- |
| Completion dimensions (`VerifyDimensions`, `all_dimensions_before_final`, `converged`, `StrengthClass`, ledger closure) | Dimensions become verify and audit.  `"simulate"` stays accepted in old records and is ignored by the checks. |
| Illegal-cell coverage, every type | One rule for all types: a `[illegal-cell: ...]` tag on a falsifier or test row in `final.md`.  `CheckIllegalCellFalsifiers` and `parseIllegalCells` move out of `simulation_checker.go`.  For a blueprint, the row names the test that implementation writes, and P1 later proves it can fail. |
| Test-to-scenario mapping (`bts:scenario`, validator 7d, `bts check test-coverage`, stats) | Retired.  Tests link to invariants through falsifiers and to user paths through prove journeys. |
| Deviation ingestion at sync (Driver `simulate:<id>`, validator 7c, `bts sim-deviations`) | Drivers `interrogate:<id>` and `prove:<journey>` take its place; 7c and the command retire. |
| Sync-check "UNRESOLVED simulation gaps" | Retired. |
| `simulate.finding_batch`, which also caps `/bts-defend` | Renamed `defend.finding_batch`, with a settings migration; the rest of `simulate:` becomes a dead key. |
| Fix recipe Step 4 (fix spec) | The reproduction becomes a test that fails before the fix, and the one fact the fix's safety rests on is shown by a run (pstack `tdd`, `blast-radius`). |
| Fix recipe Step 9 (code) | The prove stage, as in `/bts-implement`. |
| Debug recipe Step 5 | A run that confirms or refutes the leading hypothesis (a repro script), in place of a walk over `draft.md`. |
| Resume and hints (`bts-implement:54-60`, `session_start.go:428-445`, `workstate.go`, `doctor.go:628`) | Keyed to the prove records. |
| `migrate simulations` and the two simulation migrations | Retired; old recipes validate with the simulation checks skipped. |
| `scripts/test-e2e.sh` fixtures, `scripts/bts-monitor.ts` | Updated with the gates. |
