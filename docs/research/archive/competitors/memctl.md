# memctl — Detailed Analysis

> Team shared memory MCP server. Most direct competitor to Context Sync.

- **Website**: memctl.com
- **GitHub**: github.com/memctl/memctl
- **License**: Apache-2.0
- **Stars**: 11 (as of Mar 2026)
- **Founded**: London, ~2026
- **HN Show HN**: ~2 weeks ago

---

## Architecture

### Monorepo Structure
```
packages/cli/     — MCP server (TypeScript/ESM)
packages/db/      — Drizzle ORM schema for Turso (libSQL/SQLite)
packages/shared/  — Types, validators, intent classification, relevance scoring
apps/web/         — Next.js web app with 80+ API routes
plugins/memctl/   — Claude plugin configuration
```

### Tech Stack
- **Database**: Turso (SQLite via libSQL) + Drizzle ORM
- **Backend**: Next.js 14+ with TypeScript
- **Embeddings**: `@xenova/transformers` (all-MiniLM-L6-v2, 384-dim)
- **Search**: FTS5 + vector similarity (hybrid)
- **Auth**: Better Auth (GitHub OAuth + email)
- **Billing**: Stripe

---

## Data Model (28 Drizzle Tables + 1 FTS5 Virtual Table)

### Core Memory
- **`memories`** — Primary storage: key, content (max 16KB), metadata (JSON), scope (project|shared), priority (0-100), tags, relatedKeys, embedding (quantized Float32 as JSON), accessCount, helpfulCount/unhelpfulCount, pinnedAt, archivedAt, expiresAt
  - Unique constraint: (projectId, key)
- **`memory_versions`** — Version history: content snapshots, changeType (created|updated|restored), changedBy
- **`memory_locks`** — TTL-based pessimistic locking for concurrency control

### Organization
- **`users`** → **`organizations`** (owner) → **`projects`**
- **`organization_members`** — role: owner/admin/member
- **`project_members`** — optional per-project access
- **`api_tokens`** — hashed API keys with expiry

### Session & Activity
- **`session_logs`** — sessionId, branch, summary, keysRead/Written (JSON), toolsUsed
- **`activity_logs`** — granular action log (tool_call, memory_read, memory_write, memory_delete)

### Other
- `memory_snapshots`, `project_templates`, `plan_templates`, `org_memory_defaults`
- `changelog_entries`, `blog_posts`, `onboarding_responses`, `promo_codes/redemptions`
- `context_types` — custom org-scoped context schemas with JSON validation
- `admin_actions` — audit log

---

## MCP Tools (11 tools, 60+ actions)

| Tool | Actions | Purpose |
|------|---------|---------|
| **memory** | store, get, search, list, delete, update, pin, archive, bulk_get, store_safe, capacity | Core CRUD, dedup (warn/skip/merge), TTL, rate limiting |
| **memory_advanced** | link, unlink, traverse, co-access | Knowledge graph operations |
| **memory_lifecycle** | health scoring, archival policies, expired cleanup, version rollback | Maintenance |
| **context** | bootstrap, bootstrap_compact, bootstrap_delta, functionality_get/set/delete/list, context_for, budget, compose, smart_retrieve, search_org, rules_evaluate, thread | 12 built-in context types + custom |
| **context_config** | define org-scoped context types with JSON schema | Custom schema management |
| **branch** | get/set/delete branch plans, checklist tracking | Git branch-scoped context |
| **session** | end, history, claims_check, claim, rate_status | Conflict detection via claims |
| **import_export** | export (agents_md, cursorrules, json), bulk import | Bulk operations |
| **repo** | scan, scan_check, onboard | Repository scanning & indexing |
| **org** | defaults_list/set/apply, context_diff, template management | Org-wide operations |
| **activity** | memo_read, memo_leave, tool usage tracking | Activity logging |

---

## Search Implementation

### Hybrid Search (FTS5 + Vector)
- **FTS5**: Virtual table `memories_fts` (key, content, tags), auto-synced via triggers
- **Vector**: all-MiniLM-L6-v2 (384-dim), quantized Int8, stored as JSON in `memories.embedding`
- **Merge**: Reciprocal Rank Fusion — `score += 1/(k + rank + 1)` with k=60, configurable k parameter
- **Similarity threshold**: cosine > 0.3 (configurable)
- **Intent-aware boosting**: entity → FTS weight, temporal → recency sort, relationship → vector + graph, aspect → vector + priority, exploratory → balanced

