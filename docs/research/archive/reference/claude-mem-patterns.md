# claude-mem — Reference Patterns for Context Sync

> Patterns and architectural decisions from claude-mem that inform Context Sync design.
> These are conceptual references — no code reuse (license constraint).

---

## 1. Six Lifecycle Hooks (Auto-Capture Pattern)

claude-mem hooks into Claude Code's lifecycle at 6 points, achieving fully automatic capture with zero developer friction:

| Hook | Trigger | What Happens |
|------|---------|-------------|
| Setup | Plugin install | `smart-install.js` -- dependency installation, worker binary setup |
| SessionStart | Session begins (`startup\|clear\|compact`) | 3-stage pipeline: smart-install check → worker service start (`bun-runner.js`) → context injection |
| UserPromptSubmit | User types prompt | Session initialized via `session-init` handler, prompt stored (with privacy tag stripping) |
| PostToolUse | Tool executes (any tool, `*`) | Observation captured → PendingMessageStore queue → SDK/Gemini/OpenRouter agent triggered. 120s timeout |
| Stop | Response completes | Summary requested, queued for SDK processing. 120s timeout |
| SessionEnd | Session closes | Session marked complete, zombie process cleanup via ProcessRegistry. 30s timeout |

**Key insight**: The developer never calls anything. Every tool execution is automatically captured, filtered, and processed in the background. The `bun-runner.js` wrapper enables Bun runtime for SQLite performance while maintaining Node.js compatibility.

**Execution model**: All hooks run via `node bun-runner.js worker-service.cjs hook claude-code <handler>`. Exit code semantics are strict: 0=success (continue), 2=blocking error (stderr shown to user). The `hookCommand()` function in `hook-command.ts` classifies errors as transient (ECONNREFUSED, 429) or blocking (4xx, TypeError) to prevent UI disruption.

**For Context Sync**: Same pattern — hook into AI tool lifecycle, capture automatically. The capture layer should be invisible to the developer.

---

## 2. SDK Agent Compression (Structured Refinement)

Raw tool outputs (file reads, bash commands, etc.) are compressed by an SDK agent (Claude subprocess) into structured observations:

### Input (Raw)
```
tool_name: "Bash"
tool_input: {"command": "grep -r 'auth' src/"}
tool_output: {"output": "src/auth/middleware.ts:14:export function validateToken..."}
```

### Output (Refined XML)
```xml
<observation>
  <type>discovery</type>
  <title>Auth middleware uses JWT validation</title>
  <subtitle>In src/auth/middleware.ts</subtitle>
  <facts>
    <fact>validateToken exported from middleware.ts line 14</fact>
    <fact>Uses jsonwebtoken library for verification</fact>
  </facts>
  <narrative>Examined auth middleware and found JWT-based validation...</narrative>
  <concepts>
    <concept>authentication</concept>
    <concept>jwt</concept>
  </concepts>
  <files_read><file>src/auth/middleware.ts</file></files_read>
</observation>
```

### Compression Ratio
- Raw tool output: ~500-5000 tokens
- Refined observation: ~50-200 tokens
- **~10x compression** while preserving semantic value

**Key insight**: The refinement prompt determines quality. It must extract decisions, explorations, and constraints — not just summarize.

**For Context Sync**: Adopt structured extraction but with richer taxonomy — explicitly separate decisions, explorations (including abandoned approaches with reasons), and constraints. This is where we surpass claude-mem.

---

## 3. Three-Layer Search (Token-Efficient Retrieval)

### Layer 1: Search Index (~50-100 tokens/result)
```
search(query="authentication", limit=20)
→ Returns table: ID, time, type, title, token estimate
→ User scans 20 results at ~1500 total tokens
```

### Layer 2: Timeline Context
```
timeline(anchor=11131, depth_before=5, depth_after=5)
→ Returns 5 observations before + anchor + 5 after
→ Interleaved with session summaries and prompts
→ Provides chronological context around the match
```

### Layer 3: Full Fetch (only filtered IDs)
```
get_observations(ids=[11131, 10942])
→ Returns full objects: narrative, facts, concepts, files
→ ~500-1000 tokens each, but only for 2-3 selected items
```

**Key insight**: Don't return full results on search. Return just enough to decide relevance, then fetch details only for what matters. **10x token savings** vs returning everything.

**For Context Sync**: Adopt this pattern. Especially important for team-scale data where search results could be massive.

---

## 4. Atomic Transaction + Claim-Confirm (Crash Safety)

### Problem
If the process crashes between "parse observation" and "store observation", data is lost or duplicated.

### Solution: Two-Phase Pattern

**Phase 1 — Claim**: When SDK agent starts processing a pending message, message ID is added to `session.processingMessageIds[]`

**Phase 2 — Confirm**: After atomic transaction commits successfully:
1. `pendingStore.confirmProcessed(messageId)` — deletes from queue
2. `session.processingMessageIds[]` — cleared

**Atomic Transaction**:
```
BEGIN TRANSACTION
  INSERT INTO observations (all observations)
  INSERT INTO session_summaries (if summary exists)
COMMIT
```

