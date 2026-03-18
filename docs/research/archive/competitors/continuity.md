# CONTINUITY — Detailed Analysis

> AI session state persistence + crash recovery MCP server.

- **GitHub**: duke-of-beans/CONTINUITY
- **License**: Open source
- **Status**: Small personal project, maintenance uncertain

---

## Architecture

### MCP Server (8 Tools)

| Tool | Purpose |
|------|---------|
| `continuity_save_session` | Save current session state |
| `continuity_load_session` | Restore saved session |
| `continuity_checkpoint` | Auto-checkpoint every 3-5 tool calls |
| `continuity_recover_crash` | Recover from abnormal termination |
| `continuity_log_decision` | Record decision with structured fields |
| `continuity_query_decisions` | Search decision registry |
| `continuity_handoff_quality` | Verify handoff completeness |
| `continuity_compress_context` | Compress context to save tokens |

### Storage
- Local file-based (no database)
- Simple persistence model

---

## Decision Registry (Well-Designed Structure)

```
Decision {
  category: string
  decision: string
  rationale: string
  alternatives: string[]
  impact: string
  revisit_trigger: string
}
```

This is a clean, practical schema for capturing decisions with enough context to be useful later.

---

## Key Features

### Crash Recovery
- Unique among competitors
- `continuity_checkpoint` saves state every 3-5 tool calls
- `continuity_recover_crash` restores from last checkpoint on abnormal termination
- Practical for long-running sessions that may be interrupted

### Handoff Quality Verification
- `continuity_handoff_quality` checks if session state is complete enough for handoff
- Validates: all decisions logged, context compressed, session summarized
- Prevents incomplete handoffs between sessions

### Context Compression
- `continuity_compress_context` reduces token usage
- Preserves essential information while trimming verbose content

---

## Strengths

1. **Crash recovery** — Only tool that addresses session interruption/recovery
2. **Decision schema** — Clean structure: category, decision, rationale, alternatives, impact, revisit_trigger
3. **Handoff verification** — Quality gate for session handoffs
4. **Lightweight** — Single MCP server, minimal setup

---

## Gaps (Context Sync Perspective)

1. **Personal only** — No team sharing
2. **No auto-capture** — Agent must explicitly call tools
3. **No PR/code linking** — No git integration
4. **No semantic search** — No search capability at all
5. **No stale detection** — No code change awareness
6. **No refinement** — Manual decision logging only
7. **File-based storage** — No database, limited scalability
8. **Uncertain maintenance** — Small personal project

---

## Key Takeaway

CONTINUITY's two novel ideas worth considering:

1. **Crash recovery / checkpointing** — Practical concern that no other tool addresses. Context Sync should consider checkpoint-based recovery for long sessions.
2. **Handoff quality verification** — A gate that checks "is this session state complete enough to hand off?" could be valuable for AI-to-AI review scenarios.

The decision schema (with alternatives and revisit_trigger) mirrors Deciduous's approach but in a simpler form.