### Intent Classification (5 types)
| Intent | Example | Boost |
|--------|---------|-------|
| entity | File paths, identifiers | FTS, priority |
| temporal | "recent", "latest" | Recency sort |
| relationship | "related", "depends" | Vector + graph traversal |
| aspect | "conventions", "patterns" | Vector + priority |
| exploratory | Default fallback | Balanced |

### Relevance Scoring
```
score = (priority/100) * (1 + log(1 + accessCount)) * exp(-0.03 * days) * feedbackFactor * pinBoost * 100
```
- Decay half-life: ~23 days
- Access curve: logarithmic (plateaus at ~100 accesses)
- Feedback: 0.5 (all unhelpful) to 1.5 (all helpful)
- Pin: 1.5x multiplier

---

## Pricing Model (Pure Capacity)

| Plan | Price | Projects | Members | Memories/Project | API/min |
|------|-------|----------|---------|------------------|---------|
| Free | $0 | 3 | 1 | 400 | 60 |
| Lite | $5 | 10 | 3 | 1,200 | 100 |
| Pro | $18 | 25 | 10 | 5,000 | 150 |
| Business | $59 | 100 | 30 | 10,000 | 150 |
| Scale | $149 | 150 | 100 | 25,000 | 150 |
| Enterprise | Custom | Unlimited | Unlimited | Unlimited | 150 |

- Extra seats: $8/month each
- **No feature gating** — all features available on Free
- SSO/audit logs listed on marketing site but **not implemented in code**
- Self-hosted: `SELF_HOSTED=true` → enterprise plan, all limits infinite, billing disabled

### Upgrade Drivers
1. Memory capacity wall (400 per project on Free)
2. Project count limit (3 on Free)
3. Solo-only on Free (1 member)

---

## Git Integration

- `getBranchInfo()` — `git rev-parse --abbrev-ref HEAD`
- `repo scan` — `git ls-files` for file listing
- `repo scan_check` — compare current vs stored file map
- Branch tagging in memory metadata: `branch:{branchname}`
- **No push-trigger re-indexing** — repo scan is manual
- **No commit SHA linking**
- **No PR connection**

---

## Session Conflict Detection

- **Claims mechanism**: `session claim keys=[...] ttlMinutes=60`
- Creates `agent/claims/{sessionId}` memory entry
- Before write: `session claims_check keys=[...]` queries active claims
- TTL-based auto-release (default 60 min)
- Session handoff: summary + keysRead/Written passed to next session

---

## Key Design Decisions

1. **Semi-auto capture via hooks**: `extractHookCandidates()` in `hooks.ts` performs keyword-based semantic classification (decisions, constraints, issues, lessons) from user/assistant messages. Hook adapters exist for Claude, Cursor, Windsurf, VS Code, Codex, Cline. However, extraction is heuristic (keyword-based), not LLM-refined.
2. **Agent-assisted storage**: Agent can also explicitly call write_memory for additional context. Quality varies by agent capability + hook heuristic accuracy.
3. **Context types**: 12 built-in (coding_style, architecture, testing, etc.) + custom org-scoped
4. **Capacity management**: Auto-evict lowest-health non-pinned memories when full
5. **Low-signal filter**: Blocks generic capability noise, uncontextualized shell commands, code-heavy with <10 explanatory words

---

## Gaps (Context Sync Perspective)

1. **Heuristic-only capture** — Hook-based auto-capture exists with keyword-based semantic classification (`extractHookCandidates()` in `hooks.ts` detects decisions, constraints, issues, lessons). However, this is keyword matching, not LLM refinement. Quality is inconsistent.
2. **No deep refinement** — Hook extracts categories (decision, constraint, lesson) but does not extract structured fields (rationale, alternatives, abandonment reasons). No agent-based compression like claude-mem.
3. **No PR/commit linking** — Context floats loose, connected only by file paths and branch tags (`branch:{branchname}`)
4. **No stale detection** — Old context injected without checking if code has changed. Relevance scoring decays over time (half-life ~23 days) but does not check code diffs.
5. **No "why not" tracking** — No structure for abandoned approaches or exploration history
6. **No AI-to-AI review** — No session handoff for review purposes
7. **Weak git integration** — `git rev-parse` for branch, `git ls-files` for scan, but no push hooks, no commit SHA linking, no PR connection. Manual `repo scan` only.
8. **Cloud-dependent** — Turso (libSQL) is cloud-hosted. Self-hosted mode exists (`SELF_HOSTED=true`) but requires setup.
