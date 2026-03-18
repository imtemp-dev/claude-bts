import { createReadStream } from 'node:fs';
import { createInterface } from 'node:readline';
import type { RawEvent, ContentBlock, Turn, ToolCall } from './types.js';

const SKIP_TYPES = new Set(['progress', 'file-history-snapshot', 'system', 'last-prompt']);

const SYSTEM_PREFIXES = [
  '<local-command-caveat>',
  '<command-name>',
  '<task-notification>',
  'This session is being continued from',
  '<local-command-stdout>',
];

const READ_ONLY_TOOLS = new Set(['Read', 'Glob', 'Grep', 'ToolSearch']);

const INPUT_TRUNCATE = 500;
const RESULT_TRUNCATE = 1000;

function truncate(s: string, max: number): string {
  return s.length > max ? s.slice(0, max) + '...' : s;
}

function isSystemMessage(content: string): boolean {
  return SYSTEM_PREFIXES.some(p => content.startsWith(p));
}

function getUserTextContent(event: RawEvent): string | null {
  if (event.type !== 'user') return null;
  const content = event.message?.content;
  if (typeof content === 'string' && content.trim().length > 0 && !isSystemMessage(content.trim())) {
    return content.trim();
  }
  return null;
}

function extractToolUses(content: ContentBlock[]): Array<{ id: string; name: string; input: Record<string, unknown> }> {
  const results: Array<{ id: string; name: string; input: Record<string, unknown> }> = [];
  for (const block of content) {
    if (block.type === 'tool_use') {
      results.push({ id: block.id, name: block.name, input: block.input });
    }
  }
  return results;
}

function extractToolResults(event: RawEvent): Map<string, string> {
  const results = new Map<string, string>();
  const content = event.message?.content;
  if (!Array.isArray(content)) return results;

  for (const block of content) {
    if (block.type === 'tool_result') {
      let resultText = '';
      if (typeof block.content === 'string') {
        resultText = block.content;
      } else if (Array.isArray(block.content)) {
        resultText = block.content
          .filter((b): b is { type: 'text'; text: string } => b.type === 'text')
          .map(b => b.text)
          .join('\n');
      }
      results.set(block.tool_use_id, resultText);
    }
  }

  // toolUseResult 필드도 확인 (더 풍부한 결과)
  if (event.toolUseResult) {
    const asStr = JSON.stringify(event.toolUseResult);
    // tool_result가 없는 경우에만 toolUseResult 사용
    if (results.size === 0 && event.sourceToolAssistantUUID) {
      results.set('_toolUseResult', asStr);
    }
  }

  return results;
}

function extractTextResponse(content: ContentBlock[]): string {
  return content
    .filter((b): b is { type: 'text'; text: string } => b.type === 'text')
    .map(b => b.text)
    .join('\n')
    .trim();
}

function extractFilePaths(toolCalls: ToolCall[]): string[] {
  const files = new Set<string>();
  for (const tc of toolCalls) {
    const input = tc.input;
    if (typeof input.file_path === 'string') files.add(input.file_path);
    if (typeof input.notebook_path === 'string') files.add(input.notebook_path);
    if (typeof input.path === 'string' && !input.path.includes('*')) files.add(input.path);
  }
  return [...files];
}

function classifySignal(toolCalls: ToolCall[], assistantResponse: string, userPrompt: string): 'high' | 'medium' | 'low' {
  if (toolCalls.length === 0 && userPrompt.length < 20) return 'low';
  if (toolCalls.length === 0 && assistantResponse.length < 200) return 'low';

  const hasWriteTools = toolCalls.some(tc =>
    tc.name === 'Edit' || tc.name === 'Write' || tc.name === 'NotebookEdit' ||
    tc.name === 'Agent' || tc.name === 'EnterPlanMode'
  );
  if (hasWriteTools) return 'high';

  const allReadOnly = toolCalls.every(tc => READ_ONLY_TOOLS.has(tc.name));
  if (allReadOnly && assistantResponse.length < 200) return 'low';

  if (assistantResponse.length > 500) return 'high';

  return 'medium';
}

