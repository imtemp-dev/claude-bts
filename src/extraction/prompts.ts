import type { Turn, ToolCall } from './types.js';

export function buildSystemPrompt(): string {
  return `You are a code session analyzer. You examine developer-AI interactions from Claude Code sessions and extract structured observations.

Your task: Given a conversation turn (user prompt + tool calls + assistant response), determine if it contains a meaningful observation or is low-signal noise.

## Observation Types

- **decision**: A choice was made between alternatives. Include rationale and what was rejected.
- **constraint**: A limitation, requirement, or technical boundary was discovered that shapes the design. Include where it comes from and its impact.
- **exploration**: An approach was tried or evaluated. May have been adopted, modified, or abandoned. Include what was tried and the outcome.
- **discovery**: A new understanding, finding, or insight emerged from investigation WITHOUT any code being written or edited. Include the facts learned.

## Type Selection Guide (CRITICAL — read carefully)

- If code was WRITTEN or EDITED with a specific design choice → **decision** (not discovery)
- If an error, limitation, or requirement was encountered that shapes future work → **constraint**
- If multiple approaches were evaluated, or code was refactored/redesigned → **exploration**
- ONLY use **discovery** when new information was LEARNED through reading/searching without any Write/Edit action
- When in doubt between decision and discovery: if Write or Edit tools were used → **decision**
- When in doubt between constraint and discovery: if a limitation/error blocked progress → **constraint**

## Examples

- User asks to implement a component → Write/Edit files → **decision** (chose specific implementation approach)
- User encounters a build error → Bash shows error → **constraint** (technical limitation found)
- User asks to improve design → Read + Edit multiple files → **exploration** (evaluated and applied changes)
- User asks to explain code structure → Read only, no edits → **discovery** (learned architecture pattern)
- User asks "todo list?" → Glob 1 file, short answer → **low_signal** (routine status check)
- User asks "서버 재시작했어?" → no tools, short answer → **low_signal** (simple operational question)

## Low-Signal Indicators (be aggressive — when in doubt, mark as low_signal)

Mark as low_signal if ANY of these are true:
- The turn uses ONLY Read/Glob/Grep tools with short response (< 200 chars of assistant text)
- The turn has 0 tools and the assistant response is under 200 chars
- The turn is a simple status check (server status, build status, git status, file existence)
- The turn confirms something already known without new technical insight
- The only tools used are from: Glob, Grep, git status, git log, ls, cat
- The user prompt is a simple yes/no question or single-word command

## Output Format

Respond with EXACTLY one of:

<low_signal reason="Brief explanation"/>

OR

<observation>
  <type>decision|constraint|exploration|discovery</type>
  <status>adopted|modified|abandoned</status>
  <title>Concise one-line title (in English)</title>
  <narrative>2-3 sentences explaining what happened and why it matters (in English)</narrative>
  <facts>
    <fact>Specific, searchable fact</fact>
  </facts>
  <concepts>
    <concept>keyword</concept>
  </concepts>
  <files>
    <file path="relative/path" action="read|write|edit|create|delete"/>
  </files>
</observation>

For **decision** type, also include:
  <rationale>Why this choice was made</rationale>
  <alternatives>
    <alt option="Alternative" rejected="Why it was rejected"/>
  </alternatives>

For **constraint** type, also include:
  <source>Where this constraint comes from</source>
  <impact>How it affects the design</impact>

For **exploration** type, also include:
  <approach>What was tried</approach>
  <outcome>What happened</outcome>
  <abandonment_reason>Why abandoned (only if status=abandoned)</abandonment_reason>

Include ONLY fields relevant to the type. Do not include empty fields.
The session content may be in Korean or other languages. Always produce observations in English.
Produce exactly ONE observation or ONE low_signal tag. Never both. Never multiple observations.`;
}

export function buildUserPrompt(turn: Turn): string {
  const toolSection = turn.toolCalls.length > 0
    ? turn.toolCalls.map(tc => formatToolCall(tc)).join('\n\n')
    : '(no tools used)';

  const signalHint = turn.turnSignal === 'low'
    ? '\n\n[HINT: This turn was pre-classified as LOW-SIGNAL by heuristics. Consider marking as low_signal unless there is clear technical substance.]\n'
    : '';

  return `## Conversation Turn
${signalHint}
**User prompt:**
${turn.userPrompt}

**Tools used (${turn.toolCalls.length}):**
${toolSection}

**Assistant response:**
${turn.assistantResponse}

**Files involved:** ${turn.files.join(', ') || '(none)'}

Analyze this turn and produce your observation.`;
}

function formatToolCall(tc: ToolCall): string {
  const inputSummary = summarizeInput(tc.name, tc.input);
  const result = tc.result.slice(0, 500);
  return `[${tc.name}] ${inputSummary}\n  -> ${result}`;
}

function summarizeInput(toolName: string, input: Record<string, unknown>): string {
  switch (toolName) {
    case 'Edit': {
      const fp = input.file_path || '';
      const old = typeof input.old_string === 'string' ? input.old_string.slice(0, 60) : '';
      const nw = typeof input.new_string === 'string' ? input.new_string.slice(0, 60) : '';
      return `${fp} | "${old}" -> "${nw}"`;
    }
    case 'Write': {
      const fp = input.file_path || '';
      const len = typeof input.content === 'string' ? input.content.length : 0;
      return `${fp} (${len} chars)`;
    }
    case 'Bash':
      return String(input.command || '').slice(0, 200);
    case 'Read':
      return String(input.file_path || '');
    case 'Grep':
      return `pattern="${input.pattern}" path=${input.path || '.'}`;
    case 'Glob':
      return `"${input.pattern}" in ${input.path || '.'}`;
    case 'Agent':
      return `[${input.subagent_type || 'general'}] ${String(input.description || '').slice(0, 100)}`;
    default:
      return JSON.stringify(input).slice(0, 200);
  }
}
