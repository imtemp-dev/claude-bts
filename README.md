# bts — Bulletproof Technical Specification

Make your implementation spec so detailed that AI generates working code on the first try.

## The Problem

```
Rough plan → AI codes → bugs → fix → bugs → fix → ... (repeat N times)
```

Most time is spent debugging AI-generated code. The root cause: the spec was vague, so the AI guessed.

## The Solution

```
Spec → verify → fix → verify → ... → bulletproof spec → AI codes → done
```

Iterate on the **document**, not the code. Documents are free to change — no builds, no tests, no side effects. When the spec is bulletproof, AI generates code with minimal iteration.

## Full Lifecycle

```
/recipe blueprint "feature"
  → Scoping → Research → Draft → Verify Loop → Simulate → Debate → Finalize
  → /implement → Build Loop → /test → /sync → Complete

/recipe fix "known bug"
  → Diagnose → Fix Spec → Simulate → Expert Review → Verify → Implement → Test → Complete

/recipe debug "unknown symptom"
  → 6 Perspectives → Cross-Reference → Hypothesis → Simulate → Debate → Verify
  → /implement → Test → Sync → Complete
```

bts covers **Planning → Build → Verify** as a single automated pipeline.

## Install

```bash
# One-line install (macOS / Linux)
curl -fsSL https://raw.githubusercontent.com/jlim/bts/main/install.sh | bash

# Or build from source (Go 1.22+)
git clone https://github.com/jlim/bts.git
cd bts
make install    # installs to ~/.local/bin/bts
```

PATH에 `~/.local/bin`이 없으면 `.zshrc` 또는 `.bashrc`에 추가:
```bash
export PATH="$HOME/.local/bin:$PATH"
```

업데이트:
```bash
git pull && make install
```

버전 확인:
```bash
bts --version
```

## Quick Start

```bash
# Initialize in your project
bts init .

# Start Claude Code
claude

# Create a bulletproof spec
/recipe blueprint "add OAuth2 authentication"

# Fix a known bug
/recipe fix "login bcrypt hash comparison fails"

# Debug an unknown issue
/recipe debug "session drops after 5 minutes"

# Review code quality
/bts-review
/bts-review security src/auth/

# Check project health
bts doctor
```

## Development Process

How bts fits into a real development lifecycle:

```
┌─────────────────────────────────────────────────────────────┐
│                    DEVELOPMENT LIFECYCLE                     │
│                                                             │
│  PLAN          BUILD           VERIFY          ITERATE      │
│  ────          ─────           ──────          ───────      │
│  blueprint     implement       test            fix (known)  │
│  design        build loop      sync            debug (unknown)│
│  analyze       scaffolding     review          new blueprint │
│  scope+debate                  doctor                       │
│                                                             │
│  ◄──────────── bts covers ──────────────►                   │
│                                              deploy/monitor │
│                                              = out of scope │
└─────────────────────────────────────────────────────────────┘
```

Typical project progression:

```
New Project
  │
  ├─ /recipe blueprint "Feature A" ──→ implement ──→ complete
  │    scope → research → draft → verify loop → finalize
  │
  ├─ /recipe blueprint "Feature B" ──→ implement ──→ complete
  │    reads project-map.md for existing context
  │
  ├─ /recipe fix "Bug in A" ──→ complete
  │    diagnosis → fix-spec → test
  │
  ├─ /recipe debug "Unknown issue" ──→ implement ──→ complete
  │    6 perspectives → root cause → fix spec
  │
  ├─ /bts-review security ──→ review.md
  │
  ├─ /recipe blueprint "Feature C" ──→ implement ──→ complete
  │    project-map shows A + B + fixes
  │
  └─ bts doctor ──→ health check across all recipes
```

## State Machine

Recipe phase transitions:

```
                    ┌──────────────────────────────────────────┐
                    │           SPEC PHASE                     │
                    │                                          │
 (start) ──→ scoping ──→ research ──→ draft ◄──┐              │
                                        │       │              │
                                        ▼       │              │
                                   ┌─ verify ──┤              │
                                   │    │       │              │
                                   │    ▼       │              │
                                   │  assess ───┤              │
                                   │    │       │              │
                                   │    ├─→ improve ──→ (back) │
                                   │    ├─→ simulate           │
                                   │    ├─→ debate             │
                                   │    ├─→ audit              │
                                   │    └─→ sync-check         │
                                   │           │               │
                                   │           ▼               │
                                   │      finalize             │
                                   └───────────┘               │
                    └──────────────────────────────────────────┘
                                        │
                         <bts>DONE</bts> │ stop hook validates
                                        ▼
                    ┌──────────────────────────────────────────┐
                    │         IMPLEMENT PHASE                   │
                    │                                          │
                    │  implement ──→ test ──→ sync ──→ status  │
                    │  (task loop)  (fix loop) (compare)       │
                    └──────────────────────────────────────────┘
                                        │
                    <bts>IMPLEMENT DONE</bts> │ stop hook validates
                                        ▼
                                    complete
                                        │
                                  (follow-up)
                              ┌─────────┴─────────┐
                              ▼                   ▼
                         /recipe fix         /recipe debug
                              │                   │
                         <bts>FIX DONE</bts>      │
                              ▼              (produces final.md)
                          complete                │
                                            /bts-implement
                                                  │
                                        <bts>IMPLEMENT DONE</bts>
                                                  ▼
                                              complete
```

Terminal states: `complete`, `cancelled`

### Stop Hook Gates

| Marker | Validates | Sets phase |
|--------|-----------|------------|
| `<bts>DONE</bts>` | verify-log: critical=0, major=0 | → finalize |
| `<bts>IMPLEMENT DONE</bts>` | tasks done + tests pass + deviation.md exists | → complete |
| `<bts>FIX DONE</bts>` | fix-spec.md exists + tests pass | → complete |

## Document Flow

How documents are created and consumed across the lifecycle:

```
SPEC PHASE                          IMPLEMENT PHASE
──────────                          ───────────────

scope.md ──→ research/v1.md         final.md
              │                       │
              ▼                       ▼
         drafts/v1.md ──→ v2 ──→ vN  tasks.json (decomposed)
              │                       │
              ▼                       ▼
     verifications/vN.md            [code files created]
     simulations/001.md               │
     debates/001-topic/               ▼
              │                   test-results.json
              ▼                       │
         final.md ─────────────→      ▼
                                  deviation.md (report, not gate)
                                      │
                                      ▼
                              project-status.md
                              project-map.md
```

### Project-level Documents

```
.bts/state/
├── project-map.md          Level 0: layer overview (~300 tokens)
│                           Auto-synced on recipe completion
├── layers/{name}.md        Level 1: layer detail (on-demand)
│                           Created when scoping needs it
├── project-status.md       Recipe status table + architecture
└── recipes/
    ├── r-1001/             Blueprint recipe
    │   ├── scope.md
    │   ├── final.md
    │   ├── deviation.md    Follow-up items (not gate)
    │   └── ...
    ├── r-fix-1002/         Fix recipe
    │   ├── diagnosis.md
    │   ├── fix-spec.md
    │   └── ...
    └── r-debug-1003/       Debug recipe
        ├── perspectives.md
        ├── final.md
        └── ...
```

## Recipes

| Recipe | Purpose | Output |
|--------|---------|--------|
| `/recipe analyze` | Understand existing system | Level 1 analysis doc |
| `/recipe design` | Design a feature | Level 2 design doc |
| `/recipe blueprint` | Full implementation spec | Level 3 spec → code → tests |
| `/recipe fix` | Known bug fix (lightweight) | Fix spec → code → tests |
| `/recipe debug` | Unknown bug investigation | 6-perspective analysis → spec → code |

