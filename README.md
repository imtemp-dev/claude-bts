# claude-bts

**B**ulletproof **T**echnical **S**pecification — catches spec errors before they become debugging sessions.

[![CI](https://github.com/imtemp-dev/claude-bts/actions/workflows/ci.yml/badge.svg)](https://github.com/imtemp-dev/claude-bts/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/imtemp-dev/claude-bts)](https://github.com/imtemp-dev/claude-bts/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8.svg)](https://go.dev)

[한국어](README.ko.md) | [中文](README.zh.md) | [日本語](README.ja.md) | [For AI Agents](llms.txt)

## Why

You already do the right things — reminding AI of the architecture, asking for reviews, checking edge cases. But doing it manually means some sessions you're thorough and some you're not. Mistakes in the plan slip through to code, where they cost builds and debugging instead of a text edit. And once AI is deep in implementation, it loses sight of what the whole system should look like.

bts automates what you're already doing:

- **Isolated verification** — a separate AI instance reviews the spec without sharing the blind spots of the session that wrote it
- **State tracking** — issues found during verification persist across sessions and compactions, so nothing gets lost
- **Completion gates** — specs can't finalize without passing verification; code can't complete without tests, review, and deviation docs
- **Big picture first** — intent, scope, and wireframe are established before drafting begins, giving every later step a destination to refer back to

The core idea: **fix errors in documents, not in code.** A spec edit is free. A code fix is a build-test-debug cycle.

## Quick Start

```bash
brew tap imtemp-dev/tap && brew install bts   # or: curl -fsSL https://raw.githubusercontent.com/imtemp-dev/claude-bts/main/install.sh | bash
cd your-project && bts init . && claude
```

Then inside Claude Code:

```bash
/bts-recipe-blueprint add OAuth2 authentication    # spec → implement → test → simulate → review → sync → complete
/bts-recipe-fix login bcrypt hash comparison fails  # diagnose → fix-spec → implement → test → complete
/bts-recipe-debug session drops after 5 minutes     # 6-perspective → fix-spec → implement → test → complete
```

## How It Works

Blueprint lifecycle — the full spec-to-code cycle:

### 1. Establish the destination

```mermaid
flowchart LR
    INT["Discover Intent"] --> VIS["Vision & Roadmap"] --> SC["Scope"] --> WF["Wireframe"]
```

Before writing anything, bts establishes *what the finished system looks like*. Intent discovery clarifies purpose. Wireframe designs structure with mermaid diagrams. This is the map every later step refers back to.

### 2. Iterate the spec until bulletproof

```mermaid
flowchart LR
    D["Draft"] --> V["Verify ↗"]
    V -->|"issues"| A["Assess"]
    A -->|"improve"| D
    A -->|"debate"| DB["Debate"] --> D
    A -->|"simulate"| SM["Simulate ↗"] --> D
    A -->|"audit"| AU["Audit ↗"] --> D
    V -->|"pass"| F["Finalize"]
```

The adaptive loop: draft → verify → assess what's needed → act → verify again. **↗ = fork context** (separate AI instance). The loop runs until verification passes with zero critical and zero major issues.

### 3. Generate and validate code

```mermaid
flowchart LR
    F["Level 3 Spec"] --> IMP["Implement"] --> T["Test"]
    T -->|"fail"| IMP
    T -->|"pass"| SIM["Simulate ↗"] --> RV["Review ↗"] --> SY["Sync"] --> DONE["Complete"]
```

Code is generated from a spec that has survived multiple rounds of independent verification. Test failures loop back to implementation. Simulate and review run in fork context. Sync documents any spec-code deviations.

## Models

Core quality gates (verify, audit, simulate, review) use your **session model** in a **fork context** — a separate AI instance that doesn't share the conversation history. Pattern-based checks (cross-check, sync-check, security review) use Sonnet.

Override any agent model in `.bts/config/settings.yaml`:

```yaml
agents:
  # verifier: sonnet         # default: session model
  # auditor: sonnet          # default: session model
  reviewer_security: sonnet  # pattern-based
```

## Recipes

| Recipe | Lifecycle | Output |
|--------|-----------|--------|
| `/bts-recipe-blueprint` | discover → scope → wireframe → adaptive loop → implement → test → review → sync | Level 3 spec → code → tests |
| `/bts-recipe-fix` | diagnose → fix-spec → implement → test | Fix spec → code → tests |
| `/bts-recipe-debug` | 6-perspective analysis → cross-reference → fix-spec → implement → test | Root cause → fix |
| `/bts-recipe-design` | research → draft ←→ verify → finalize | Level 2 design doc |
| `/bts-recipe-analyze` | research → draft ←→ verify → finalize | Level 1 analysis doc |

## CLI

```
bts init [dir]              Initialize project
bts doctor [recipe-id]      Health check
bts recipe list|status|create|cancel   Manage recipes
bts recipe log <id>         Record action / phase
bts stats [recipe-id]       Metrics and cost (--json, --csv)
bts graph [recipe-id]       Document relationship graph
bts verify <file>           Check document consistency
bts validate [recipe-id]    JSON schema check
bts sync-check [recipe-id]  Verify document sync
bts update                  Update templates
bts version                 Show versions
```

## Architecture

**Go binary** — single statically-linked binary (~5ms startup), zero runtime dependencies. Manages state, validates completion, deploys templates, tracks metrics.

**Claude Code integration** — 21 skills, 8 lifecycle hooks, 6 rules. Verification always runs in separate agent contexts.

**File structure:**

```
.bts/
├── specs/     # git tracked — recipes, vision, roadmap
└── local/     # gitignored — metrics, work-state
```

## Requirements

- [Claude Code](https://docs.anthropic.com/en/docs/claude-code)
- Go 1.22+ ([install](https://go.dev/dl/))
- macOS, Linux (Windows via WSL)

## Contributing

```bash
git clone https://github.com/imtemp-dev/claude-bts.git && cd claude-bts
make install && go test -race ./...
```

[Open an issue](https://github.com/imtemp-dev/claude-bts/issues) for bugs or feature requests.

## License

MIT
