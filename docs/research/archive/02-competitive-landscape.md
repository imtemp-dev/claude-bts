# Context Sync — Competitive Landscape

## Market Timing

The "context problem" for AI coding is being recognized but barely addressed:

| Product | Launch | Age | Traction |
|---------|--------|-----|----------|
| SpecStory | Dec 2024 | ~15 months | 3K+ devs, 50K+ conversations |
| Deciduous | Dec 2025 (HN) | ~3 months | 1.1K+ nodes (self-dogfooding) |
| ContextStream | Dec 2025 (MCP release) | ~3 months | 30 GitHub stars |
| memctl | Mar 2026 (HN Show HN) | ~2 weeks | 11 GitHub stars |
| CONTINUITY | — | — | Small personal project |
| Mem0 | — | Established | 66K+ stars (but not coding-specific) |

**Implication**: No winner yet. The most advanced project (SpecStory) has only 15 months of head start. ContextStream is ~3 months old, and memctl launched just ~2 weeks ago. The starting line is essentially the same.

## Six-Product Comparison

| | memctl | ContextStream | SpecStory | Deciduous | CONTINUITY | claude-mem | Mem0 |
|---|--------|---------------|-----------|-----------|------------|------------|------|
| **Focus** | Team shared memory | AI context + knowledge graph | Session capture | Decision tracking | Session continuity | Personal session memory | General AI memory |
| **Protocol** | MCP | MCP | CLI + extension | CLI + skills | MCP | Claude plugin + MCP | SDK/API |
| **Capture** | Semi-auto (hooks + keyword) | Semi-auto (27 hooks) | Auto (full raw) | Manual (CLI calls) | Manual (tool calls) | Auto (6 hooks) | Manual (SDK calls) |
| **Refine** | Keyword classification | Partial (lesson/compress) | None | Structured (DAG) | Structured (registry) | Multi-agent XML (SDK/Gemini/OpenRouter) | None |
| **Team** | Yes (org/project/role RBAC) | Partial (Team plan) | Roadmap | No | No | No | No |
| **Search** | Hybrid (FTS5+384d vector, RRF) | 8-mode semantic (cloud) | Local file grep | FTS5 | None | 3-strategy (SQLite+Chroma+Hybrid) | Vector |
| **Code link** | File paths + branch tag | Dependency graph + code graph | None | Commit SHA | None | File path + git branch | None |
| **PR link** | No | No | No | No | No | No | No |
| **Stale detect** | No (time decay only) | No (token pressure only) | No | No | No | No | No |
| **License** | Apache-2.0 | MIT | Proprietary | Open source | Open source | AGPL-3.0 | Apache-2.0 |
| **Pricing** | $0-149/mo (capacity) | $0-40/seat (feature+capacity) | Free local, cloud TBD | Free | Free | Free | Free + paid cloud |
| **Source lines** | ~22K (mono, 4 packages) | ~42K (single package) | N/A (proprietary) | N/A | N/A | ~36K (185 files) | N/A |

## Pipeline Coverage Matrix

| Stage | memctl | ContextStream | claude-mem | SpecStory | Deciduous | CONTINUITY | **Context Sync (target)** |
|-------|--------|---------------|------------|-----------|-----------|------------|--------------------------|
| **Capture** | Semi-auto (hooks + keyword) | Semi-auto (27 hooks) | Auto (6 hooks, 120s) | Full raw auto | Semi-manual | Manual | Auto (session/PR) |
| **Refine** | Keyword classification | Partial (lesson/compress) | Multi-agent XML | None | Best (DAG) | Structured | LLM auto-extraction |
| **Store** | Team cloud Turso + vector | Cloud API + knowledge graph | SQLite WAL + Chroma | Local markdown | Local SQLite | Local files | PR-linked team DB |
| **Inject** | MCP auto (context_for) | Per-message auto + lesson | SessionStart context | Manual search | Partial | Manual load | Diff-verified auto |
| **Code link** | File path + branch | Dep graph + code graph | File path + branch | None | Commit SHA | None | PR+commit+diff live |
| **Team share** | Full (org/project/role) | Partial (Team plan) | None | Roadmap | None | None | Full |
| **Stale detect** | None (time decay) | None (token pressure) | None | None | None | None | Core feature |
| **AI-to-AI review** | None | None | None | None | None | None | Core feature |

## Key Gaps No One Fills

1. **PR-based context linking** — No product connects session context to PRs as a first-class unit. memctl and ContextStream track file paths and branches but not PR boundaries.
2. **Stale context verification** — No product checks if context is still valid against current code. memctl uses time-decay (half-life ~23 days). ContextStream uses token pressure (4 levels). Neither checks `git diff`.
3. **AI-to-AI review handoff** — No product enables an AI reviewer to inherit the authoring session. ContextStream has pre/post compaction snapshots but not for review purposes.
4. **Structured "why not" extraction** — Deciduous auto-extracts decisions from git history (archaeology mode) but not from live AI sessions. claude-mem captures observations with 6 types (decision/bugfix/feature/refactor/discovery/change) but no explicit "abandoned approach" type. No one auto-extracts explorations and abandonment reasons.
5. **Full pipeline automation** — Each competitor excels at one stage: SpecStory (capture), Deciduous (refine), memctl (store/team), ContextStream (inject/search). claude-mem is closest to full pipeline (auto-capture + agent refinement + store + inject) but lacks team and code linking. No one delivers capture → refine → store → inject with team + code linking as a single automated flow.

## Competitive Positioning

```
                    Manual capture ←————————→ Auto capture
                         |                        |
                    Deciduous                 SpecStory (full raw)
                    CONTINUITY                claude-mem (6 hooks, agent)
                         |              ContextStream (27 hooks, semi-auto)
                         |              memctl (hooks, keyword)
                         |                        |
                    No refinement ←———————→ Structured refinement
                         |                        |
                    SpecStory                 Deciduous (manual DAG)
                    memctl (keyword only)     claude-mem (agent XML)
                                              Context Sync (auto) ←— TARGET
                         |
                    No team ←—————————————→ Full team
                         |                        |
                    Deciduous                 memctl (org/project/role)
                    CONTINUITY                ContextStream (Team plan)
                    SpecStory                 Context Sync ←— TARGET
                    claude-mem
```

Context Sync targets the upper-right corner of every axis: auto capture, structured refinement, full team support — a position no current product occupies. claude-mem is the closest to full-pipeline but lacks team support entirely.
