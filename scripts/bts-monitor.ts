#!/usr/bin/env tsx
// Measure BTS flow health on a target project and emit the 7 indicators
// defined in docs/bts-flow-metrics.md. Designed to run weekly (or after
// each Phase completes) against a baseline captured by scripts/bts-baseline.ts.
//
// Usage:
//   tsx scripts/bts-monitor.ts --target <path> [--out <file.json>]
//                              [--baseline <baseline.json>]
//
// When --baseline is supplied, deltas are included per indicator so the
// evaluator can see trend direction.

import { execFileSync } from 'node:child_process';
import { readFileSync, readdirSync, writeFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import {
  H2_LIMIT,
  LATE_ROUND_FROM,
  analyzeFindings,
  dimensionSwitchSpike,
  lateRoundNewFindings,
  measureDoc,
  roundsAfterFirstFailure,
  type FindingRow,
} from './lib/doc-metrics.js';

interface VerifyLogEntry {
  iteration: number;
  critical: number;
  major: number;
  minor?: number;
  minor_resolvable?: number;
  minor_deferred?: number;
  full_pass?: boolean;
  dimensions?: string[];
  status: string;
}

interface ChangelogEntry {
  time?: string;
  action: string;
  output?: string;
  result?: string;
}

interface RecipeIndicators {
  // Phase 9 — task anchor coverage
  task_anchor_orphans: number;
  task_anchor_total: number;
  // Phase 14 — modify scope
  modify_scope_violations: number;
  modify_scope_tasks: number;
  legacy_modify_scope_tasks: number;
  // Phase 10 — per-task structural findings
  structure_findings_total: number;
  completed_tasks: number;
  // Phase 11 — mid-run review
  midrun_invocations: number;
  midrun_expected: number;
  // Phase 16 — deviation driver diversity
  deviation_rows_total: number;
  deviation_rows_non_code_diff: number;
  // Phase 13 — test scenario coverage
  test_scenarios_total: number;
  test_scenarios_linked: number;
  test_scenarios_legacy: number;
  // Phase 15 — retry ladder histogram (index 0 unused, 1..6 tiers)
  retry_ladder_histogram: number[];
}

interface RecipeSnapshot {
  id: string;
  phase: string;
  first_converge_iter: number | null;
  architect_invocations: number;
  has_domain_md: boolean;
  has_review_md: boolean;
  convergence_failures: number;
  refactor_signals: number;
  invariant_violation_count: number;
  cross_boundary_ratio: number; // NaN if no simulations
  unauthorized_coupling_count: number;
  // v0.13.0 span-side measurement — see docs/bts-flow-metrics.md 15-19.
  draft_lines: number;
  draft_h2_max_lines: number;
  draft_h2_over_limit: number;
  findings_unique: number;
  findings_density: number;
  new_findings_per_round: number[];
  late_round_new_findings: number;
  dimension_switch_spike: number | null;
  rounds_after_failed: number;
  // Phase 8-16 indicators populated from `bts stats --indicators`.
  indicators?: RecipeIndicators;
}

interface MonitoringReport {
  captured_at: string;
  target: string;
  baseline_path?: string;
  indicators: {
    // v0.4.0 indicators (7)
    mean_iteration_to_converge: number;
    median_iteration_to_converge: number;
    mean_architect_invocations: number;
    recipes_with_multiple_architect_runs: number;
    invariant_mono_owner_rate: number;
    mean_cross_boundary_ratio: number;
    recipes_below_cross_boundary_threshold: number;
    unauthorized_coupling_total: number;
    refactor_signal_total: number;
    recipes_with_signals: number;
    convergence_failure_rate: number;
    // v0.5.0 indicators (7)
    task_anchor_orphan_rate: number;               // #8  — P9
    modify_scope_violation_rate: number;            // #9  — P14
    structure_findings_per_task: number;            // #10 — P10
    midrun_review_coverage: number;                 // #11 — P11
    deviation_driver_diversity: number;             // #12 — P16
    test_scenario_link_coverage: number;            // #13 — P13
    retry_ladder_tier_distribution: number[];       // #14 — P15 aggregate
    // v0.13.0 indicators (5) — span side
    median_draft_lines: number;                     // #15
    max_draft_lines: number;                        // #15
    recipes_over_span_limit: number;                // #15
    mean_findings_density: number;                  // #16 — control, not a goal
    late_round_new_finding_share: number;           // #17
    mean_dimension_switch_spike: number;            // #18
    rounds_logged_after_failed: number;             // #19
  };
  per_recipe: RecipeSnapshot[];
  delta?: Partial<Record<keyof MonitoringReport['indicators'], number>>;
}

function parseArgs() {
  const args = process.argv.slice(2);
  let target = '';
  let out = '';
  let baseline = '';
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--target') target = args[++i];
    else if (args[i] === '--out') out = args[++i];
    else if (args[i] === '--baseline') baseline = args[++i];
  }
  if (!target) {
    console.error('Usage: bts-monitor.ts --target <path> [--out file.json] [--baseline baseline.json]');
    process.exit(2);
  }
  if (!out) {
    const date = new Date().toISOString().slice(0, 10);
    const name = target.split('/').filter(Boolean).pop() || 'project';
    out = `data/monitoring/${name}-${date}.json`;
  }
  return { target, out, baseline };
}

