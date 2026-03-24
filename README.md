# bts — Bulletproof Technical Specification

[한국어](README.ko.md) | [中文](README.zh.md) | [日本語](README.ja.md)

```
╔════════════════════════════════════════════════════════════════╗
║                                                                ║
║   Ralph Mode                    Lisa Mode                      ║
║                                                                ║
║   code -> fail                  spec -> verify                 ║
║     -> code -> fail               -> spec -> verify            ║
║       -> code -> fail               -> spec -> verify          ║
║         -> code -> fail               -> bulletproof spec      ║
║           -> ...                        -> code                ║
║             -> works?                     -> works. first try. ║
║                                                                ║
║   Loop the CODE (expensive)     Loop the DOCS (safe to fail)   ║
║   builds, tests, side effects   no builds, no tests, no breakage║
║                                                                ║
║                    bts is Lisa Mode.                           ║
║                                                                ║
╚════════════════════════════════════════════════════════════════╝
```

> **Ralph loops code. Lisa loops documents.**
> Both iterate until it works — but documents are safe to change.
> No builds, no tests, no side effects. When the spec is bulletproof,
> AI generates working code on the first try.

## Full Lifecycle

```mermaid
flowchart LR
    subgraph Blueprint
        DIS["Discover"] --> S["Scoping"] --> R["Research"] --> D["Draft"] --> V["Verify Loop"]
        V --> SIM["Simulate"] --> DB["Debate"] --> F["Finalize"]
    end
    subgraph Implement
        IMP["Implement"] --> T["Test"] --> CSM["Simulate"] --> RV["Review"] --> SY["Sync"] --> ST["Status"]
    end
    F --> IMP
    ST --> DONE["Complete"]
```

```mermaid
flowchart LR
    subgraph Fix
        DG["Diagnose"] --> FS["Fix Spec"] --> SM["Simulate"]
        SM --> ER["Expert Review"] --> VR["Verify"] --> IM["Implement"] --> TE["Test"]
    end
    TE --> FD["Complete"]
```

```mermaid
flowchart LR
    subgraph Debug
        P["6 Perspectives"] --> CR["Cross-Reference"] --> HY["Hypothesis"]
        HY --> S2["Simulate"] --> D2["Debate"] --> V2["Verify"] --> FM["final.md"]
    end
    FM --> IMP2["/implement"] --> DONE2["Complete"]
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

If `~/.local/bin` is not in your PATH, add it to `.zshrc` or `.bashrc`:
```bash
export PATH="$HOME/.local/bin:$PATH"
```

Update:
```bash
git pull && make install
```

Check version:
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

```mermaid
flowchart LR
    subgraph PLAN
        blueprint
        design
        analyze
        scope+debate
    end
    subgraph BUILD
        implement
        build_loop["build loop"]
        scaffolding
    end
    subgraph VERIFY
        test
        sync
        review
        doctor
    end
    subgraph ITERATE
        fix["fix (known)"]
        debug["debug (unknown)"]
        new_bp["new blueprint"]
    end

    PLAN --> BUILD --> VERIFY --> ITERATE
    ITERATE --> PLAN
```

Typical project progression:

```mermaid
flowchart TD
    NEW["New Project"] --> VIS["Vision & Roadmap"]
    VIS -->|"decompose"| A["/recipe blueprint Feature A"]
    A --> AC["complete → roadmap ✓"]
    AC --> B["/recipe blueprint Feature B"]
    B -->|"roadmap item 2/5"| BC["complete → roadmap ✓"]
    BC --> FX["/recipe fix Bug in A"]
    FX --> FXC["complete"]
    FXC --> C["/recipe blueprint Feature C"]
    C -->|"roadmap item 3/5"| CC["complete → roadmap ✓"]
    CC --> DOC["bts doctor — health check"]
```

## Development Lifecycle

bts maps to a standard development process:

```
Requirements → Planning → Design → Implementation → Verification → Release
     ↓            ↓         ↓           ↓                ↓           ↓
  discover     vision    blueprint   implement        test+review   sync+status
  (intent)    roadmap    (spec)      (code)          simulate      (complete)
               scope
