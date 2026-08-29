// Span-side measurement, shared by scripts/bts-baseline.ts and
// scripts/bts-monitor.ts.
//
// It lives in one file because the two scripts must agree: the baseline
// records these numbers and the monitor recomputes them, so any drift
// between two copies would show up as a trend that nothing in the
// project actually did.
//
// The prior is engine/section_span_checker.go — across one Level 3
// draft's 21 H2 sections and the 453 findings anchored to them, section
// length and finding count correlate at r=+0.95 at ~12.7 findings per
// 100 lines. Findings are largely a property of how much document there
// is, and the completion gate asks for zero of them. See
// docs/bts-flow-metrics.md indicators 15-19.

import { readFileSync, statSync } from 'node:fs';

/** engine/settings.go VerifySettings.MaxSectionLines default. */
export const H2_LIMIT = 300;

/**
 * Rounds from here on are "late". Measured on r-026, every finding that
 * named the design's direction — release ordering, a missing host
 * allowlist, a reversed contract decision, a reopened domain cell — had
 * arrived by round 4. What came after was dominated by claims an
 * execution would have settled and by contradictions the document's own
 * size created.
 */
export const LATE_ROUND_FROM = 5;

/** One document's span, measured as engine/section_span_checker.go does. */
export interface DocSpan {
  lines: number;
  h2_sections: number;
  h2_max_lines: number;
  h2_over_limit: number;
  fence_lines: number;
}

export interface FindingRow {
  id: string;
  doc?: string;
  iteration: number;
  severity: string;
  status: string;
}

export interface FindingStats {
  rows: number;
  unique: number;
  by_severity: Record<string, number>;
  new_per_round: number[]; // index 0 = round 1
}

/** The fields dimensionSwitchSpike needs from a verify-log entry. */
export interface ClassifiableRound {
  iteration: number;
  full_pass?: boolean;
  dimensions?: string[];
}

/** The field roundsAfterFirstFailure needs from a verify-log entry. */
export interface StatusRound {
  status: string;
}

/**
 * Rounds logged after the convergence budget FIRST fired, including the
 * later rounds that fired it again.
 *
 * The budget is computed in engine/convergence.go and `bts recipe log`
 * exits non-zero with [CONVERGENCE FAILED] — but stopping is left to the
 * model reading the message, so this counts how often that message was
 * read and ignored. r-026 recorded `status: failed` at rounds 13, 14, 15
 * and 16 and logged four more rounds regardless.
 */
export function roundsAfterFirstFailure(rounds: StatusRound[]): number {
  let sawFailed = false;
  let after = 0;
  for (const r of rounds) {
    if (sawFailed) after++;
    if (r.status === 'failed') sawFailed = true;
  }
  return after;
}

function exists(path: string): boolean {
  try {
    statSync(path);
    return true;
  } catch {
    return false;
  }
}

export const EMPTY_SPAN: DocSpan = {
  lines: 0,
  h2_sections: 0,
  h2_max_lines: 0,
  h2_over_limit: 0,
  fence_lines: 0,
};

/**
 * Measure a markdown document's total length and its H2 section spans.
 * Fenced code is respected, so a `##` inside a shell snippet does not
 * read as a heading.
 */
