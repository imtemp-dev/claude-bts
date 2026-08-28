# jig Flow Metrics

This document defines the **14 indicators** tracked by
`scripts/jig-monitor.ts` against a baseline from `scripts/jig-baseline.ts`.
Indicators 1–7 target Spec-side failure modes (Sprints 1–3,
v0.4.0). Indicators 8–14 target Implement-side failure modes (Sprints
5–7, v0.5.0).

The baseline of record (2026-04-22) is
`data/baselines/snap-voca-2026-04-22.json`. Weekly runs land in
`data/monitoring/{target}-{YYYY-MM-DD}.json`.

Run:

```bash
JIG_BIN=$(which jig) npx tsx scripts/jig-monitor.ts \
  --target /Users/jlim/Workspace/snap-voca-doc-scanner \
  --baseline data/baselines/snap-voca-2026-04-22.json \
  --out data/monitoring/snap-voca-$(date +%Y-%m-%d).json
```

---

## 1. Iteration-to-converge

**What**: mean number of verify iterations to reach
`(critical=0, major=0, minor_resolvable=0)` for the first time.

**Signal it catches**: the "patch-of-patches" loop. Each unnecessary
iteration is time spent circling instead of advancing, usually because
the spec is incomplete, the wireframe is wrong, or the decomposition is
confused.

**Baseline (snap-voca, 17 recipes)**: 3.93.

**Targets by phase**:

| After | Target |
|-------|--------|
| Phase 2 (schema contracts) | −10% → ≤ 3.54 |
| Phase 4 (domain model) | −20% → ≤ 3.14 |
| Phase 6 (cross-boundary sim) | −30% → ≤ 2.75 |

---

## 2. Re-decomposition count

**What**: mean number of `architect` action entries in a recipe's
changelog. Also tracked: count of recipes where this exceeds 1.

**Signal it catches**: the decomposition was wrong on the first attempt
and had to be redone mid-recipe. One architect invocation per recipe is
healthy (alternatives debated, selected). Two or more means the initial
debate missed something the implementation surfaced.

**Baseline**: 0 (legacy recipes predate /jig-architect).

**Target after Phase 5**: mean ≤ 1.0; zero recipes with > 1.

---

## 3. Invariant mono-owner rate

**What**: share of recipes whose `domain.md §2 Invariants` table has
zero `invariant_multiple_owners` critical from `jig verify`.

**Signal it catches**: "the truth is stored in two places at once" — the
Duolingo failure mode directly. Each violation indicates a duplicated
source-of-truth the implementation will paper over with coupling.

**Baseline**: 100% (no recipes have domain.md yet, so the denominator is
zero and the ratio defaults to 1.0 — this will fall as recipes start
adopting domain.md; a drop to 100% from new recipes is the target state).

**Target after Phase 4**: 100% for recipes with domain.md. Track the
*denominator growth* (number of recipes carrying domain.md) as a
separate uptake metric.

---

## 4. Cross-boundary simulation coverage

**What**: mean fraction of non-legacy tagged simulation scenarios that
carry `[cross-boundary: ...]` or `[illegal-cell: ...]`.

**Signal it catches**: simulations that only exercise per-module paths
and miss the interaction bugs that show up under concurrent state
changes across modules.

**Baseline**: 0.0% (all existing scenarios tagged `[single-axis: legacy]`
by `jig migrate simulations`).

**Target after Phase 6**: ≥ 30% per simulation file. Recipes below the
threshold are reported as `recipes_below_cross_boundary_threshold`.

---

## 5. Spec-code structural match

**What**: total count of `unauthorized_coupling` and
`unimplemented_dependency` findings across all recipes. Measured by
`jig graph --import` against `wireframe.md` component diagrams.

**Signal it catches**: code that silently imports modules the wireframe
did not authorize, or planned dependencies that were never wired up
during implementation. Both are signs the implementation has drifted
from the selected decomposition.

**Baseline**: 0 (measurement not yet wired through `jig-review` agent
output; placeholder zero).

**Target after Phase 6.3**: 0 unauthorized couplings per recipe.
Follow-up work (beyond this sprint): parse `review.md` for these
findings so the monitor can count them automatically instead of relying
on the reviewer's prose summary.

---

## 6. Refactor signal frequency

**What**: total signals returned by `jig refactor-signal` across all
recipes, and count of recipes where the CLI reports at least one
signal.

**Signal it catches**: the recipe's history itself shows a
patch-of-patches pattern — `test_fix_cascade` (one test → 3+ module
edits) or `cross_module_churn` (one module edited 4+ times, or 3+
modules edited 3+ times each).

**Baseline**: 0 signals across 0 recipes (none of the 17 snap-voca
recipes trigger the detectors — clean baseline).

**Target**: no increase over baseline. If this metric rises for new
recipes, the decomposition gates (Phase 4/5) are not catching the
problem in time.

---

## 7. Convergence failure rate

**What**: number of `verify-log.jsonl` entries with `status=failed`
divided by total recipes.