### Blueprint Flow

```
Scoping (user alignment)
  → Research (codebase + Context7 + web)
  → Draft + Self-Check
  → Verify Loop (max 3 cycles)
  → Simulate (scenarios, early after first critical=0)
  → Debate + Adjudicate (if uncertain decisions)
  → Finalize (Level 3 spec)
  → Implement (task decomposition + build loop)
  → Test (generate + run + fix loop)
  → Sync (spec ↔ code comparison)
  → Complete
```

### Debug Flow

```
Collect 6 Perspectives:
  Data Flow │ Dependencies │ Design Intent
  Runtime Context │ Change History │ Impact Map
  → Cross-reference → Ranked hypotheses
  → Fix spec draft → Simulate → Debate → Verify
  → /implement → Test → Sync → Complete
```

## Skills (19)

| Category | Skills |
|----------|--------|
| **Recipes** | blueprint, design, analyze, fix, debug |
| **Verification** | verify, cross-check, audit, assess, sync-check |
| **Analysis** | research, simulate, debate, adjudicate |
| **Implementation** | implement, test, sync, status |
| **Quality** | review (basic / security / performance / patterns) |

## Architecture

```
Go binary (bts)                    Claude Code
├── bts init        deploy →       .claude/skills/     (19 skills)
├── bts validate    schema →       .claude/agents/     (3 agents)
├── bts doctor      health →       .claude/hooks/      (6 hooks)
├── bts hook        lifecycle      .claude/rules/      (6 rules)
├── bts recipe      state mgmt    .claude/commands/    (1 dispatcher)
├── bts statusline  display       .mcp.json           (Context7)
└── bts debate      state mgmt    .bts/status_line.sh (statusline)
```

### Hooks

| Hook | Purpose |
|------|---------|
| session-start | Source-aware context injection (resume/compact/startup) |
| pre-compact | Work state snapshot before context compaction |
| session-end | Work state persistence for cross-session resume |
| stop | Completion gates (DONE / IMPLEMENT DONE / FIX DONE) |
| subagent-start/stop | 🟡 indicator on statusline during agent execution |

### Statusline

```
bts v0.1.0 │ JWT auth │ 🟡 verify │ ctx 45%
bts v0.1.0 │ JWT auth │ implement 3/5 │ ctx 60%
bts v0.1.0 │ bcrypt fix │ test │ ctx 30%
```

### Project Map

Lightweight project overview, auto-synced on recipe completion:
```
.bts/state/project-map.md     — Level 0: layer paths + build/test commands
.bts/state/layers/{name}.md   — Level 1: on-demand detail per layer
```

## Key Principles

- **Document first**: Iterate on the spec, not the code
- **Never verify your own output**: Verification uses separate agent contexts
- **Context as glue**: Skills provide situational awareness, not rigid rules
- **Deviation = follow-up**: Spec-code differences are reports, not gates
- **Crash resilient**: Work state persists via tasks.json + work-state.json
- **Hierarchical map**: Lightweight project overview, detail on demand

## CLI

```
bts init [dir]              Initialize project
bts doctor [recipe-id]      Recipe health check (documents, manifest, flow)
bts validate [recipe-id]    Check JSON schema compliance
bts recipe status           Show active recipe
bts recipe list             All recipes
bts recipe log <id>         Record action/phase/iteration
bts recipe cancel           Cancel active recipe
bts debate list             All debates
bts statusline              Render status for Claude Code (internal)
bts hook <event>            Handle lifecycle events (internal)
```

## Document Levels

| Level | Name | Contains | AI Code Accuracy |
|-------|------|----------|-----------------|
| 1 | Understanding | System structure, files, dependencies | Not possible |
| 2 | Design | Components, data flow, tech choices | ~60-70% |
| 3 | Implementation-ready | File paths, signatures, types, edge cases, scaffolding | **Very high** |

## License

MIT