export function measureDoc(path: string): DocSpan {
  if (!exists(path)) return { ...EMPTY_SPAN };
  const raw = readFileSync(path, 'utf-8').split('\n');
  // A file ending in a newline splits to a trailing empty element that
  // is not a line of the document.
  const lines =
    raw.length > 0 && raw[raw.length - 1] === '' ? raw.slice(0, -1) : raw;

  const spans: number[] = [];
  let current: number | null = null;
  let inFence = false;
  let fenceLines = 0;
  for (const line of lines) {
    // Fence detection mirrors section_span_checker.go exactly: it trims
    // the line first and accepts both delimiters. Testing /^```/ on the
    // raw line instead meant a `~~~` document, or a fence indented
    // inside a list item, toggled inFence in Go and not here — so a `##`
    // buried in that block counted as a heading in one and not the
    // other, and the monitored h2_max_lines / h2_over_limit disagreed
    // with the number `bts verify` actually gates on.
    const trimmed = line.trim();
    const isFenceDelimiter =
      trimmed.startsWith('```') || trimmed.startsWith('~~~');
    if (isFenceDelimiter) inFence = !inFence;
    else if (inFence) fenceLines++;

    // A heading only counts as one inside prose. Fenced content is
    // skipped for DETECTION but not for COUNTING: section_span_checker.go
    // measures every line from a heading to the next one, so excluding
    // fence delimiters here would report a smaller span than the number
    // `bts verify` gates on.
    //
    // The `## ` prefix test is the Go checker's own second condition —
    // it requires a space, so `##\tTitle` is not a heading there either.
    if (
      !inFence &&
      !isFenceDelimiter &&
      line.startsWith('## ') &&
      /^##\s+\S/.test(line)
    ) {
      if (current !== null) spans.push(current);
      // Start at 1: the Go checker measures FROM the heading, so the
      // heading line is part of its own section's span.
      current = 1;
      continue;
    }
    if (current !== null) current++;
  }
  if (current !== null) spans.push(current);

  return {
    lines: lines.length,
    h2_sections: spans.length,
    h2_max_lines: spans.length > 0 ? Math.max(...spans) : 0,
    h2_over_limit: spans.filter(n => n > H2_LIMIT).length,
    fence_lines: fenceLines,
  };
}

/**
 * Summarize a findings ledger by FIRST sighting. The ledger re-emits an
 * open finding every round, so counting rows measures how long the loop
 * ran; counting first sightings measures what it actually turned up.
 */
export function analyzeFindings(rows: FindingRow[]): FindingStats {
  const first = new Map<string, FindingRow>();
  for (const r of rows) {
    if (!r.id) continue;
    if (!first.has(r.id)) first.set(r.id, r);
  }
  const bySeverity: Record<string, number> = {};
  const perRound = new Map<number, number>();
  let maxRound = 0;
  for (const r of first.values()) {
    bySeverity[r.severity] = (bySeverity[r.severity] || 0) + 1;
    perRound.set(r.iteration, (perRound.get(r.iteration) || 0) + 1);
    if (r.iteration > maxRound) maxRound = r.iteration;
  }
  const newPerRound: number[] = [];
  for (let i = 1; i <= maxRound; i++) newPerRound.push(perRound.get(i) || 0);
  return {
    rows: rows.length,
    unique: first.size,
    by_severity: bySeverity,
    new_per_round: newPerRound,
  };
}

/** New findings first seen at LATE_ROUND_FROM or later. */
export function lateRoundNewFindings(newPerRound: number[]): number {
  return newPerRound.slice(LATE_ROUND_FROM - 1).reduce((a, b) => a + b, 0);
}

/**
 * Observation makes state. bts-verification-protocol.md already records
 * that findings track how hard a round looked (r=+0.69 against subagents
 * spawned) far more than what changed in the document (r=+0.16 against
 * edits) — so pointing a NEW instrument at unchanged text should produce
 * a visible step.
 *
 * This is that step, quantified: mean new findings on rounds whose
 * measurement class differs from the previous round, over mean new
 * findings on rounds that repeated the previous class. A ratio near 1
 * means rounds are measuring the document; well above 1 means they are
 * measuring the instrument. Returns null when either side is empty or
 * the repeat rounds found nothing to divide by.
 */
export function dimensionSwitchSpike(
  rounds: ClassifiableRound[],
  newPerRound: number[],
): number | null {
  const classOf = (e: ClassifiableRound) => {
    const dims = (e.dimensions ?? []).slice().sort().join('+') || '?';
    return `${dims}/${e.full_pass ? 'full' : 'delta'}`;
  };
  const switched: number[] = [];
  const repeated: number[] = [];
  for (let i = 1; i < rounds.length; i++) {
    const n = newPerRound[rounds[i].iteration - 1];
    if (n === undefined) continue;
    if (classOf(rounds[i]) !== classOf(rounds[i - 1])) switched.push(n);
    else repeated.push(n);
  }
  if (switched.length === 0 || repeated.length === 0) return null;
  const mean = (xs: number[]) => xs.reduce((a, b) => a + b, 0) / xs.length;
  const base = mean(repeated);
  if (base === 0) return null;
  return Number((mean(switched) / base).toFixed(2));
}
