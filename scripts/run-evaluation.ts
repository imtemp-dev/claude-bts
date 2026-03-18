#!/usr/bin/env tsx
import { readFileSync } from 'node:fs';
import type { Turn, EvaluationEntry } from '../src/extraction/types.js';
import { isLowSignal } from '../src/extraction/types.js';
import { buildSystemPrompt, buildUserPrompt } from '../src/extraction/prompts.js';
import { parseExtractionResult } from '../src/extraction/parser.js';
import { callLLM, getProvider } from '../src/extraction/llm-client.js';

const turnsPath = process.argv[2] || 'data/test-turns.json';

let turns: Turn[];
try {
  turns = JSON.parse(readFileSync(turnsPath, 'utf-8')) as Turn[];
} catch {
  console.error(`Failed to read ${turnsPath}. Run 'npm run select' first.`);
  process.exit(1);
}

const provider = getProvider();
console.error(`Loaded ${turns.length} turns from ${turnsPath} (using ${provider})\n`);

const systemPrompt = buildSystemPrompt();
const results: EvaluationEntry[] = [];

for (let i = 0; i < turns.length; i++) {
  const turn = turns[i];
  const toolNames = turn.toolCalls.map(tc => tc.name).join(', ') || '(none)';
  console.error(`[${i + 1}/${turns.length}] Turn #${turn.index} | ${turn.turnSignal} | ${toolNames}`);

  const userPrompt = buildUserPrompt(turn);
  let rawXml = '';
  let parsed: EvaluationEntry['parsed'] = null;
  let parseError: string | undefined;

  try {
    rawXml = await callLLM(systemPrompt, userPrompt);
    parsed = parseExtractionResult(rawXml);
    if (!parsed) {
      parseError = 'Failed to parse XML from response';
    }
  } catch (err) {
    parseError = err instanceof Error ? err.message : String(err);
    console.error(`  ERROR: ${parseError}`);
  }

  const entry: EvaluationEntry = {
    turnIndex: turn.index,
    userPrompt: turn.userPrompt.slice(0, 100),
    toolSummary: toolNames,
    rawXml,
    parsed,
    parseError,
  };
  results.push(entry);

  // Print quick summary
  if (parsed) {
    if (isLowSignal(parsed)) {
      console.error(`  -> LOW_SIGNAL: ${parsed.reason}`);
    } else {
      console.error(`  -> ${parsed.type} (${parsed.status}): ${parsed.title}`);
    }
  } else {
    console.error(`  -> PARSE FAILED: ${parseError || 'unknown'}`);
  }

  // Rate limit courtesy
  if (i < turns.length - 1) {
    await new Promise(r => setTimeout(r, 4000));
  }
}

// Output JSON results to stdout
console.log(JSON.stringify(results, null, 2));

// Output markdown summary to stderr
console.error('\n' + '='.repeat(80));
console.error('# Extraction Evaluation Results\n');
console.error(`| # | Signal | Type | Status | Title | Parse |`);
console.error(`|---|--------|------|--------|-------|-------|`);

for (const entry of results) {
  const turn = turns.find(t => t.index === entry.turnIndex);
  const signal = turn?.turnSignal || '?';
  let type = '--';
  let status = '--';
  let title = '--';
  let parseOk = 'FAIL';

  if (entry.parsed) {
    parseOk = 'OK';
    if (isLowSignal(entry.parsed)) {
      type = 'low_signal';
      title = entry.parsed.reason.slice(0, 40);
    } else {
      type = entry.parsed.type;
      status = entry.parsed.status;
      title = entry.parsed.title.slice(0, 40);
    }
  }

  console.error(`| ${entry.turnIndex} | ${signal} | ${type} | ${status} | ${title} | ${parseOk} |`);
}

console.error(`\nParse success: ${results.filter(r => r.parsed).length}/${results.length}`);
console.error(`Low-signal detected: ${results.filter(r => r.parsed && isLowSignal(r.parsed)).length}`);
console.error(`Observations: ${results.filter(r => r.parsed && !isLowSignal(r.parsed)).length}`);