```

## State Machine

```mermaid
stateDiagram-v2
    [*] --> discovery

    state "Spec Phase" as spec {
        discovery --> scoping : intent confirmed
        scoping --> research
        research --> draft
        draft --> verify
        verify --> assess
        assess --> improve : content missing
        assess --> simulate : gaps may exist
        assess --> debate : decision needed
        assess --> audit : completeness uncertain
        assess --> sync_check : Level 3 achieved
        improve --> verify
        simulate --> improve
        debate --> improve
        audit --> improve
        sync_check --> finalize
    }

    finalize --> implement : DONE (verify-log OK)

    state "Implement Phase" as impl {
        implement --> test
        test --> simulate_code : code simulation
        simulate_code --> review
        review --> sync
        sync --> status
    }

    status --> complete : IMPLEMENT DONE

    complete --> fix : follow-up (known bug)
    complete --> debug : follow-up (unknown bug)

    fix --> complete : FIX DONE
    debug --> finalize_d : DONE
    finalize_d --> implement
```

### Stop Hook Gates

| Marker | Validates | Sets phase |
|--------|-----------|------------|
| `<bts>DONE</bts>` | verification.md + verify-log: critical=0, major=0 | → finalize |
| `<bts>IMPLEMENT DONE</bts>` | tasks done + tests pass + review.md + deviation.md | → complete |
| `<bts>FIX DONE</bts>` | fix-spec.md + tests pass | → complete |

## Document Flow

```mermaid
flowchart TD
    subgraph Spec Phase
        SC["scope.md"] --> RS["research/v1.md"]
        RS --> DR["draft.md"]
        DR --> VF["verification.md"]
        DR --> SM["simulations/"]
        DR --> DB["debates/"]
        DR --> FM["final.md"]
    end

    subgraph Implement Phase
        FM --> TK["tasks.json"]
        TK --> CODE["code files"]
        CODE --> TR["test-results.json"]
        TR --> RV2["review.md"]
        RV2 --> DV["deviation.md"]
    end

    subgraph Project Level
        DV --> PS["project-status.md"]
        DV --> PM["project-map.md"]
        PS --> RM["roadmap.md"]
    end
```

### Project-level Documents

```
.bts/state/
├── vision.md               Product vision (purpose, components, constraints)
├── roadmap.md              Ordered recipe decomposition (checkbox items)
├── project-map.md          Level 0: layer overview (~300 tokens)
├── layers/{name}.md        Level 1: layer detail (on-demand)
├── project-status.md       Recipe status table + architecture
└── recipes/
    ├── r-1001/             Blueprint: scope.md, final.md, deviation.md, ...
    ├── r-fix-1002/         Fix: diagnosis.md, fix-spec.md, ...
    └── r-debug-1003/       Debug: perspectives.md, final.md, ...
```

**Vision & Roadmap**: Large features auto-decompose into recipe-sized units.
`vision.md` captures the final product vision; `roadmap.md` tracks ordered
items with checkbox progress. Each recipe links to its roadmap item, and
completion automatically marks it done.

## Recipes

| Recipe | Purpose | Output |
|--------|---------|--------|
| `/recipe analyze` | Understand existing system | Level 1 analysis doc |
| `/recipe design` | Design a feature | Level 2 design doc |
| `/recipe blueprint` | Full implementation spec | Level 3 spec → code → tests |
| `/recipe fix` | Known bug fix (lightweight) | Fix spec → code → tests |
| `/recipe debug` | Unknown bug investigation | 6-perspective analysis → spec → code |

## Skills (19)

| Category | Skills |
|----------|--------|
| **Recipes** | blueprint, design, analyze, fix, debug |
| **Verification** | verify, cross-check, audit, assess, sync-check |
| **Analysis** | research, simulate, debate, adjudicate |
| **Implementation** | implement, test, sync, status |
| **Quality** | review (basic / security / performance / patterns) |

## Architecture

```mermaid
flowchart LR
    subgraph Go["Go binary (bts)"]
        init["bts init"]
        validate["bts validate"]
        doctor["bts doctor"]
        hook["bts hook"]
        recipe["bts recipe"]
        statusline["bts statusline"]
    end

    subgraph CC["Claude Code"]
        skills["19 skills"]
        agents["3 agents"]
        hooks["6 hooks"]
        rules["6 rules"]
        commands["1 dispatcher"]
        mcp[".mcp.json (Context7)"]
    end

    init -->|"deploy"| CC
    hook -->|"lifecycle"| CC
    statusline -->|"display"| CC
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
- **Fast**: Single Go binary, zero runtime dependencies, ~5ms startup

## CLI

```
bts init [dir]              Initialize project
bts doctor [recipe-id]      Recipe health check (documents, manifest, flow, vision/roadmap)
bts validate [recipe-id]    Check JSON schema compliance
bts version                 Show binary and template versions
bts update                  Deploy latest templates
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
