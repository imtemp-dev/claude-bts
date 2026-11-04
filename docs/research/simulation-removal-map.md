# Simulation removal map

Mapped on `feat/code-first-recipe` at `d23a572` (2026-11-05), read-only, for decision 6 of [`prove-stage-design.md`](prove-stage-design.md).  A Sonnet recon agent wrote the map; these claims were checked against the source: `VerifyDimensions` at `internal/state/recipe.go:418`, the completion-evidence message at `internal/engine/completion_evidence.go:102`, the simulate steps in the fix recipe (`:220`, `:319`) and the debug recipe (`:284`), and the build branch of the illegal-cell check at `internal/engine/validator.go:155`.  Line numbers drift with edits; re-grep before relying on one.

## 1. Gates that block

| Gate | Where | Applies to | Effect |
| --- | --- | --- | --- |
| `simulate_at_least_once` | `internal/hook/stop.go:492-528`; registry `internal/engine/gate_registry.go:31-36`; overridable `:315`, recipe-scoped `:354` | blueprint | "No simulate action in changelog. Rule 5 ..." |
| `all_dimensions_before_final` | `internal/engine/completion_evidence.go:102-113`; `state.VerifyDimensions` `internal/state/recipe.go:418`; `HasAllDimensions` `:469`; called from `stop.go:355`; registry `gate_registry.go:224-228` | any recipe reaching `handleSpecDone` | completion needs verify+audit+simulate |
| `replicated_clean_pass`, `full_pass_before_final` | `completion_evidence.go` (`qualifies`; also from `internal/cli/recipe_findings.go:341`) | blueprint-style | a round without all dimensions cannot qualify |
| Round status `converged` | `internal/cli/recipe.go:460-473` | `bts recipe log` | not converged unless all dimensions ran |
| Partial-dimension note | `recipe.go:670-685` | `bts recipe log` | "Run verify, audit and simulate in one batch" |
| Review precondition | `recipe.go:951-954` | entering review | warning: "no code simulation found" |
| Validator 7c | `internal/engine/validator.go:104-112` -> `CheckSimDeviationConsumption` (`simulation_deviation.go:152`); registry `sim_deviation_consumed` `gate_registry.go:170-174` | once `deviation.md` exists | `sim_deviation_unconsumed` (major) |
| Validator 7d | `validator.go:115-124` -> `CheckTestScenarioCoverage` (`test_scenario_map.go:138`); registry `test_scenario_link_required` `gate_registry.go:175-180` | once `test-results.json` exists | `scenario_orphan`, `scenario_unlinked` (critical for cross-boundary or illegal-cell), `failure_category_missing` |
| Validator 8 | `validator.go:127-153` -> `CheckSimulationScenarios` (`simulation_checker.go:47`), `CheckScenarioBudget` (`:124`) | recipes with `simulations/*.md` | scenario tags, cross-boundary ratio, floor, budget |
| Illegal-cell, non-build | `validator.go:155-173` -> `CheckIllegalCellCoverage` (`simulation_checker.go:173`) | blueprint, fix, other non-build with `domain.md` | `uncovered_illegal_cell` (critical) |
| `bts check test-coverage` | `internal/cli/check.go:126-175` | CLI | non-zero on a critical |
| Schema enums | `validator.go:329` (`SIMULATE`), `:593` (phase), `:654` (manifest type `simulation`), `:721` (action) | `bts validate` | accept, do not require |

No gate requires simulate for fix or build recipes.

## 2. Commands and Go records

- `internal/cli/sim_deviations.go`: `bts sim-deviations`, used by `/bts-sync` Step 2.5.
- `internal/cli/sync_check.go:69-76`: "UNRESOLVED simulation gaps" via `manifest.GetUnresolvedGaps` (`internal/state/manifest.go:112`); feeds `sync_check_before_final` (`stop.go:530`).
- `internal/cli/recipe.go`: `:211` hint, `:305` `--dimension` parsing, `:460-473`, `:670-685`, `:823-835` help, `:850` action `simulate` -> doc type `simulation`, `:1089` allowed phases, `:951-954`.
- `internal/cli/recipe_findings.go:23, 164-182`: `defend-batch` caps at `2 x Simulate.FindingBatch`.
- `internal/cli/migrate.go`: `migrate simulations` (`:558-660`), simulation deviation migration (`:1369-1520`), test-coverage seed (`:1589-1700`), runner `:1726`, table `:1840-1861`.
- `internal/cli/update.go:138-148, 188`: retired template list, dead settings keys.
- `internal/cli/doctor.go:258, 654` (phase weight), `:628-633` (warning "test without code simulation"); `doctor_config.go:163`; `doctor_drift.go:27`.
- `internal/cli/stats_outcomes.go:33, 81-82, 203-214`: simulate ran or skipped.
- `internal/engine/simulation_checker.go`: scenario checks; also the build recipe's `CheckIllegalCellFalsifiers` (`:200`), `parseIllegalCells` (`:274`), `illegalCellTagRe` (`:227`), which must move, not go.
- `internal/engine/simulation_patterns.go`: scenario regexes shared with `test_scenario_map.go` and `migrate.go`.
- `internal/engine/simulation_deviation.go`; `deviation_checker.go:26, 230-239` (Driver `simulate:<id>`).
- `internal/engine/test_scenario_map.go`: `bts:scenario` links; `projectRootFromRecipeDir` (`:317`) is used by `validator.go:136`.
- `internal/engine/convergence.go:29, 91, 148-258`: `StrengthClass` by dimension set.
- `internal/engine/findings_merge.go`: merges verification, audit and simulation blocks.
- `internal/engine/settings.go:17, 49-75, 176-182, 227-242`: the `Simulate` settings.
- `internal/engine/stats.go:24-26, 128-148`: scenario linkage stats.
- `internal/state/findings.go:87-91, 422-455, 543`: ledger `Dimensions`, `roundCovers`.
- `internal/state/manifest.go:30, 112-130`; `recipe.go:20, 254-272, 413-475`; `subphase.go:21-75` (`simulate-state.json`, no non-test writer); `workstate.go:34, 173, 307-333`.