If crash occurs:
- Before commit → transaction rolls back, no partial data
- After commit but before confirm → message reprocessed on recovery (dedup via content hash catches it)

**Content-Hash Deduplication** (verified in `observations/store.ts`):
```
hash = SHA256(memorySessionId + title + narrative).slice(0, 16)
DEDUP_WINDOW_MS = 30_000  // 30-second window — same hash within 30s = skip
```

**Claim-Confirm Pattern** (verified in `PendingMessageStore.ts`):
- `enqueue()` → status=pending
- `claimNextMessage()` → status=processing (atomic claim)
- `confirmProcessed()` → DELETE from queue
- `markFailed()` → retry count incremented, max 3 retries
- Self-healing: stale processing messages (>60s, `STALE_PROCESSING_THRESHOLD_MS`) auto-reset to pending on next claim

**For Context Sync**: Essential for team-scale reliability. Adopt atomic transactions + content-hash dedup + claim-confirm queue. Add team-level conflict resolution.

---

## 5. Edge Privacy Stripping (Security Pattern)

### Two Tag Types
- `<private>content</private>` — User manually marks content as private → stripped before storage
- `<claude-mem-context>content</claude-mem-context>` — System auto-tags injected context → prevents recursive storage

### Edge Processing Pattern
```
Hook executes (BEFORE data reaches worker)
    ↓
stripMemoryTagsFromPrompt(userPrompt)    → removes both tag types
stripMemoryTagsFromJson(toolInput)       → removes both tag types
stripMemoryTagsFromJson(toolResponse)    → removes both tag types
    ↓
Clean data sent to worker/storage
```

**Key insight**: Strip at the edge (hook layer), not at storage layer. Data that reaches the worker is already clean.

**For Context Sync**: Critical for enterprise. Extend with:
- Team-level privacy policies (certain repos/files never captured)
- PII detection (auto-strip secrets, credentials, personal data)
- Compliance controls (data residency, retention policies)

---

## 6. Chroma Granular Documents (Precise Vector Search)

### Problem
Storing an entire observation as one vector document → search matches are imprecise (the whole blob matches, but which part?)

### Solution: Split per semantic field
```
One observation (id=123) → Multiple vector documents:
  obs_123_narrative  → "Examined auth middleware and found JWT-based..."
  obs_123_fact_0     → "validateToken exported from middleware.ts line 14"
  obs_123_fact_1     → "Uses jsonwebtoken library for verification"
```

Each document has metadata linking back to the SQLite observation (sqlite_id, field_type, etc.)

**Key insight**: Searching for "jsonwebtoken" matches `obs_123_fact_1` specifically, not the entire observation blob. Much more precise semantic matching.

**For Context Sync**: Adopt granular document splitting. For team-scale, this is even more important — searching across thousands of observations needs precision.

---

## 7. Event-Driven Queue (Zero-Latency Processing)

### Problem
Polling-based processing adds latency and wastes resources.

### Solution
```
SessionManager:
  sessionQueues: Map<sessionDbId, EventEmitter>
    → Emits 'message' event when observation/summarize queued

SDKAgent:
  → Listens for 'message' events (async iterator)
  → Processes immediately on arrival
  → No polling, no sleep loops
```

**For Context Sync**: Adopt event-driven architecture. For team server, use WebSocket or SSE for real-time context updates across team members.

---

## 8. Context Injection (SessionStart Hook)

### Flow
```
SessionStart → ContextBuilder.generateContext()
  1. Query recent observations (filtered by type/concept/count from settings)
  2. Query recent summaries
  3. Extract prior assistant message from transcript
  4. Build timeline (chronological, grouped by date)
  5. Calculate token economics
  6. Render as formatted markdown
  → Return as hookSpecificOutput
  → Claude Code injects into system prompt
```

### Configurable Filters
```json
{
  "CLAUDE_MEM_OBSERVATION_TYPES": "discovery,decision",
  "CLAUDE_MEM_OBSERVATION_CONCEPTS": "architecture,refactoring",
  "CLAUDE_MEM_TOTAL_OBSERVATIONS": 50,
  "CLAUDE_MEM_SESSION_COUNT": 5,
  "CLAUDE_MEM_SHOW_LAST_MESSAGE": true
}
```

**For Context Sync**: Extend with:
- Team-sourced context (not just personal)
- PR/file relevance scoring (inject context related to currently touched files)
- Staleness verification (diff check before injection)
- Role-based filtering (developer sees code context, CX sees feature context)

---

## Summary: What to Adopt vs. Improve

| Pattern | Adopt As-Is | Improve For Context Sync |
|---------|-------------|--------------------------|
| Auto-capture via hooks | Yes | Add PR-boundary capture trigger |
| SDK Agent compression | Yes | Richer taxonomy (decisions/explorations/constraints) |
| 3-Layer search | Yes | Add team-scale + PR-linked results |
| Atomic transaction + dedup | Yes | Add team-level conflict resolution |
| Edge privacy stripping | Yes | Add enterprise PII detection, compliance |
| Granular vector docs | Yes | Same approach, team-scale index |
| Event-driven queue | Yes | WebSocket/SSE for team real-time |
| Context injection | Yes | Add staleness verification + team context |