export async function parseTurns(filePath: string): Promise<Turn[]> {
  // 1. Read and filter events
  const events: RawEvent[] = [];
  const rl = createInterface({ input: createReadStream(filePath, 'utf-8'), crlfDelay: Infinity });

  for await (const line of rl) {
    try {
      const event = JSON.parse(line) as RawEvent;
      if (SKIP_TYPES.has(event.type)) continue;
      if (event.isSidechain) continue;
      events.push(event);
    } catch {
      // skip malformed lines
    }
  }

  // 2. Identify user text prompt indices
  const userPromptIndices: number[] = [];
  for (let i = 0; i < events.length; i++) {
    if (getUserTextContent(events[i]) !== null) {
      userPromptIndices.push(i);
    }
  }

  // 3. Group into turns
  const turns: Turn[] = [];

  for (let t = 0; t < userPromptIndices.length; t++) {
    const startIdx = userPromptIndices[t];
    const endIdx = t + 1 < userPromptIndices.length ? userPromptIndices[t + 1] : events.length;
    const userEvent = events[startIdx];
    const userPrompt = getUserTextContent(userEvent)!;

    // 4. Extract tool calls and results from the range
    const pendingToolUses = new Map<string, { name: string; input: Record<string, unknown> }>();
    const toolCalls: ToolCall[] = [];
    let assistantResponse = '';

    for (let i = startIdx + 1; i < endIdx; i++) {
      const e = events[i];

      if (e.type === 'assistant' && Array.isArray(e.message?.content)) {
        const content = e.message!.content as ContentBlock[];

        // Extract tool_use blocks
        for (const tu of extractToolUses(content)) {
          pendingToolUses.set(tu.id, { name: tu.name, input: tu.input });
        }

        // Extract text response (take the last one with end_turn or longest)
        const text = extractTextResponse(content);
        if (text.length > assistantResponse.length) {
          assistantResponse = text;
        }
      }

      if (e.type === 'user' && Array.isArray(e.message?.content)) {
        // Match tool results
        const results = extractToolResults(e);
        for (const [toolUseId, resultText] of results) {
          const pending = pendingToolUses.get(toolUseId);
          if (pending) {
            toolCalls.push({
              name: pending.name,
              input: truncateInput(pending.input),
              result: truncate(resultText, RESULT_TRUNCATE),
            });
            pendingToolUses.delete(toolUseId);
          }
        }
      }
    }

    // Flush any tool_uses without matched results
    for (const [, pending] of pendingToolUses) {
      toolCalls.push({
        name: pending.name,
        input: truncateInput(pending.input),
        result: '(no result captured)',
      });
    }

    // Skip empty turns
    if (toolCalls.length === 0 && assistantResponse.length === 0) continue;

    const files = extractFilePaths(toolCalls);
    const signal = classifySignal(toolCalls, assistantResponse, userPrompt);
    const project = userEvent.cwd || '';

    turns.push({
      index: turns.length,
      userPrompt,
      toolCalls,
      assistantResponse: truncate(assistantResponse, 2000),
      timestamp: userEvent.timestamp || '',
      sessionId: userEvent.sessionId || '',
      project,
      files,
      turnSignal: signal,
    });
  }

  return turns;
}

function truncateInput(input: Record<string, unknown>): Record<string, unknown> {
  const result: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(input)) {
    if (typeof value === 'string') {
      result[key] = truncate(value, INPUT_TRUNCATE);
    } else {
      result[key] = value;
    }
  }
  return result;
}

export function printTurnStats(turns: Turn[]): void {
  const signalCounts = { high: 0, medium: 0, low: 0 };
  const toolCounts: Record<string, number> = {};
  let totalTools = 0;

  for (const turn of turns) {
    signalCounts[turn.turnSignal]++;
    for (const tc of turn.toolCalls) {
      toolCounts[tc.name] = (toolCounts[tc.name] || 0) + 1;
      totalTools++;
    }
  }

  console.error(`Turns: ${turns.length} (high: ${signalCounts.high}, medium: ${signalCounts.medium}, low: ${signalCounts.low})`);
  console.error(`Total tool calls: ${totalTools}`);
  const sorted = Object.entries(toolCounts).sort((a, b) => b[1] - a[1]);
  console.error(`Tools: ${sorted.map(([k, v]) => `${k}:${v}`).join(', ')}`);
}
