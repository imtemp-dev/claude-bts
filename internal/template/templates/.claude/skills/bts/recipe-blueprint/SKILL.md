---
name: bts-recipe-blueprint
description: >
  Create a Level 3 implementation spec through an adaptive loop of research,
  drafting, debate, simulation, and verification. The loop continues until
  the document is bulletproof.
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent
argument-hint: "\"feature description\""
---

# Recipe: Blueprint

Create a bulletproof implementation spec for: $ARGUMENTS

## Resume Check

Before starting, check for an existing recipe:
```bash
bts recipe status
```
If active, read `.bts/state/{id}/manifest.json` and `changelog.jsonl` to understand
what has been done. Resume from where it left off.

## Adaptive Loop

This recipe does NOT follow a fixed sequence. Instead, it runs an adaptive loop:

```
ASSESS → decide action → execute → VERIFY (mandatory after any change) → ASSESS → ...
```

ASSESS determines what to do next based on the document's current state.

### Loop Protocol

**ALWAYS after modifying the document:**
1. Save as new version: `drafts/vN.md` (never overwrite previous versions)
2. Run /verify on the new version
3. Log the change: `bts recipe log {id} --action [action] --output drafts/vN.md`
4. Run /assess to determine the next action

**Starting from scratch (no existing code):**
1. /research — investigate the technology, best practices, libraries
2. Write initial draft (Level 1) → drafts/v1.md → /verify
3. /assess → loop begins

**Starting with existing code:**
1. /research — explore existing codebase
2. Write initial draft referencing existing code → drafts/v1.md → /verify
3. /assess → loop begins

### ASSESS Decision Tree

After each /assess, execute the recommended action:

| Assessment | Action | Details |
|------------|--------|---------|
| "Information insufficient" | /research | Use plan mode. Investigate docs, APIs, libraries |
| "Technical decision needed" | /debate | 3 experts, multiple rounds. Save state |
| "Gaps may exist" | /simulate | Design 5+ scenarios. Walk through spec |
| "Content missing for next level" | IMPROVE | Add specific items. Save as new draft version |
| "Contradictions suspected" | /verify | Check internal consistency |
| "Completeness uncertain" | /audit | Review for missing cases |
| "Level 3 achieved" | /sync-check | Final cross-document verification |

### Quality Rules

1. **Every document modification → /verify.** No exceptions.
2. **Every debate conclusion → update draft → /verify.**
3. **Every simulation gap found → update draft → /verify.**
4. **/simulate at least once** before declaring Level 3.
5. **/debate for every uncertain technical choice.** Don't guess.
6. **/sync-check before finalizing.** All documents must be in sync.

### Version Management

Every draft version is preserved. Never overwrite.
```
.bts/state/{id}/
├── manifest.json            # Document relationships
├── changelog.jsonl           # Every action logged
├── research/v1.md
├── drafts/
│   ├── v1.md                # Initial draft
│   ├── v2.md                # After debate-001
│   ├── v3.md                # After simulation gaps
│   └── v4.md                # After audit items
├── debates/001-topic/
│   ├── meta.json
│   ├── round-1.md
│   └── round-2.md
├── simulations/001-scenarios.md
├── verifications/draft-v1.md
└── final.md
```

After each action, update manifest.json:
```bash
bts recipe log {id} --action [type] --output [path] --based-on [deps]
```

### Finalization

When /assess declares Level 3 achieved AND /sync-check passes:
1. Copy current draft to `final.md`
2. Output `<bts>DONE</bts>`
3. Stop hook will verify:
   - verify-log last entry: critical=0, major=0
   - All sync checks passed

### Human Intervention Points

The loop runs automatically. It pauses ONLY when:
- **[DECISION REQUIRED]**: A technical choice needs human judgment
- **[CONVERGENCE FAILED]**: Same issues persist after N iterations
- **[DEBATE DEADLOCK]**: Experts can't agree after 3 rounds

## Output Target

The final document should contain, for every component:
- Exact file paths (create/modify)
- Function signatures (name, params with types, return type)
- Data types and interfaces
- Connection points to other components
- Error handling for every failure mode
- Edge cases enumerated
- Code scaffolding (skeleton structure)
- Test scenarios (happy + error + edge)

When this document is given to Claude Opus, it should generate working code
with minimal additional iteration.
