#!/usr/bin/env tsx
// Capture a snapshot of a BTS-managed project for regression monitoring.
//
// Usage:
//   tsx scripts/bts-baseline.ts --target <path> --out <file.json>
//
// Records per-recipe verify-log iteration counts, convergence outcomes,
// phase progression, and presence/absence of downstream artifacts.
// Output is structured JSON so later runs (scripts/bts-monitor.ts) can
// diff against it without re-parsing the source project.

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
  info?: number;
  doc?: string;
  full_pass?: boolean;
  dimensions?: string[];
  status: string;
  timestamp: string;
}

interface ChangelogEntry {
  time?: string;
  timestamp?: string;
  action: string;
  output?: string;
  result?: string;
}

interface RecipeSnapshot {
  id: string;
  type: string;
  phase: string;
  iteration: number;
  level: number;
  started_at: string;
  updated_at: string;
  // derived metrics
  verify_iterations: number;
  first_converge_iter: number | null;
  last_entry_critical: number;
  last_entry_major: number;
  last_entry_minor_resolvable: number;
  last_entry_minor_deferred: number;
  legacy_minor_only: boolean; // pre-split format
  has_domain_md: boolean;
  has_wireframe_md: boolean;
  has_final_md: boolean;
  has_verification_md: boolean;
  has_review_md: boolean;
  has_deviation_md: boolean;
  has_tasks_json: boolean;
  has_test_results_json: boolean;
  changelog_action_counts: Record<string, number>;
  convergence_failures: number; // status=failed entries

  // Span and density.
  //
  // engine/section_span_checker.go measured section length against
  // findings at r=+0.95, ~12.7 findings per 100 lines. A long document
  // is not a document that happened to need many rounds; it is a
  // document that already held those findings before anyone looked, and
  // the completion gate asks for zero. Span is therefore the only lever
  // on the loop's total cost that is knowable before the loop starts —
  // so it is measured here, per recipe, from the baseline forward.
  draft_lines: number;
  final_lines: number;
  draft_h2_sections: number;
  draft_h2_max_lines: number;
  draft_h2_over_limit: number;
  draft_fence_lines: number;
  findings_rows: number;
  findings_unique: number;
  findings_by_severity: Record<string, number>;
  findings_density: number; // unique findings per 100 draft lines
  new_findings_per_round: number[];
  late_round_new_findings: number; // first seen at round >= LATE_ROUND_FROM
  dimension_switch_spike: number | null;
  rounds_after_failed: number; // rounds logged after the budget first fired
}

interface Baseline {
  captured_at: string;
  target: string;
  project_status_present: boolean;
  roadmap_present: boolean;
  project_map_present: boolean;
  recipes: RecipeSnapshot[];
  aggregate: {
    total_recipes: number;
    recipes_by_phase: Record<string, number>;
    recipes_by_type: Record<string, number>;
    mean_verify_iterations: number;
    median_verify_iterations: number;
    legacy_minor_format_count: number;
    convergence_failure_rate: number;
    median_draft_lines: number;
    max_draft_lines: number;
    recipes_over_span_limit: number; // any H2 section beyond H2_LIMIT
    mean_findings_density: number; // unique findings per 100 draft lines
    late_round_new_finding_share: number; // share first seen at round >= LATE_ROUND_FROM
    mean_dimension_switch_spike: number;
    rounds_logged_after_failed: number;
  };
}

