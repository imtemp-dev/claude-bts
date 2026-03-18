#!/usr/bin/env tsx
import { parseTurns, printTurnStats } from '../src/extraction/transcript-parser.js';

const args = process.argv.slice(2);
const statsOnly = args.includes('--stats');
const filePath = args.find(a => !a.startsWith('--'));

if (!filePath) {
  console.error('Usage: tsx scripts/extract-turns.ts <session.jsonl> [--stats]');
  process.exit(1);
}

const turns = await parseTurns(filePath);

if (statsOnly) {
  printTurnStats(turns);
  console.error('\nTurn summaries:');
  for (const t of turns) {
    const tools = t.toolCalls.map(tc => tc.name).join(',') || '(none)';
    const prompt = t.userPrompt.slice(0, 60).replace(/\n/g, ' ');
    console.error(`  ${String(t.index).padStart(3)} | ${t.turnSignal.padEnd(6)} | ${tools.padEnd(25)} | ${prompt}`);
  }
} else {
  console.log(JSON.stringify(turns, null, 2));
}
