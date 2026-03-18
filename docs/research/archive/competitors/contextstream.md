# ContextStream — Detailed Analysis

> AI coding tool memory + semantic search + knowledge graph. Most feature-rich competitor.

- **Website**: contextstream.io
- **GitHub**: github.com/contextstream/mcp-server
- **License**: MIT
- **Stars**: 30 (as of Mar 2026)
- **MCP release**: Dec 2025
- **Commits**: 328, 103 releases

---

## Architecture

### Single-Package Structure
```
src/
├── tools.ts          — 15,460 lines, all MCP tool implementations
├── client.ts         — 6,673 lines, API client (~176 methods across 13 domains)
├── session-manager.ts — 865 lines, auto-context per MCP connection + token tracking
├── setup.ts          — 2,074 lines, interactive onboarding wizard (8+ editors)
├── hooks/            — 27 files, 6 hook events with multiple handlers per event
├── rules-templates.ts — Rule generation for Claude/Cursor/Cline
├── token-savings.ts  — Token tracking analytics
├── tool-catalog.ts   — Ultra-compact tool reference (120 tokens)
├── cache.ts          — Global memory cache
├── config.ts         — Configuration management
├── prompts.ts        — MCP prompts registry
└── resources.ts      — MCP resources
```

### Tech Stack
- **Runtime**: Node.js + TypeScript (esbuild bundling)
- **Storage**: Cloud API (no local DB — cloud-dependent)
- **Protocol**: MCP (stdio)
- **Hooks**: 27 hook files, registered via `buildHooksConfig()` across 6 Claude Code hook events (PreToolUse, UserPromptSubmit, PostToolUse, PreCompact, SessionStart, Stop, SessionEnd, SubagentStart/Stop, etc.). Some hooks are noop in runner but active via direct execution.

---

## MCP Tools (~20 Consolidated Tools, 100+ Actions)

### Consolidated Domain Tools (v0.4.x, Strategy 8 — 75% token reduction)
One multi-purpose tool per domain instead of individual CRUD operations. `CONSOLIDATED_TOOLS` Set defines 19 domain names. Three tool surface profiles: LIGHT_TOOLSET (~43), STANDARD_TOOLSET (~76), Complete (all). Auto-detected for Claude Code via `CONTEXTSTREAM_AUTO_TOOLSET`.

### Session Tools
| Tool | Purpose |
|------|---------|
| `init` | Workspace context initialization |
| `context` | Smart context per message (renamed from context_smart) |
| `capture` | Save conversation state |
| `recall` | Find saved context |
| `remember` | Quick preferences |
| `compress` | End-session compression |
| `capture_lesson` | Record mistakes for future avoidance |
| `get_lessons` | Retrieve learned lessons |
| `capture_plan` / `get_plan` / `update_plan` | Plan management |

### Search (8 Modes, 1 Tool)
| Mode | Trigger | Use Case |
|------|---------|----------|
| semantic | Natural language, 3+ words | Conceptual matching |
| hybrid | Default | Keyword + semantic combo |
| keyword | Quoted literals | Exact term matching |
| pattern | Glob/regex characters | File discovery |
| exhaustive | "all occurrences" | Refactoring impact |
| refactor | Identifier-like (camelCase, snake_case) | Safe renaming |
| team | Cross-project query | Team-wide search |
| crawl | Deep workspace-level | Comprehensive results |

**Smart mode detection**: `recommendSearchMode(query)` — 20+ conditions, auto-selects optimal mode. Hybrid → semantic fallback if confidence < 0.35.

### Graph Tools (Tiered Access)
| Tool | Tier | Available To |
|------|------|-------------|
| `graph_dependencies` | lite | Free, Pro, Elite, Team |
| `graph_impact` | lite | Free, Pro, Elite, Team |
| `graph_ingest` | lite | Free, Pro, Elite, Team |
| `graph_related` | **full** | Elite, Team only |
| `graph_decisions` | **full** | Elite, Team only |
| `graph_path` | **full** | Elite, Team only |
| `graph_call_path` | **full** | Elite, Team only |
| `graph_circular_dependencies` | **full** | Elite, Team only |
| `graph_unused_code` | **full** | Elite, Team only |
| `graph_contradictions` | **full** | Elite, Team only |

Lite constraints: module/file/path targets only, max_depth=1, no transitive analysis.

### Integration Tools (Auto-Hidden)
- **GitHub**: stats, repos, search, issues, activity, contributors, knowledge, summary
- **Slack**: stats, channels, search, discussions, activity, sync_users
- **Notion**: create_page, list_databases, search_pages, get_page, query_database, update_page, stats, activity, knowledge, summary

Auto-hidden via `CONTEXTSTREAM_AUTO_HIDE_INTEGRATIONS=true` if not connected.

---

## Data Model

### Memory Events (Cloud API)
```
MemoryEvent {
  event_id: UUID
  workspace_id: UUID
  project_id?: UUID
  event_type: "note" | "lesson" | "decision" | "session_snapshot"
  title: string
  content: string | JSON
  metadata: { tags, severity, category, keywords }
  created_at, updated_at
}
```

### Lessons
```
Lesson {
  title: string
  trigger: string         — When to surface
  prevention: string      — What to avoid
  severity: "low" | "high" | "critical"
  keywords: string[]      — For matching in user messages
  occurrence_count?: number
}
```
- 2-minute dedup window to prevent re-capturing same lesson