function parseArgs(): { target: string; out: string } {
  const args = process.argv.slice(2);
  let target = '';
  let out = '';
  for (let i = 0; i < args.length; i++) {
    if (args[i] === '--target') target = args[++i];
    else if (args[i] === '--out') out = args[++i];
  }
  if (!target) {
    console.error('Usage: bts-baseline.ts --target <path> --out <file.json>');
    process.exit(2);
  }
  if (!out) {
    const date = new Date().toISOString().slice(0, 10);
    const name = target.split('/').filter(Boolean).pop() || 'project';
    out = `data/baselines/${name}-${date}.json`;
  }
  return { target, out };
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
  const content = readFileSync(path, 'utf-8');
  return content
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

function readJson<T>(path: string): T | null {
  if (!exists(path)) return null;
  try {
    return JSON.parse(readFileSync(path, 'utf-8')) as T;
  } catch {
    return null;
  }
}

function captureRecipe(recipeDir: string, id: string): RecipeSnapshot | null {
  const recipeJson = readJson<{
    id: string;
    type: string;
    phase: string;
    iteration?: number;
    level?: number;
    started_at?: string;
    updated_at?: string;
  }>(join(recipeDir, 'recipe.json'));

  if (!recipeJson) return null;

  const verifyLog = readJsonl<VerifyLogEntry>(join(recipeDir, 'verify-log.jsonl'));
  const changelog = readJsonl<ChangelogEntry>(join(recipeDir, 'changelog.jsonl'));

  let firstConverge: number | null = null;
  let convergenceFailures = 0;
  for (const entry of verifyLog) {
    if (entry.status === 'converged' && firstConverge === null) {
      firstConverge = entry.iteration;
    }
    if (entry.status === 'failed') convergenceFailures++;
  }

  const last = verifyLog[verifyLog.length - 1];
  const legacyOnly =
    last !== undefined &&
    last.minor_resolvable === undefined &&
    last.minor_deferred === undefined;

  const actionCounts: Record<string, number> = {};
  for (const c of changelog) {
    if (c.action) actionCounts[c.action] = (actionCounts[c.action] || 0) + 1;
  }

  const draftSpan = measureDoc(join(recipeDir, 'draft.md'));
  const finalSpan = measureDoc(join(recipeDir, 'final.md'));
  const findings = analyzeFindings(
    readJsonl<FindingRow>(join(recipeDir, 'findings.jsonl')),
  );
  const lateNew = lateRoundNewFindings(findings.new_per_round);

  // Legacy minor maps conservatively to resolvable for monitoring.
  const lastResolvable =
    last?.minor_resolvable ?? (last?.minor !== undefined ? last.minor : 0);
  const lastDeferred = last?.minor_deferred ?? 0;

  return {
    id,
    type: recipeJson.type ?? 'unknown',
    phase: recipeJson.phase ?? 'unknown',
    iteration: recipeJson.iteration ?? 0,
    level: recipeJson.level ?? 0,
    started_at: recipeJson.started_at ?? '',
    updated_at: recipeJson.updated_at ?? '',
    verify_iterations: verifyLog.length,
    first_converge_iter: firstConverge,
    last_entry_critical: last?.critical ?? 0,
    last_entry_major: last?.major ?? 0,
    last_entry_minor_resolvable: lastResolvable,
    last_entry_minor_deferred: lastDeferred,
    legacy_minor_only: legacyOnly,
    has_domain_md: exists(join(recipeDir, 'domain.md')),
    has_wireframe_md: exists(join(recipeDir, 'wireframe.md')),
    has_final_md: exists(join(recipeDir, 'final.md')),
    has_verification_md: exists(join(recipeDir, 'verification.md')),
    has_review_md: exists(join(recipeDir, 'review.md')),
    has_deviation_md: exists(join(recipeDir, 'deviation.md')),
    has_tasks_json: exists(join(recipeDir, 'tasks.json')),
    has_test_results_json: exists(join(recipeDir, 'test-results.json')),
    changelog_action_counts: actionCounts,
    convergence_failures: convergenceFailures,
    draft_lines: draftSpan.lines,
    final_lines: finalSpan.lines,
    draft_h2_sections: draftSpan.h2_sections,
    draft_h2_max_lines: draftSpan.h2_max_lines,
    draft_h2_over_limit: draftSpan.h2_over_limit,
    draft_fence_lines: draftSpan.fence_lines,
    findings_rows: findings.rows,
    findings_unique: findings.unique,
    findings_by_severity: findings.by_severity,
    findings_density:
      draftSpan.lines > 0
        ? Number(((findings.unique / draftSpan.lines) * 100).toFixed(2))
        : 0,
    new_findings_per_round: findings.new_per_round,
    late_round_new_findings: lateNew,
    dimension_switch_spike: dimensionSwitchSpike(
      verifyLog,
      findings.new_per_round,
    ),
    rounds_after_failed: roundsAfterFirstFailure(verifyLog),
  };
}

function median(xs: number[]): number {
  if (xs.length === 0) return 0;
  const s = [...xs].sort((a, b) => a - b);
  const mid = Math.floor(s.length / 2);
  return s.length % 2 === 0 ? (s[mid - 1] + s[mid]) / 2 : s[mid];
}

function main() {
  const { target, out } = parseArgs();

  const specsDir = join(target, '.bts', 'specs');
  if (!exists(specsDir)) {
    console.error(`Not a BTS project: ${specsDir} missing`);
    process.exit(1);
  }
  const recipesDir = join(specsDir, 'recipes');
  const recipeDirs = exists(recipesDir)
    ? readdirSync(recipesDir).filter(n => {
        try {
          return statSync(join(recipesDir, n)).isDirectory();
        } catch {
          return false;
        }
      })
    : [];

  const recipes: RecipeSnapshot[] = [];
  for (const id of recipeDirs) {
    const snap = captureRecipe(join(recipesDir, id), id);
    if (snap) recipes.push(snap);
  }

  const byPhase: Record<string, number> = {};
  const byType: Record<string, number> = {};
  let totalFailures = 0;
  for (const r of recipes) {
    byPhase[r.phase] = (byPhase[r.phase] || 0) + 1;
    byType[r.type] = (byType[r.type] || 0) + 1;
    totalFailures += r.convergence_failures;
  }

  const iters = recipes
    .map(r => r.first_converge_iter)
    .filter((x): x is number => x !== null);
  const meanIter =
    iters.length > 0 ? iters.reduce((a, b) => a + b, 0) / iters.length : 0;

  const draftLines = recipes.map(r => r.draft_lines).filter(n => n > 0);
  // Density is averaged only over recipes that HAVE a findings ledger.
  // The ledger arrived in v0.12; recipes finished before it report zero
  // findings, and folding those zeros in would report the feature's
  // rollout date as an improvement in document quality.
  //
  // The cohort is findings_unique, not findings_rows, because that is
  // what bts-monitor.ts uses and density is unique-per-100-lines on both
  // sides. A ledger whose rows all lack an `id` is skipped by
  // analyzeFindings, so it has rows and no unique findings: counting it
  // here and not there moved mean_findings_density between capture and
  // monitoring for a reason the project never caused.
  const densities = recipes
    .filter(r => r.findings_unique > 0)
    .map(r => r.findings_density);
  const meanDensity =
    densities.length > 0
      ? densities.reduce((a, b) => a + b, 0) / densities.length
      : 0;
  const allNew = recipes.reduce(
    (a, r) => a + r.new_findings_per_round.reduce((x, y) => x + y, 0),
    0,
  );
  const lateNew = recipes.reduce((a, r) => a + r.late_round_new_findings, 0);
  const spikes = recipes
    .map(r => r.dimension_switch_spike)
    .filter((x): x is number => x !== null);
  const meanSpike =
    spikes.length > 0 ? spikes.reduce((a, b) => a + b, 0) / spikes.length : 0;

  const baseline: Baseline = {
    captured_at: new Date().toISOString(),
    target,
    project_status_present: exists(join(specsDir, 'project-status.md')),
    roadmap_present: exists(join(specsDir, 'roadmap.md')),
    project_map_present: exists(join(specsDir, 'project-map.md')),
    recipes,
    aggregate: {
      total_recipes: recipes.length,
      recipes_by_phase: byPhase,
      recipes_by_type: byType,
      mean_verify_iterations: Number(meanIter.toFixed(2)),
      median_verify_iterations: median(iters),
      legacy_minor_format_count: recipes.filter(r => r.legacy_minor_only).length,
      convergence_failure_rate:
        recipes.length > 0
          ? Number((totalFailures / recipes.length).toFixed(3))
          : 0,
      median_draft_lines: median(draftLines),
      max_draft_lines: draftLines.length > 0 ? Math.max(...draftLines) : 0,
      recipes_over_span_limit: recipes.filter(r => r.draft_h2_over_limit > 0)
        .length,
      mean_findings_density: Number(meanDensity.toFixed(2)),
      late_round_new_finding_share:
        allNew > 0 ? Number((lateNew / allNew).toFixed(3)) : 0,
      mean_dimension_switch_spike: Number(meanSpike.toFixed(2)),
      rounds_logged_after_failed: recipes.reduce(
        (a, r) => a + r.rounds_after_failed,
        0,
      ),
    },
  };

  writeFileSync(out, JSON.stringify(baseline, null, 2));
  console.error(
    `Captured ${recipes.length} recipes from ${target} → ${out}\n` +
      `  phases: ${JSON.stringify(byPhase)}\n` +
      `  mean_first_converge: ${meanIter.toFixed(2)} iterations\n` +
      `  legacy_minor_format: ${baseline.aggregate.legacy_minor_format_count}/${recipes.length}\n` +
      `  draft span: median ${baseline.aggregate.median_draft_lines}, max ${baseline.aggregate.max_draft_lines} lines` +
      ` (${baseline.aggregate.recipes_over_span_limit} recipes over the ${H2_LIMIT}-line section limit)\n` +
      `  findings density: ${baseline.aggregate.mean_findings_density} per 100 lines` +
      ` (${densities.length} recipes with a ledger)\n` +
      `  late-round share (round >= ${LATE_ROUND_FROM}): ${baseline.aggregate.late_round_new_finding_share}\n` +
      `  dimension-switch spike: ${baseline.aggregate.mean_dimension_switch_spike}x` +
      `, rounds logged after failed: ${baseline.aggregate.rounds_logged_after_failed}`,
  );
}

main();
