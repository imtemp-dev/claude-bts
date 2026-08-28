#!/usr/bin/env tsx
// Measure jig flow health on a target project and emit the 7 indicators
// defined in docs/jig-flow-metrics.md. Designed to run weekly (or after
// each Phase completes) against a baseline captured by scripts/jig-baseline.ts.
//
// Usage:
//   tsx scripts/jig-monitor.ts --target <path> [--out <file.json>]
//                              [--baseline <baseline.json>]
//
// When --baseline is supplied, deltas are included per indicator so the
// evaluator can see trend direction.

import { execFileSync } from 'node:child_process';
import { readFileSync, readdirSync, writeFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

interface VerifyLogEntry {
  iteration: number;
  critical: number;
  major: number;
  minor?: number;
  minor_resolvable?: number;
  minor_deferred?: number;
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
  // Phase 8-16 indicators populated from `jig stats --indicators`.
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
    console.error('Usage: jig-monitor.ts --target <path> [--out file.json] [--baseline baseline.json]');
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
    // jig verify exits 1 on critical/major findings, 0 otherwise.
    // We scan stderr for the specific claim tag.
    const out = execFileSync(process.env.JIG_BIN || 'jig', ['verify', domain], {
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
    execFileSync(process.env.JIG_BIN || 'jig', ['refactor-signal', recipeID, '--json'], {
      encoding: 'utf-8',
      cwd: target,
    });
    // Non-error path: parse the returned JSON.
    const out = execFileSync(process.env.JIG_BIN || 'jig', ['refactor-signal', recipeID, '--json'], {
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

// fetchRecipeIndicators delegates to `jig stats --indicators --recipe` so
// the numbers stay consistent with what the engine's own checkers see.
// Failures fall back to undefined so the TS aggregation skips fields that
// didn't compute.
function fetchRecipeIndicators(recipeID: string, target: string): RecipeIndicators | undefined {
  try {
    const raw = execFileSync(process.env.JIG_BIN || 'jig',
      ['stats', '--indicators', '--recipe', recipeID],
      { encoding: 'utf-8', cwd: target });
    return JSON.parse(raw) as RecipeIndicators;
  } catch {
    return undefined;
  }
}

function captureRecipe(target: string, recipeID: string): RecipeSnapshot {
  const recipeDir = join(target, '.jig', 'specs', 'recipes', recipeID);
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
  const specsDir = join(target, '.jig', 'specs');
  if (!exists(specsDir)) {
    console.error(`Not a jig project: ${specsDir} missing`);
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
      `  retry ladder distribution [t1..t6]: ${indicators.retry_ladder_tier_distribution.slice(1).join(', ')}`,
  );
}

main();