## 3. Hooks

- `internal/hook/pre_tool_use.go:156-210, 244`: batch caps for `simulator`, `simulator-validator`, `simulator-rebuttal`, and `defender` (the last is not simulation).
- `internal/hook/session_start.go:251-258` (resume hint), `:428-445` (test-phase hint "run code simulation").
- `internal/hook/stop.go:492-528, 560`.

## 4. Templates (`internal/template/templates/`)

- Retire: `.claude/skills/bts-simulate/SKILL.md`, `.claude/agents/bts-simulator.md`.
- Invocations: `bts-recipe-blueprint` (`:107-156` spec loop, `:488`, `:508`, `:536`, `:569`, `:625-632`, `:791`); `bts-recipe-fix` Step 4 (`:220-231`) **and Step 9, code simulate (`:319-331`)**; `bts-implement` (`:54-60` resume, `:87-89`, `:381`, `:399-418` Step 5.3, `:448-461`, `:520`); **`bts-recipe-debug` Step 5 (`:284-292`, `:337`)**.
- Gate instructions: `bts-sync` Step 2.5 (`:66-92`, `:197`); `bts-test` (`:50`, `:91`, `:154`, `:215`); `bts-assess` (`:53`, `:89`, `:160`); `bts-sync-check` (`:27`, `:39-41`); `bts-verify` (`:257-263`); `bts-domain-model` (`:125`); rules `bts-recipe-protocol.md` (`:20`, `:29`, `:79`, `:81` rule 5, `:87`), `bts-verification-protocol.md` (`:23`, `:189-211`, `:253`, `:305`), `bts-schema.md`, `bts-document-management.md`; `.bts/config/settings.yaml` (`:93-125`, `:170-174`, `:189`).
- Passing mentions: `bts-defend`, `bts-recipe-build`, `bts-recipe-design`, `bts-recipe-analyze`, `bts-falsify`, agents `bts-defender`, `bts-diagnosis-rebuttal`.

## 5. Docs, scripts and tests

- Docs: `README.md` (`:55`, `:81`, `:98`, `:101`, `:105`, `:115`), `README.ko/ja/zh.md`, `llms.txt`, `docs/bts-flow-metrics.md`, research docs.
- Scripts: `scripts/test-e2e.sh` (simulate fixtures for stop-gate cases), `scripts/bts-monitor.ts` (`:81`, `:203`), repo skills `template-eval`, `reflect`.
- Go tests to delete or rework: `internal/engine/simulation_checker_test.go`, `simulation_compact_report_test.go`, `simulation_deviation_test.go`, `test_scenario_map_test.go`; those touching the path in `internal/hook/` (`stop_test.go`, `stop_verify_scope_test.go`, `stop_build_test.go`, `pre_tool_use_batch_test.go`), `internal/engine/` (`completion_evidence_test.go`, `convergence_class_test.go`, `convergence_test.go`, `findings_merge_test.go`, `build_illegal_cell_test.go`, others), `internal/state/` (`workstate_test.go`, `manifest_test.go`, `subphase_test.go`, `findings_test.go`), `internal/cli/` (`recipe_log_merge_test.go`, `verify_budget_test.go`, `recipe_test.go`, `stats_outcomes_test.go`, `migrate_test.go`, `update_*_test.go`, `settings_migrate_test.go`, `doctor_config_test.go`).

## 6. Consumers of simulation output

1. Completion evidence and convergence: `VerifyDimensions`, `HasAllDimensions`, `converged`, `defend-batch`, `StrengthClass`, ledger closure; old `verify-log.jsonl` and ledger entries store `"simulate"`, and `NormalizeDimensions` rejects unknown names.
2. Findings merge of `simulations/NNN.md` blocks (verify and audit keep using the merge).
3. Deviation ingestion at sync (Driver `simulate:<id>`, validator 7c, `/bts-sync` Step 2.5).
4. Test-to-scenario mapping: `bts-test` Step 2, validator 7d, `check test-coverage`, stats, migrate; with no scenarios every `bts:scenario` tag would be flagged orphan.
5. Illegal-cell coverage for non-build recipes, and `bts-domain-model:125`.
6. Sync-check "UNRESOLVED simulation gaps".
7. Resume and hints: `session_start.go:428-445`, `bts-implement:54-60`, `workstate.go`, `doctor.go:628`.
8. Outcome and linkage stats.
9. Settings: `simulate.finding_batch` also caps `/bts-defend`.
10. External: `scripts/test-e2e.sh`, `scripts/bts-monitor.ts`.

## Counts

12 gate or check rows; 29 non-test Go source files; 4 hook files; 25 template files (7 skills with an invocation or gate step); 15 doc, script and data files; 29 Go test files; 10 consumers.