function exists(path: string): boolean {
  try {
    statSync(path);
    return true;
  } catch {
    return false;
  }
}

function readJsonl<T>(path: string): T[] {
  if (!exists(path)) return [];
  return readFileSync(path, 'utf-8')
    .split('\n')
    .map(l => l.trim())
    .filter(Boolean)
    .map(l => {
      try {
        return JSON.parse(l) as T;
      } catch {
        return null;
      }
    })
    .filter((e): e is T => e !== null);
}

function countInvariantViolations(recipeDir: string): number {
  const domain = join(recipeDir, 'domain.md');
  if (!exists(domain)) return 0;
  try {
    // bts verify exits 1 on critical/major findings, 0 otherwise.
    // We scan stderr for the specific claim tag.
    const out = execFileSync(process.env.BTS_BIN || 'bts', ['verify', domain], {
      encoding: 'utf-8',
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    return (out.match(/invariant_multiple_owners/g) || []).length;
  } catch (err: unknown) {
    // verify exits non-zero on findings — that's the normal case when
    // violations exist. Parse its stdout from the error object.
    const e = err as { stdout?: string | Buffer };
    const out = e.stdout ? e.stdout.toString() : '';
    return (out.match(/invariant_multiple_owners/g) || []).length;
  }
}

function crossBoundaryRatio(recipeDir: string): number {
  const simsDir = join(recipeDir, 'simulations');
  if (!exists(simsDir)) return NaN;
  let total = 0;
  let crossOrIllegal = 0;
  let legacy = 0;
  for (const f of readdirSync(simsDir)) {
    if (!f.endsWith('.md') || f.endsWith('.bak')) continue;
    const content = readFileSync(join(simsDir, f), 'utf-8');
    const lines = content.split('\n');
    for (const line of lines) {
      if (!/^(?:#{1,6}\s+.*\bscenario\b|scenario:|-\s+scenario\s+\d+)/i.test(line)) continue;
      total++;
      if (/\[cross-boundary[:\s][^\]]*\]/i.test(line) || /\[illegal-cell[:\s][^\]]*\]/i.test(line)) {
        crossOrIllegal++;
      } else if (/\[single-axis:\s*legacy\s*\]/i.test(line)) {
        legacy++;
      }
    }
  }
  const denom = total - legacy;
  if (denom <= 0) return NaN;
  return crossOrIllegal / denom;
}

function countRefactorSignals(recipeID: string, target: string): number {
  try {
    execFileSync(process.env.BTS_BIN || 'bts', ['refactor-signal', recipeID, '--json'], {
      encoding: 'utf-8',
      cwd: target,
    });
    // Non-error path: parse the returned JSON.
    const out = execFileSync(process.env.BTS_BIN || 'bts', ['refactor-signal', recipeID, '--json'], {
      encoding: 'utf-8',
      cwd: target,
    });
    const arr = JSON.parse(out || 'null');
    return Array.isArray(arr) ? arr.length : 0;
  } catch {
    return 0;
  }
}

function countArchitectInvocations(changelog: ChangelogEntry[]): number {
  return changelog.filter(e => e.action === 'architect').length;
}

// fetchRecipeIndicators delegates to `bts stats --indicators --recipe` so
// the numbers stay consistent with what the engine's own checkers see.
// Failures fall back to undefined so the TS aggregation skips fields that
// didn't compute.
function fetchRecipeIndicators(recipeID: string, target: string): RecipeIndicators | undefined {
  try {
    const raw = execFileSync(process.env.BTS_BIN || 'bts',
      ['stats', '--indicators', '--recipe', recipeID],
      { encoding: 'utf-8', cwd: target });
    return JSON.parse(raw) as RecipeIndicators;
  } catch {
    return undefined;
  }
}

function captureRecipe(target: string, recipeID: string): RecipeSnapshot {
  const recipeDir = join(target, '.bts', 'specs', 'recipes', recipeID);
  const recipeJson = (() => {
    try {
      return JSON.parse(readFileSync(join(recipeDir, 'recipe.json'), 'utf-8')) as { phase?: string };
    } catch {
      return {};
    }
  })();

  const verifyLog = readJsonl<VerifyLogEntry>(join(recipeDir, 'verify-log.jsonl'));
  const changelog = readJsonl<ChangelogEntry>(join(recipeDir, 'changelog.jsonl'));

  let firstConverge: number | null = null;
  let failures = 0;
  for (const e of verifyLog) {
    if (e.status === 'converged' && firstConverge === null) firstConverge = e.iteration;
    if (e.status === 'failed') failures++;
  }
  const roundsAfterFailed = roundsAfterFirstFailure(verifyLog);

  const draftSpan = measureDoc(join(recipeDir, 'draft.md'));
  const findings = analyzeFindings(
    readJsonl<FindingRow>(join(recipeDir, 'findings.jsonl')),
  );

  return {
    id: recipeID,
    phase: recipeJson.phase ?? 'unknown',
    first_converge_iter: firstConverge,
    architect_invocations: countArchitectInvocations(changelog),
    has_domain_md: exists(join(recipeDir, 'domain.md')),
    has_review_md: exists(join(recipeDir, 'review.md')),
    convergence_failures: failures,
    refactor_signals: countRefactorSignals(recipeID, target),
    invariant_violation_count: countInvariantViolations(recipeDir),
    cross_boundary_ratio: crossBoundaryRatio(recipeDir),
    unauthorized_coupling_count: 0, // populated when review.md parsing lands in Phase 6.3 follow-up
    draft_lines: draftSpan.lines,
    draft_h2_max_lines: draftSpan.h2_max_lines,
    draft_h2_over_limit: draftSpan.h2_over_limit,
    findings_unique: findings.unique,
    findings_density:
      draftSpan.lines > 0
        ? Number(((findings.unique / draftSpan.lines) * 100).toFixed(2))
        : 0,
    new_findings_per_round: findings.new_per_round,
    late_round_new_findings: lateRoundNewFindings(findings.new_per_round),
    dimension_switch_spike: dimensionSwitchSpike(verifyLog, findings.new_per_round),
    rounds_after_failed: roundsAfterFailed,
    indicators: fetchRecipeIndicators(recipeID, target),
  };
}

function median(xs: number[]): number {
  if (xs.length === 0) return 0;
  const s = [...xs].sort((a, b) => a - b);
  const m = Math.floor(s.length / 2);
  return s.length % 2 === 0 ? (s[m - 1] + s[m]) / 2 : s[m];
}

function main() {
  const { target, out, baseline } = parseArgs();
  const specsDir = join(target, '.bts', 'specs');
  if (!exists(specsDir)) {
    console.error(`Not a BTS project: ${specsDir} missing`);
    process.exit(1);
  }

  const recipesDir = join(specsDir, 'recipes');
  const recipeIDs = exists(recipesDir)
    ? readdirSync(recipesDir).filter(n => {
        try {
          return statSync(join(recipesDir, n)).isDirectory();
        } catch {
          return false;
        }
      })
    : [];

  const recipes: RecipeSnapshot[] = recipeIDs.map(id => captureRecipe(target, id));

  // Aggregate.
  const converges = recipes
    .map(r => r.first_converge_iter)
    .filter((x): x is number => x !== null);
  const meanIter = converges.length > 0 ? converges.reduce((a, b) => a + b, 0) / converges.length : 0;

  const architectRuns = recipes.map(r => r.architect_invocations);
  const meanArch = architectRuns.length > 0 ? architectRuns.reduce((a, b) => a + b, 0) / architectRuns.length : 0;

  const ratios = recipes.map(r => r.cross_boundary_ratio).filter(r => !isNaN(r));
  const meanCross = ratios.length > 0 ? ratios.reduce((a, b) => a + b, 0) / ratios.length : 0;

  const recipesWithDomain = recipes.filter(r => r.has_domain_md).length;
  const violations = recipes.reduce((s, r) => s + r.invariant_violation_count, 0);
  const monoRate = recipesWithDomain > 0 ? 1 - violations / recipesWithDomain : 1;

  const totalFailures = recipes.reduce((s, r) => s + r.convergence_failures, 0);
  const failureRate = recipes.length > 0 ? totalFailures / recipes.length : 0;

  const signalTotal = recipes.reduce((s, r) => s + r.refactor_signals, 0);
  const signalRecipes = recipes.filter(r => r.refactor_signals > 0).length;

  // Aggregate v0.5.0 indicators across recipes that reported data.
  const present = recipes.map(r => r.indicators).filter((x): x is RecipeIndicators => !!x);
  const sum = (get: (i: RecipeIndicators) => number) =>
    present.reduce((acc, i) => acc + get(i), 0);

  const anchorTotal = sum(i => i.task_anchor_total);
  const anchorOrphans = sum(i => i.task_anchor_orphans);
  const modifyTasks = sum(i => i.modify_scope_tasks);
  const modifyViolations = sum(i => i.modify_scope_violations);
  const completedTasks = sum(i => i.completed_tasks);
  const structureFindings = sum(i => i.structure_findings_total);
  const midrunActual = sum(i => i.midrun_invocations);
  const midrunExpected = sum(i => i.midrun_expected);
  const deviationTotal = sum(i => i.deviation_rows_total);
  const deviationNonCodeDiff = sum(i => i.deviation_rows_non_code_diff);
  const scenariosTotal = sum(i => i.test_scenarios_total);
  const scenariosLinked = sum(i => i.test_scenarios_linked);

  // v0.13.0 span-side aggregation.
  const draftLines = recipes.map(r => r.draft_lines).filter(n => n > 0);
  // Density and the switch spike are averaged only over recipes that HAVE
  // a findings ledger. The ledger arrived in v0.12; recipes finished
  // before it report zero findings, and folding those zeros in would
  // report a feature's rollout date as an improvement in the documents.
  const ledgered = recipes.filter(r => r.findings_unique > 0);
  const meanDensity =
    ledgered.length > 0
      ? ledgered.reduce((a, r) => a + r.findings_density, 0) / ledgered.length
      : 0;
  const spikes = recipes
    .map(r => r.dimension_switch_spike)
    .filter((x): x is number => x !== null);
  const meanSpike =
    spikes.length > 0 ? spikes.reduce((a, b) => a + b, 0) / spikes.length : 0;
  const allNew = recipes.reduce(
    (a, r) => a + r.new_findings_per_round.reduce((x, y) => x + y, 0),
    0,
  );
  const lateNew = recipes.reduce((a, r) => a + r.late_round_new_findings, 0);

  // Retry ladder tier distribution — element-wise sum across recipes.
  const retryAgg = new Array(7).fill(0) as number[];
  for (const i of present) {
    for (let t = 0; t < retryAgg.length && t < i.retry_ladder_histogram.length; t++) {
      retryAgg[t] += i.retry_ladder_histogram[t];
    }
  }

  const indicators = {
    // v0.4.0 (7)
    mean_iteration_to_converge: Number(meanIter.toFixed(2)),
    median_iteration_to_converge: median(converges),
    mean_architect_invocations: Number(meanArch.toFixed(2)),
    recipes_with_multiple_architect_runs: architectRuns.filter(n => n > 1).length,
    invariant_mono_owner_rate: Number(monoRate.toFixed(3)),
    mean_cross_boundary_ratio: Number(meanCross.toFixed(3)),
    recipes_below_cross_boundary_threshold: ratios.filter(r => r < 0.3).length,
    unauthorized_coupling_total: recipes.reduce((s, r) => s + r.unauthorized_coupling_count, 0),
    refactor_signal_total: signalTotal,
    recipes_with_signals: signalRecipes,
    convergence_failure_rate: Number(failureRate.toFixed(3)),
    // v0.5.0 (7)
    task_anchor_orphan_rate: anchorTotal > 0 ? Number((anchorOrphans / anchorTotal).toFixed(3)) : 0,
    modify_scope_violation_rate: modifyTasks > 0 ? Number((modifyViolations / modifyTasks).toFixed(3)) : 0,
    structure_findings_per_task: completedTasks > 0 ? Number((structureFindings / completedTasks).toFixed(3)) : 0,
    midrun_review_coverage: midrunExpected > 0 ? Number((midrunActual / midrunExpected).toFixed(3)) : 1,
    deviation_driver_diversity: deviationTotal > 0 ? Number((deviationNonCodeDiff / deviationTotal).toFixed(3)) : 0,
    test_scenario_link_coverage: scenariosTotal > 0 ? Number((scenariosLinked / scenariosTotal).toFixed(3)) : 1,
    retry_ladder_tier_distribution: retryAgg,
    // v0.13.0 (5) — span side
    median_draft_lines: median(draftLines),
    max_draft_lines: draftLines.length > 0 ? Math.max(...draftLines) : 0,
    recipes_over_span_limit: recipes.filter(r => r.draft_h2_over_limit > 0).length,
    mean_findings_density: Number(meanDensity.toFixed(2)),
    late_round_new_finding_share: allNew > 0 ? Number((lateNew / allNew).toFixed(3)) : 0,
    mean_dimension_switch_spike: Number(meanSpike.toFixed(2)),
    rounds_logged_after_failed: recipes.reduce((a, r) => a + r.rounds_after_failed, 0),
  };

  let delta: MonitoringReport['delta'] | undefined;
  if (baseline && exists(baseline)) {
    try {
      const prev = JSON.parse(readFileSync(baseline, 'utf-8')) as {
        aggregate?: {
          mean_verify_iterations?: number;
          convergence_failure_rate?: number;
        };
      };
      delta = {
        mean_iteration_to_converge:
          indicators.mean_iteration_to_converge - (prev.aggregate?.mean_verify_iterations ?? 0),
        convergence_failure_rate:
          indicators.convergence_failure_rate - (prev.aggregate?.convergence_failure_rate ?? 0),
      };
    } catch (e) {
      console.error(`Could not read baseline: ${(e as Error).message}`);
    }
  }

  const report: MonitoringReport = {
    captured_at: new Date().toISOString(),
    target,
    baseline_path: baseline || undefined,
    indicators,
    per_recipe: recipes,
    delta,
  };

  writeFileSync(out, JSON.stringify(report, null, 2));
  console.error(
    `Monitored ${recipes.length} recipes from ${target} → ${out}\n` +
      `  mean iteration-to-converge:       ${indicators.mean_iteration_to_converge}\n` +
      `  invariant mono-owner rate:        ${(indicators.invariant_mono_owner_rate * 100).toFixed(1)}%\n` +
      `  mean cross-boundary ratio:        ${(indicators.mean_cross_boundary_ratio * 100).toFixed(1)}%\n` +
      `  refactor signals:                 ${indicators.refactor_signal_total} across ${indicators.recipes_with_signals} recipe(s)\n` +
      `  convergence failure rate:         ${(indicators.convergence_failure_rate * 100).toFixed(1)}%\n` +
      `  task-anchor orphan rate:          ${(indicators.task_anchor_orphan_rate * 100).toFixed(1)}%\n` +
      `  modify-scope violation rate:      ${(indicators.modify_scope_violation_rate * 100).toFixed(1)}%\n` +
      `  structure findings per task:      ${indicators.structure_findings_per_task}\n` +
      `  mid-run review coverage:          ${(indicators.midrun_review_coverage * 100).toFixed(1)}%\n` +
      `  deviation driver diversity:       ${(indicators.deviation_driver_diversity * 100).toFixed(1)}%\n` +
      `  test-scenario link coverage:      ${(indicators.test_scenario_link_coverage * 100).toFixed(1)}%\n` +
      `  retry ladder distribution [t1..t6]: ${indicators.retry_ladder_tier_distribution.slice(1).join(', ')}\n` +
      `  draft span:                       median ${indicators.median_draft_lines}, max ${indicators.max_draft_lines} lines` +
      ` (${indicators.recipes_over_span_limit} over the ${H2_LIMIT}-line section limit)\n` +
      `  findings density:                 ${indicators.mean_findings_density} per 100 lines\n` +
      `  late-round share (round >= ${LATE_ROUND_FROM}):    ${(indicators.late_round_new_finding_share * 100).toFixed(1)}%\n` +
      `  dimension-switch spike:           ${indicators.mean_dimension_switch_spike}x\n` +
      `  rounds logged after failed:       ${indicators.rounds_logged_after_failed}`,
  );
}

main();
