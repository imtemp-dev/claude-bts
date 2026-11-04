# Design: land in code sooner, and let executables decide

Status: slices 1 and 2 implemented on `feat/code-first-recipe` (2026-11-04); slices 3 (model check) and 4 (prototype record) not started, and the recipe has not yet run on a real issue.  Direction decided by the operator on 2026-11-04: take pstack's approach, reach code sooner, and take truth from the compiler, prototypes, a model checker and tests instead of a simulation loop over prose.

As built, the skeleton's command is `bts check build`, the red runs are re-recorded with `bts falsifier check --red`, and the single review gate is `build_review_clean` (the last full verify pass on `final.md` has no critical or major, and `final.md` is unchanged since).  None of the four build gates is overridable: each remedy is to run something again.

## Why

**Measured on AGEN-35** (recipe `r-001-implement-agen-35`, `slack-pr-relay`, bts `a081b7e`):

| Round | Wall clock | critical / major / minor_resolvable | simulate findings |
| --- | --- | --- | --- |
| 1 | 23 min | 1 / 18 / 26 | 12 (4 major) |
| 2 | 27 min | 0 / 8 / 28 | 10 (2 major) |
| 3 | 27 min | 1 / 4 / 12 | 5 (1 major) |

Every round re-runs verify, audit and simulate.  `confirm_passes: 2` then needs two clean full passes over an unchanged revision, so the loop owes about 50 more minutes after the document is already clean.  The defense step also misfired twice: pass 2 re-defended the same twelve findings, and round 3's defense was skipped because fixed-but-unreported findings held its slots.

**bts's own record says the same.**  Two rounds over a byte-identical document disagreed by two criticals and nine majors, and finding counts tracked how hard a round looked rather than what changed (`settings.yaml`, `confirm_passes`).  `65950f5` concluded that a claim about code that does not exist has no truth value until something executes, cut the blueprint to a skeleton, and made falsifiers mandatory, but still as prose naming a test.

**pstack** sets direction with grounding and independent candidates, then gets truth from execution: the sketch is code that compiles, empirical forks are settled by throwaway prototypes, contested designs face adversaries who must run their findings, and state and ordering properties go to a model checker (TLA+ with TLC, read through `statechart`).

## What stays

- Intent, scope, domain model, research, and architect with its two runners: direction still comes from grounding and independent candidates.
- `domain.md` invariants with one owner each, illegal cells, and the wireframe's File Structure rows as task anchors.
- Implement with the retry ladder and escalate mode, test with `impl_hash`, review with run-proven criticals, sync.
- `/bts-recipe-blueprint` itself, unchanged, so the two can be compared.

## The new recipe: `/bts-recipe-build`

```
discover -> scope -> domain -> research -> architect -> wireframe
  -> skeleton -> falsify -> [model check] -> [prototype] -> one review
  -> implement -> test -> review -> sync -> complete
```

