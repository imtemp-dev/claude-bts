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

## How It Works

bts is a Go binary + Claude Code plugin. It deploys skills, agents, hooks, and rules that automate spec verification.

### Verification Loop

```
You write a spec
  → /cross-check   facts against source code (deterministic, bts binary)
  → /verify        logical consistency (independent subagent)
  → /audit         missing scenarios, edge cases (independent subagent)
  → errors found?  → fix spec → re-verify (max N iterations)
  → all clear?     → spec is bulletproof
```

### Recipes

Recipes compose skills into end-to-end workflows:

```
/recipe analyze    "auth system"        → verified analysis doc
/recipe design     "OAuth2 login"       → verified design doc
/recipe blueprint  "API endpoints"      → implementation-ready spec (Level 3)
```

The **blueprint** recipe produces a spec with exact file paths, function signatures, types, connection points, edge cases, and scaffolding — detailed enough for Opus to implement directly.

### Expert Debate

```
/debate "OAuth2 vs JWT vs custom tokens"
  → 3 expert personas discuss across multiple rounds
  → state saved, resumable across sessions
  → deadlock? → asks you to decide
```

## Install

```bash
# Build from source (Go 1.22+)
git clone https://github.com/jlim/bts.git
cd bts
make build
./bin/bts init your-project
```

## Quick Start

```bash
# Initialize in your project
bts init .

# Start Claude Code
claude

# Run a recipe
/recipe blueprint "add OAuth2 authentication"

# Or use individual skills
/verify docs/design.md
/cross-check docs/spec.md
/audit docs/spec.md
/debate "Redis vs Memcached for session store"
```

## Architecture

```
Go binary (bts)                    Claude Code
├── bts init        deploy →       .claude/skills/bts/     (8 skills)
├── bts verify      fact-check     .claude/agents/bts/     (3 agents)
├── bts hook        lifecycle      .claude/commands/bts/   (6 commands)
├── bts recipe      state mgmt    .claude/rules/bts/      (2 rules)
└── bts debate      state mgmt    .claude/hooks/bts/      (4 hooks)
```

**Key principle**: Facts are checked by the binary (deterministic). Judgment is done by isolated subagents (independent context). The main Claude session never verifies its own output.

## Orchestration

Claude follows the recipe protocol (SKILL.md). The binary provides checkpoints:

- **Soft gate**: Claude checks verification results per the recipe protocol
- **Hard gate**: Stop hook reads `verify-log.jsonl` — blocks completion if critical/major > 0

Sessions can be interrupted and resumed. Recipe state persists in `.bts/state/`.

## CLI

```
bts init [dir]              Initialize project
bts verify <file>           Deterministic fact-check
bts recipe status           Show active recipe
bts recipe list             All recipes
bts recipe log <id>         Record verify iteration
bts recipe cancel           Cancel active recipe
bts debate list             All debates
bts debate export <id>      Export as markdown
bts doctor                  System diagnostics
bts config set/get          Configuration
```

## Document Levels

| Level | Name | Contains | AI Code Accuracy |
|-------|------|----------|-----------------|
| 1 | Understanding | System structure, files, dependencies | Not possible |
| 2 | Design | Components, data flow, tech choices | ~60-70% |
| 3 | Implementation-ready | File paths, signatures, types, edge cases, scaffolding | **Very high** |

bts takes your spec from wherever it is to Level 3.

## Verification Severity

| Severity | Meaning | Blocks completion? |
|----------|---------|-------------------|
| critical | References non-existent file/function | Yes |
| major | Logical inconsistency, missing error handling | Yes |
| minor | Imprecise but not wrong | No |
| info | Style suggestion | No |

## Relationship with moai-adk

```
bts:   requirements → bulletproof spec (Level 3)
moai:  spec → code (TRUST 5, TDD/DDD)
```

bts produces the document. moai produces the code. They complement each other.

## License

MIT
