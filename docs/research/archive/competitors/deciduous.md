# Deciduous — Detailed Analysis

> Decision tracking as a DAG (Directed Acyclic Graph). Best "why" documentation tool.

- **GitHub**: notactuallytreyanastasio/deciduous
- **License**: Open source (specific license type unverified — check repo)
- **Built with**: Rust (crates.io)
- **HN appearance**: Dec 2025
- **Self-dogfooding**: 1,100+ nodes tracking own development

---

## Architecture

### CLI Tool (Rust)
```bash
cargo install deciduous
cd my-project
deciduous init
# Auto-installs: .claude/ slash commands, skills, project instructions
```

### Node Types (DAG)
```
Goal → Decision → Action → Observation → Revisit
```

Each node has:
- **Content**: Description of the decision/action
- **Confidence score** (0-100): How certain the decision is
- **Commit SHA** (optional): `--commit HEAD` links to git
- **Superseded flag**: Marks replaced decisions

---

## Core Workflow

### Real-Time Tracking (AI-assisted via pulse skill)
```
1. deciduous add goal "User authentication" -c 90
2. deciduous add decision "JWT vs sessions" -c 75
3. deciduous add action "Implement JWT" --commit HEAD
4. deciduous add observation "Token expiry handling missing"
5. deciduous archaeology pivot 2 "JWT expiry issue" "Add refresh tokens"
   → Previous node marked superseded
   → New decision/action auto-generated
```

### Archaeology Mode (Retrospective)
```
deciduous archaeology
→ Analyzes 347 git commits
→ Discovers 4 major narratives
→ Generates 28 nodes, 31 edges, 3 pivot points
```
Builds decision graph retroactively from existing git history.

### Q&A Interface
```
POST /api/ask "Why did we choose PostgreSQL?"
→ Searches graph for relevant nodes
→ Decision #5: "PostgreSQL"
  rationale: "AGE extension covers graph queries"
  alternatives: ["Neo4j", "DGraph", "ArangoDB"]
  revisit_trigger: "Graph queries exceed 10K nodes/sec"
```

### Visualization
```
deciduous serve → localhost:3000
  ├── DAG view: Full decision graph
  ├── Timeline view: Chronological
  ├── Chain view: Grouped by narrative
  ├── Archaeology view: Superseded/pivot focus
  ├── Roadmap view: Future plans
  └── Q&A panel: Natural language queries
```

---

## Data Model

### Node Structure
```
Node {
  id: auto-increment
  type: goal | decision | action | observation | revisit
  content: string
  confidence: 0-100
  commit_sha?: string
  superseded: boolean
  superseded_by?: node_id
  created_at: timestamp
}
```

### Edge Structure
```
Edge {
  from_node: node_id
  to_node: node_id
  relationship: "leads_to" | "supersedes" | "triggers"
}
```

### Storage
- Local SQLite database
- FTS5 for full-text search
- No vector embeddings

---

## Strengths

1. **Best "why" documentation** — Only tool that structurally captures decisions with rationale and alternatives
2. **Superseded/pivot tracking** — Unique: marks when decisions are replaced and why
3. **Git commit linking** — `--commit` flag directly connects decisions to code
4. **Archaeology mode** — Can build decision graph from existing git history (retroactive value)
5. **Q&A interface** — Natural language queries against decision graph
6. **Confidence scoring** — Quantifies certainty of decisions
7. **Revisit triggers** — Defined conditions for when to re-examine decisions
8. **Extreme dogfooding** — 1,100+ nodes tracking its own development

---

## Pricing

- Completely free, open source
- No commercial plans
- Single developer project

---

## Gaps (Context Sync Perspective)

1. **Personal only** — No team sharing, no org model, no collaboration
2. **Not MCP** — CLI tool, requires slash commands/skills to integrate with AI
3. **No auto-capture** — AI must explicitly call `deciduous add` (via pulse skill, but not enforced)
4. **No session context** — Only structured nodes, full conversation context is lost
5. **No auto-injection** — `/recover` can restore context but not automatic on related work
6. **No semantic search** — FTS5 only, no vector/embeddings
7. **No stale detection** — Commit SHA stored but no diff-based validity checking
8. **Single developer risk** — No team, no company, sustainability uncertain

---

## Key Takeaway

Deciduous has the **best conceptual model** for "why" tracking — the DAG of goals → decisions → actions → observations → revisits with superseded tracking is elegant. The archaeology mode (retroactive graph building from git) is genuinely novel.

**What Context Sync should learn**: The node type taxonomy (decision, observation, revisit, superseded) and the concept of revisit triggers. But auto-extract these from sessions instead of requiring manual CLI calls.