1. **Skeleton.**  The chosen decomposition written as code in the repository: the files of the File Structure rows, public types and signatures, bodies that fail loudly (`raise NotImplementedError`, `panic("not implemented")`, `throw new Error("not implemented")`), and the caller usage from the alternative's Usage section as a compiled example or test stub.  Truth: the project's typecheck or build exits 0.  A dynamic language with no type checker uses "imports cleanly and the usage stub runs to the first not-implemented".
2. **Falsify.**  Each `INV-NNN` in `domain.md` becomes a test against the skeleton's public surface, written before the bodies.  Truth: each test runs and fails now, for the reason the invariant names, and passes after implementation.  Failing on `not implemented` alone is weak evidence, so the test asserts the invariant's outcome, not just a call.
3. **Model check (when the domain is a state machine).**  When `domain.md` declares states or illegal cells, the invariants and transitions go into a TLA+ model, checked with TLC.  A counterexample is a design defect to fix before any body is written, shown as its shortest path.  Truth: TLC's exit and its report.  Rerun is seconds, so it reruns on every change to the domain.
4. **Prototype (when an uncertainty is empirical).**  A Known Uncertainty whose answer a run would give (`Opens-with:`) is run now as a throwaway in a scratch directory, and its answer is recorded, instead of being deferred to implementation or argued in rounds.
5. **One review of the skeleton.**  A single pass, no loop and no confirm passes, on what only judgment can see: invariant ownership, boundary contracts, irreversible order and its rollback, scope.  A contested design may get adversaries who must run their findings (pstack's `interrogate`).  Simulate is not part of this recipe; it stays available by hand.
6. **Implement** fills the bodies until the falsifier tests and the rest go green, in small verifiable units.

## Enforcement in Go

Each step leaves a record that Go reads, so the recipe binds rather than asks:

| Step | Record | Gate before implement |
| --- | --- | --- |
| Skeleton | `bts check build <id> --cmd "<typecheck>"`: exit code and `impl_hash`, changelog `skeleton` | `skeleton_builds`: the last build record passed on the current `impl_hash` |
| Falsify | `bts falsifier run <id> --inv INV-NNN --cmd "<test>"`: expects a failing exit now, records it | `falsifiers_red`: every invariant has a recorded failing run |
| Model check | `bts model check <id> --spec <tla> --config <cfg>`: TLC exit and counterexamples | `model_checked`: required only when the domain declares states |
| Prototype | `bts recipe uncertainty run <id> U-NNN`: the command and its output | `uncertainties_run`: every `Opens-with:` uncertainty has a run |

At completion, `falsifiers_green` requires the same falsifier commands to pass on the final `impl_hash`, beside the existing `tests_cover_current_code` and `review_criticals_proven`.

## Risks

- **The model is not the code.**  pstack's rule applies: every part of the model points to code a reviewer can open, and a property is shown failing on a broken variant before it is trusted.
- **Tooling.**  TLC needs Java; pstack installs it with `mise` on first use.  The model check step is required only for state-machine domains, and a project without the tool records why it was skipped.
- **Breadth.**  The prose loop caught scope and security gaps as well as state bugs.  The single skeleton review keeps those questions; the final code review (with run-proven criticals) catches the rest on real code.
- **Weak red.**  A test that fails only because a body is missing proves little.  The falsify step's acceptance is a failure for the invariant's own reason, judged in the single review.

## Measuring it

Run `/bts-recipe-build` on the next suitable issues and compare with AGEN-35's blueprint run: wall clock and tokens to the first green falsifier set, review findings on real code, defects found after completion, and the number of design changes the model check forced before implementation.

## Slices

1. **Recipe and templates, advisory:** `bts-recipe-build`, `bts-skeleton`, `bts-falsify`, `bts-prototype`, a changed single-review mode; no Go gates yet.  Enough to run one issue end to end.
2. **Go records and gates:** `bts check build`, `bts falsifier run`, `skeleton_builds`, `falsifiers_red`, `falsifiers_green`.
3. **Model check:** `bts-model-check` skill (TLA+ from `domain.md`, TLC, counterexamples as paths), `bts model check`, `model_checked`.
4. **Prototype record:** `bts recipe uncertainty run`, `uncertainties_run`.

## Found on the first real run (AGEN-35, r-002)

- **Red on a skeleton proves less than intended.**  Every falsifier fails at the skeleton's `NotImplementedError`, so "fails for the invariant's own reason" cannot be checked at the red step, and `bts falsifier run` records only the exit code.  Candidate next slice, pstack's "able to fail": once a falsifier is green, break the guard that enforces its invariant (or let the agent name a mutation) and require the falsifier to go red again, recorded as `bts falsifier mutate`.
- Fixed during the run: `bts validate` asked a build recipe for simulation scenarios per illegal cell (now: a `[illegal-cell: …]` tag on a falsifier row in `final.md`), and the skeleton skill's log step retyped `wireframe.md` in the manifest.

## Open decisions

1. A new recipe beside the blueprint (proposed, additive and comparable), or a change to the blueprint itself.
2. TLA+ with TLC as the model checker, with Java as an optional dependency.
3. Whether slice 1 runs on its own first, or slices 1 and 2 land together.