**Signal it catches**: recipes where the verify loop hits
`verify.max_iterations` (default 3) without ever reaching
`critical=0, major=0`. These are the recipes the user has to intervene
on manually.

**Baseline**: 0.000 (no failed status in current verify-logs).

**Target**: 0. A non-zero rate going forward is a direct signal that
Phase 4/5 gates are insufficient — the decomposition was allowed to
reach implementation despite structural issues.

---

## Reading the weekly report

`data/monitoring/{target}-{date}.json` contains:

- `indicators` — the 7 numbers above plus derived counts
- `per_recipe` — per-recipe breakdown so outliers can be traced
- `delta` — difference vs. the supplied baseline (currently tracks
  `mean_iteration_to_converge` and `convergence_failure_rate`; more
  fields as baselines grow to carry them)

When an indicator regresses (wrong direction) by more than 5%, treat it
as a real signal and investigate the responsible recipe before the
pattern spreads.

## Uptake metrics (not in the indicator set)

Distinct from the 7 indicators, track the *adoption rate* of the new
gates so we know whether a steady indicator means "healthy" or "not
yet exercised":

- Number of recipes with `domain.md`
- Number of recipes with a `<!-- architect-decision -->` block
- Number of scenarios tagged `[cross-boundary: ...]` or
  `[illegal-cell: ...]` (non-legacy)
- Count of `modify_scope: ["legacy"]` tasks awaiting manual refinement
- Count of `scenario_coverage["..."] = ["legacy"]` entries

These live in `per_recipe.*` of the monitoring report; derived counts
can be added to `indicators` when adoption becomes a tracked goal.

---

# v0.5.0 additions — Implement-side indicators

Sprints 5–7 added seven more indicators targeting the implement-side
failure modes (Q1 task decomposition, Q2 structural preservation, Q3
deviation traceability). Each comes from
`jig stats --indicators --recipe <id>` so the engine's checkers and
the TS monitor agree on definitions.

## 8. Task-anchor orphan rate (P9)

**What**: `sum(orphan_anchor + missing_anchor) / sum(task_anchor_total)`
across all recipes.

**Signal it catches**: tasks.json ↔ final.md drift. An orphan anchor
in final.md means the spec promised a file the task list does not
cover (or vice versa).

**Target**: 0. Any non-zero number indicates a recipe whose
decomposition does not derive from the spec.

## 9. Modify-scope violation rate (P14)

**What**: `sum(scope_violation + scope_symbol_missing) / sum(modify_scope_tasks)`.

**Signal**: `modify` tasks are touching code outside their declared
scope, or the declared scope references symbols the source file
doesn't actually have.

**Target**: 0. Legacy placeholders are tracked separately in the
uptake-metrics block.

## 10. Structure findings per completed task (P10)

**What**: `sum(structure_findings_total) / sum(completed_tasks)`.

**Signal**: the per-task MINI-CHECK fires frequently. High values mean
tasks are introducing import drift / owner drift / symbol regressions.

**Target**: monotonically decreasing from the baseline. A jump
suggests recipes are skipping the MINI-CHECK.

## 11. Mid-run review coverage (P11)

**What**: `sum(midrun_invocations) / sum(midrun_expected)` where
`midrun_expected` is derived from `settings.implement.midrun_review_every`.

**Signal**: implementations above the task threshold are skipping the
mid-run review.

**Target**: ≥ 1.0. Values below 1 indicate recipes bypassing the
checkpoint entirely.

## 12. Deviation driver diversity (P16)

**What**: `sum(deviation_rows_non_code_diff) / sum(deviation_rows_total)`.

**Signal**: how often deviation rows cite a driver other than
`code-diff`. Higher diversity means the pipeline is catching deviations
through multiple channels (simulate, review, test) rather than relying
only on /sync's file-by-file diff.

**Target**: rising. The v0.5.0 baseline is the starting number;
healthy adoption of P12 (sync ingests simulate) and P13 (test ↔
simulate links) should lift this.

## 13. Test-scenario link coverage (P13)

**What**: `sum(test_scenarios_linked) / sum(test_scenarios_total)`.

**Signal**: the share of simulation scenarios that have a `jig:scenario`
tag (direct linkage) or an explicit `scenario_coverage` entry.

**Target**: ≥ 0.9. `scenario_coverage[id] = ["legacy"]` entries are
counted as linked so the gate passes for migrated recipes, but the
uptake metric tracks the legacy fraction separately.

## 14. Retry-ladder tier distribution (P15)

**What**: `sum over recipes of retry_ladder_histogram`, a 7-element
array where index i is the count of tasks whose final `retry_tier`
was i. index 0 is "no retry recorded"; indices 1–5 are the ladder
tiers; index 6 is `block`.

**Signal**: distribution shape. Tasks bunching at tier 5 (architect
escalate) or 6 (block) indicate decomposition problems. Tasks that
jump straight from tier 0 to 6 indicate callers are skipping the
ladder entirely.

**Target**: majority at 0–2, very few at 3–5, zero at 6.
