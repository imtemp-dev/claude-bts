# SpecStory — Detailed Analysis

> AI coding session auto-capture to markdown. Broadest tool compatibility.

- **Website**: specstory.com
- **Cloud**: cloud.specstory.com
- **License**: Proprietary
- **Users**: 3K+ developers
- **Conversations saved**: 50K+
- **Rules generated**: 50K+
- **Founded**: Dec 2024, 4-person team

---

## Architecture

### Two Distribution Modes

**IDE Extension** (Cursor, VS Code):
- Install from marketplace → automatic capture begins
- Saves to `.specstory/history/` as timestamped markdown files
- Zero configuration

**CLI Wrapper**:
```bash
brew tap specstoryai/tap && brew install specstory
specstory run claude    # Wraps Claude Code
specstory run cursor    # Wraps Cursor
specstory run codex     # Wraps Codex CLI
specstory run gemini    # Wraps Gemini CLI
```
- Intercepts and logs all AI interactions
- Transparent wrapper — no workflow change

### Tool Compatibility (Broadest)
Cursor, VS Code + Copilot, Claude Code, Codex CLI, Gemini CLI, Droid, Windsurf

---

## Data Flow

### Capture (Automatic)
```
AI conversation in progress
       ↓
.specstory/history/
  ├── 2026-03-15_auth-refactor.md
  ├── 2026-03-15_bug-fix-cors.md
  └── 2026-03-16_feature-payment.md
```
Each file = full conversation raw (prompts + responses + tool calls + timestamps + metadata).

### Retrieval (Manual)
1. **Local search**: grep/ripgrep on `.specstory/history/` or ask agent to search
2. **Cloud sync**: `specstory sync` → cloud.specstory.com for cross-project search
3. **Agent Skills**: Community-contributed analysis tools
   - `specstory-yak` — yak shaving analysis
   - `specstory-session-summary` — session summaries
   - `specstory-organize` — folder organization

### Rule Derivation
- Analyzes conversation history for repeated patterns/preferences
- Auto-generates Cursor rules / Copilot instructions
- 50K+ rules generated to date

---

## SpecFlow Workflow
```
1. brainstorm.md — Free-form idea dump
2. spec.md — Structured specification (AI-assisted)
3. plan.md — Implementation plan
4. Implementation (Claude Code / Cursor)
5. specstory sync — Save session history
6. Extract — Manual insight extraction from sessions
```

---

## Strengths

1. **Zero friction** — Install and forget, no configuration needed
2. **Broadest compatibility** — Works with every major AI coding tool
3. **Local-first** — Data stays on machine, cloud is optional
4. **Community traction** — 3K+ developers, active on X/Twitter
5. **Agent Skills ecosystem** — Community can contribute analysis tools
6. **Rule derivation** — Unique feature, automatic coding style extraction

---

## Pricing

- **Local capture**: Free (forever)
- **Cloud search/sync**: Price TBD (early user acquisition phase)
- **Revenue model**: Likely freemium cloud with team features

---

## Gaps (Context Sync Perspective)

1. **No refinement** — Raw session stored as-is, massive noise (file reads, build logs, formatting)
2. **No auto-injection** — User must manually search and paste context into new sessions
3. **No structured extraction** — "What decisions were made?" requires manual reading
4. **Team sharing incomplete** — "Promote to team space" is still on roadmap
5. **No PR/code linking** — Sessions exist independently from code changes
6. **No stale detection** — Old sessions may reference code that no longer exists
7. **No AI-to-AI review** — No session handoff mechanism
8. **Storage bloat** — Full raw sessions consume significant disk space over time

---

## Key Takeaway

SpecStory nailed the **capture** problem with zero friction. But capture alone is 1/4 of the pipeline. Without refinement, injection, and team sharing, it's essentially a logging tool — valuable for history but not for accelerating future work.

The rule derivation feature is clever and unique — worth noting as a potential Phase 2 feature for Context Sync.
