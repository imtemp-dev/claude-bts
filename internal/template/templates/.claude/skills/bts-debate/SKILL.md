---
name: bts-debate
description: >
  Run a structured expert debate with 3 personas. Produces a decision
  document with rationale. State is saved for resuming later.
user-invocable: true
allowed-tools: Read Write Bash
argument-hint: "\"topic to debate\""
---

# Expert Debate

Debate the given topic using expert personas.

Read `.bts/config/settings.yaml` for `debate.expert_count` (default: 3)
and `debate.rounds` (default: 3).

## Protocol

You will play `debate.expert_count` expert roles sequentially. Each round, all experts speak.

### Setup
Choose relevant expert perspectives for the topic. Example:
- For "OAuth2 vs JWT": Security Expert, Performance Expert, Operations Expert
- For "SQL vs NoSQL": Data Architect, Scale Engineer, Developer Experience Expert
- For "Monolith vs Microservices": System Architect, DevOps Engineer, Team Lead

### Round 1: Position Statement
Each expert states their position with supporting evidence.

### Round 2: Rebuttal
Each expert responds to the others' positions. Point out weaknesses, ask questions.

### Round 3: Synthesis
Experts seek common ground. Propose a conclusion that addresses concerns from all sides.

### Decision
After `debate.rounds` rounds:
- If consensus: State the conclusion + conditions for revisiting
- If deadlock: Report [DEBATE DEADLOCK] and ask the user for a decision

### State Management
Save debate state after each round:
```bash
bts debate log --topic "$ARGUMENTS" --round N --content "round summary"
```

To resume a previous debate:
```bash
bts debate resume <id>
```
Read the previous rounds before continuing.

### Output Format
```markdown
## Debate: [topic]

### Experts
1. [Role 1]: [Name/perspective]
2. [Role 2]: [Name/perspective]
3. [Role 3]: [Name/perspective]

### Round 1: Positions
[Expert 1]: ...
[Expert 2]: ...
[Expert 3]: ...

### Round 2: Rebuttals
[Expert 1]: ...
[Expert 2]: ...
[Expert 3]: ...

### Round 3: Synthesis
[Expert 1]: ...
[Expert 2]: ...
[Expert 3]: ...

### Conclusion
Decision: ...
Rationale: ...
Conditions for revisiting: ...
```