### Session Manager (In-Memory)
```
SessionManager {
  context: Record<string, unknown>
  sessionId: "mcp-{uuid}"
  sessionTokens: number (cumulative)
  contextThreshold: 70000 (70% of 100k window)
  conversationTurns: number
  TOKENS_PER_TURN_ESTIMATE: 3000 (heuristic)
  toolCallCount: number
  checkpointInterval: 20 (auto-save every 20 tool calls)
}
```

---

## Killer Feature: Lesson System

### Capture Flow
1. User corrects AI mistake
2. `capture_lesson` called with title, trigger, prevention, severity, keywords
3. Build signature (title + content hash) for dedup
4. Check 2-minute window → if unique, POST to `/memory/lessons`
5. Tagged with: ["lesson", category, "severity:X", ...keywords]

### Proactive Injection
1. Every `context()` call scans user message for risky keywords
2. 20+ keywords: "refactor", "delete", "deploy", "auth", "git push", etc.
3. Auto-fetch matching lessons from `/memory/high-priority-lessons`
4. Surface as warning: `[LESSONS_WARNING] Past Mistakes Found`

---

## Killer Feature: Context Pressure Management

### Token Pressure Calculation
```
estimatedTotalTokens = sessionTokens + (conversationTurns * 3000)
pressurePercent = estimatedTotalTokens / contextThreshold

> 90%: critical → suggested_action = "save_now"
> 70%: high → suggested_action = "prepare_save"
> 50%: medium
```

### Pre/Post Compaction
- **Pre-compact hook**: Parse transcript → extract active files, tool calls → build session snapshot → POST to `/memory/session_snapshot`
- **Post-compact hook**: Inject `[POST-COMPACTION CONTEXT RESTORED]` → fetch most recent snapshot → restore conversation summary, key decisions, unfinished work

---

## Novel Design Patterns

1. **First-Tool Interceptor**: SessionManager auto-initializes context on first tool call, no explicit init needed
2. **Lagging Transcript Capture**: UserPromptSubmit captures PREVIOUS exchange (not current), ensuring complete pairs
3. **Smart Mode Detection**: 20+ heuristic conditions auto-select optimal search mode
4. **Hybrid→Semantic Fallback**: If hybrid confidence < 0.35 AND semantic > hybrid + 0.08, switches automatically
5. **Ultra-Compact Tool Catalog**: 120 tokens for complete tool listing (category:tool(hint) format)
6. **Context Pack Mode**: `mode="pack"` adds AI-distilled code context at higher cost but fewer iterations

---

## Pricing Model (Feature + Capacity)

| Plan | Regular | Founding (50% off, lifetime) | Operations | Workspaces |
|------|---------|------------------------------|------------|------------|
| Free | $0 | — | 5,000 (one-time, never expires) | 2 |
| Pro | $20/mo | $10/mo | 25,000/mo | 3 |
| Elite | $30/mo | $15/mo | 100,000/mo | 10 |
| Team | $40/seat/mo | $20/seat/mo (3-seat min) | 200,000/seat/mo | 50 |

Operations top-ups: 10K=$5, 50K=$20, 250K=$75 (never expire).

### Feature Gating
| Gate | Free | Pro | Elite | Team |
|------|------|-----|-------|------|
| Integrations (GitHub/Slack/Notion) | X | O | O | O |
| Media indexing | X | O | O | O |
| AI endpoints | X | O | O | O |
| Graph lite (1-hop) | O* | O | O (full includes lite) | O (full includes lite) |
| Graph full (call path, circular deps, unused code) | X | X | **O** | **O** |
| Enhanced Context + intent detection | X | X | **O** | **O** |
| Team collaboration | X | X | X | **O** |

*Code grants Free lite graph access despite marketing saying otherwise.

### Upgrade Drivers
1. **Free→Pro**: Integrations + AI endpoints + 5x operations
2. **Pro→Elite**: **Full knowledge graph** (7 additional tools) — **the killer**
3. **Elite→Team**: Shared team graphs, admin roles, pooled operations

---

## Gaps (Context Sync Perspective)

1. **Cloud-dependent** — No local DB, all data via cloud API (`api.contextstream.io`). Offline impossible. 3 production dependencies only.
2. **No PR/commit linking** — Context connected by workspace/project/keywords, not PRs or commits. Dependency graph provides code-level links but not PR-level.
3. **No stale detection** — No diff-based validity checking. Token pressure heuristic (4 levels: low/medium/high/critical) tracks session freshness but not code freshness.
4. **No "why not" extraction** — `capture_lesson` stores mistakes with trigger/prevention, but no structured exploration/abandonment tracking.
5. **No AI-to-AI review** — No session handoff for review purposes
6. **Aggressive tool surface management** — Consolidated domain tools (Strategy 8) + 6 additional strategies (auto-hide, progressive, router, schema compact) reduce token overhead, but complexity is high (7 strategies total).
7. **Token estimation is rough** — `~4 chars/token + turns*3000` is a heuristic. Actual breakdown assumes ~500 user + ~1500 AI + ~500 system + ~1500 reasoning per turn.
8. **Risky keyword list is hardcoded** — `RISKY_ACTION_KEYWORDS` array in tools.ts, categorized but not extensible by users.
