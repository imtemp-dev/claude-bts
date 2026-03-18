// --- Raw JSONL Event Types ---

export interface RawEvent {
  type: 'user' | 'assistant' | 'progress' | 'system' | 'file-history-snapshot' | 'last-prompt';
  uuid: string;
  parentUuid: string | null;
  timestamp: string;
  sessionId: string;
  cwd?: string;
  isSidechain?: boolean;
  message?: {
    role: 'user' | 'assistant';
    id?: string;
    content: string | ContentBlock[];
    stop_reason?: 'end_turn' | 'tool_use' | 'stop_sequence' | null;
  };
  toolUseResult?: Record<string, unknown>;
  sourceToolAssistantUUID?: string;
  promptId?: string;
}

export type ContentBlock =
  | { type: 'text'; text: string }
  | { type: 'thinking'; thinking: string }
  | { type: 'tool_use'; id: string; name: string; input: Record<string, unknown> }
  | { type: 'tool_result'; tool_use_id: string; content: string | ContentBlock[] }
  | { type: 'tool_reference'; tool_name: string };

// --- Extracted Turn ---

export interface Turn {
  index: number;
  userPrompt: string;
  toolCalls: ToolCall[];
  assistantResponse: string;
  timestamp: string;
  sessionId: string;
  project: string;
  files: string[];
  turnSignal: 'high' | 'medium' | 'low';
}

export interface ToolCall {
  name: string;
  input: Record<string, unknown>;
  result: string;
}

// --- Observation Types ---

export type ObservationType = 'decision' | 'constraint' | 'exploration' | 'discovery';
export type ObservationStatus = 'adopted' | 'modified' | 'abandoned';

export interface Observation {
  type: ObservationType;
  status: ObservationStatus;
  title: string;
  narrative: string;
  facts: string[];
  concepts: string[];
  files: FileRef[];
  rationale?: string;
  alternatives?: Alternative[];
  source?: string;
  impact?: string;
  approach?: string;
  outcome?: string;
  abandonmentReason?: string;
}

export interface Alternative {
  option: string;
  rejected: string;
}

export interface FileRef {
  path: string;
  action: string;
}

// --- Extraction Result ---

export interface LowSignalResult {
  lowSignal: true;
  reason: string;
}

export type ExtractionResult = Observation | LowSignalResult;

export function isLowSignal(result: ExtractionResult): result is LowSignalResult {
  return 'lowSignal' in result && result.lowSignal === true;
}

// --- Evaluation ---

export interface EvaluationEntry {
  turnIndex: number;
  userPrompt: string;
  toolSummary: string;
  rawXml: string;
  parsed: ExtractionResult | null;
  parseError?: string;
}
