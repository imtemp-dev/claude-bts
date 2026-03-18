#!/usr/bin/env tsx
import { parseTurns } from '../src/extraction/transcript-parser.js';
import { writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

const home = homedir();
const sessions = [
  { path: join(home, '.claude/projects/-Users-jlim-Workspace-mydream-backend/195d041b-7b7e-4204-a854-b22cd98bbfb5.jsonl'), tag: 'backend' },
  { path: join(home, '.claude/projects/-Users-jlim-Workspace-mydream/645d7a87-f1f4-4e58-b19f-5255fda975fc.jsonl'), tag: 'frontend' },
  { path: join(home, '.claude/projects/-Users-jlim-Workspace-context-sync/e85214a0-9370-4134-837b-2e3bfc37ccf8.jsonl'), tag: 'research' },
];

const selected: any[] = [];

for (const s of sessions) {
  const turns = await parseTurns(s.path);

  if (s.tag === 'backend') {
    if (turns[0]) selected.push({ ...turns[0], _source: s.tag });   // 문서읽기+계획 → decision
    if (turns[7]) selected.push({ ...turns[7], _source: s.tag });   // 대규모 구현 → discovery+decision
    if (turns[3]) selected.push({ ...turns[3], _source: s.tag });   // todo 확인 → low-signal
    if (turns[13]) selected.push({ ...turns[13], _source: s.tag }); // PR 리뷰 코멘트 → exploration
    if (turns[14]) selected.push({ ...turns[14], _source: s.tag }); // 리뷰 피드백 구현 → decision
  }
  if (s.tag === 'frontend') {
    if (turns[4]) selected.push({ ...turns[4], _source: s.tag });   // 타이밍 버그 → constraint
    if (turns[5]) selected.push({ ...turns[5], _source: s.tag });   // 서버재시작 → low-signal
    if (turns[8]) selected.push({ ...turns[8], _source: s.tag });   // UX 개선 → decision
  }
  if (s.tag === 'research') {
    if (turns[5]) selected.push({ ...turns[5], _source: s.tag });   // memctl 분석 → discovery
    if (turns[13]) selected.push({ ...turns[13], _source: s.tag }); // 경쟁력 분석 → decision
  }
}

// Re-index
selected.forEach((t, i) => { t.index = i; });

writeFileSync('data/test-turns.json', JSON.stringify(selected, null, 2));
console.error(`Wrote ${selected.length} turns to data/test-turns.json`);
for (const t of selected) {
  const tools = t.toolCalls.map((tc: any) => tc.name).slice(0, 5).join(',') || '(none)';
  console.error(`  #${t.index} [${t.turnSignal}] [${t._source}] ${tools} | ${t.userPrompt.slice(0, 60).replace(/\n/g, ' ')}`);
}
