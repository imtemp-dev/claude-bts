#!/usr/bin/env tsx
import { createInterface } from 'node:readline/promises';
import { stdin, stdout } from 'node:process';
import { parseTurns, printTurnStats } from '../src/extraction/transcript-parser.js';
import { writeFileSync } from 'node:fs';
import type { Turn } from '../src/extraction/types.js';

const args = process.argv.slice(2);
if (args.length === 0) {
  console.error('Usage: tsx scripts/select-turns.ts <session1.jsonl> [session2.jsonl ...]');
  console.error('  Parses sessions, shows turn table, lets you pick turns for evaluation.');
  process.exit(1);
}

const allTurns: Turn[] = [];

for (const filePath of args) {
  console.error(`\nParsing: ${filePath}`);
  const turns = await parseTurns(filePath);
  printTurnStats(turns);

  // Re-index with global offset
  const offset = allTurns.length;
  for (const t of turns) {
    t.index = offset + t.index;
    allTurns.push(t);
  }
}

console.error(`\n${'='.repeat(100)}`);
console.error(`Total turns across all sessions: ${allTurns.length}\n`);
console.error(`${'#'.padStart(4)} | ${'Signal'.padEnd(6)} | ${'Tools'.padEnd(30)} | ${'Files'.padStart(5)} | Prompt`);
console.error(`${'-'.repeat(100)}`);

for (const t of allTurns) {
  const tools = t.toolCalls.map(tc => tc.name);
  const toolSummary = summarizeTools(tools);
  const prompt = t.userPrompt.slice(0, 50).replace(/\n/g, ' ');
  console.error(`${String(t.index).padStart(4)} | ${t.turnSignal.padEnd(6)} | ${toolSummary.padEnd(30)} | ${String(t.files.length).padStart(5)} | ${prompt}`);
}

const rl = createInterface({ input: stdin, output: stdout });
const answer = await rl.question('\nEnter turn numbers to select (comma-separated, e.g. 0,3,7,12): ');
rl.close();

const indices = answer.split(',').map(s => parseInt(s.trim(), 10)).filter(n => !isNaN(n));
const selected = allTurns.filter(t => indices.includes(t.index));

if (selected.length === 0) {
  console.error('No turns selected.');
  process.exit(1);
}

const outPath = 'data/test-turns.json';
writeFileSync(outPath, JSON.stringify(selected, null, 2));
console.error(`\nWrote ${selected.length} turns to ${outPath}`);

function summarizeTools(tools: string[]): string {
  const counts: Record<string, number> = {};
  for (const t of tools) counts[t] = (counts[t] || 0) + 1;
  return Object.entries(counts).map(([k, v]) => v > 1 ? `${k}(${v})` : k).join(', ') || '(none)';
}
