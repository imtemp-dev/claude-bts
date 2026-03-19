---
name: bts-adjudicate
description: >
  Evaluate a debate conclusion for feasibility, over-engineering, and evidence quality.
  Decides whether to accept the conclusion or drive another debate round with
  targeted preparation (web search, code analysis, repo exploration).
user-invocable: true
allowed-tools: Read Write Edit Grep Glob Bash Agent WebSearch WebFetch mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "[debate-id or recipe-id]"
---

# Adjudicate: Debate Conclusion Review

Evaluate the debate conclusion for: $ARGUMENTS

## Step 1: Read Debate Output

Read the debate document and meta.json:
- `.bts/state/recipes/{id}/debates/{debate-id}/`
- Read all rounds + conclusion
- Read the current draft to understand the context

## Step 2: Evaluate on 6 Dimensions

For each dimension, assign: PASS / CONCERN / FAIL

### 2a. Topic Alignment
- Does the conclusion actually answer the ORIGINAL debate topic?
- Did the experts drift to a different subject?
- Is the decision directly applicable to the problem that triggered the debate?
- **Red flag**: Conclusion discusses technology X but debate was about technology Y.

### 2b. Scope Compliance
- Read `.bts/state/recipes/{id}/scope.md`
- Does the conclusion stay within the confirmed scope boundaries?
- Does it introduce items explicitly listed as "Out of Scope"?
- If the conclusion requires scope expansion → flag it.
  Do NOT silently accept scope creep. Report:
  "[SCOPE CHANGE] Debate conclusion requires {item} which is out of scope.
  User must confirm scope expansion before this conclusion can be accepted."
- **If user approves scope expansion**: update scope.md (add item to In Scope,
  remove from Out of Scope, keep Status: CONFIRMED), then ACCEPT the conclusion.
- **If user rejects scope expansion**: EXTEND the debate with instruction to
  find an alternative that stays within scope. Add to prep brief:
  "Must find a solution that does NOT require {out-of-scope item}."

### 2c. Feasibility
- Can this actually be built with the chosen tech stack?
- Are there known limitations, version incompatibilities, or missing libraries?
- Is the timeline realistic for the complexity?
- **Check**: Search the codebase for relevant dependencies and constraints.

### 2d. Over-engineering
- Is the proposed solution proportional to the problem?
- Could a simpler approach achieve 80%+ of the value?
- Are there unnecessary abstractions, premature optimizations, or gold-plating?
- **Red flags**: Custom framework when a library exists, distributed system for
  single-server load, complex caching when latency is not critical.

### 2e. Evidence Quality
- Did experts cite specific technologies, benchmarks, or real-world examples?
- Or did they argue from abstract principles without concrete data?
- Were trade-offs quantified (latency numbers, memory usage, complexity cost)?
- **Weak evidence**: "X is generally faster", "Y is best practice"
- **Strong evidence**: "X handles 10k req/s per node (benchmark: Z)", "Y reduced
  incident rate by 40% at Company W"

### 2f. Blind Spots
- What did the experts NOT discuss that they should have?
- Migration path from current state?
- Operational complexity (monitoring, debugging, on-call)?
- Team expertise — can the team actually maintain this?

## Step 3: Verdict

### If all dimensions PASS:
```
VERDICT: ACCEPT
```
Output the accepted conclusion. The recipe continues to the next step.

### If any dimension has CONCERN or FAIL:

```
VERDICT: EXTEND DEBATE
Round: N/3
```

Produce a **Preparation Brief** for the next debate round:

```markdown
## Preparation Brief for Debate Round N+1

### Issues Found
1. [Dimension]: [Specific concern]
2. [Dimension]: [Specific concern]

### Research Required
Before the next round, investigate:
- [ ] [Specific question — e.g., "What is Redis Cluster's actual failover time?"]
- [ ] [Code analysis — e.g., "Check if current ORM supports the proposed query pattern"]
- [ ] [Web search — e.g., "Benchmark comparisons of X vs Y under Z conditions"]
- [ ] [Repo analysis — e.g., "Clone github.com/foo/bar and analyze their auth implementation"]

### Focus for Next Round
The next debate round should specifically address:
1. [Question the experts must answer with evidence]
2. [Trade-off that needs quantification]
3. [Alternative approach that wasn't considered]

### Expert Guidance
- [Expert 1] should focus on: [specific aspect]
- [Expert 2] should prepare: [specific data]
- [Expert 3] should investigate: [specific concern]
```

## Step 4: Execute Preparation (if extending)

If VERDICT is EXTEND DEBATE:

1. Execute the research items from the Preparation Brief:
   - Use WebSearch for benchmark data, comparisons, best practices
   - Use Agent(Explore) to analyze referenced codebases
   - Use Grep/Glob to check current codebase constraints
   - Clone and analyze external repos if needed

2. Save preparation results to `.bts/state/recipes/{id}/debates/{debate-id}/prep-roundN.md`

3. Log to changelog:
   ```bash
   bts recipe log {id} --action adjudicate --result "extend: [reasons]"
   ```

4. Kick off a follow-up debate:
   - This is a NEW debate that explicitly references the previous conclusion
   - The debate topic should be: "[original topic] — Round 2: addressing [specific concerns]"
   - Provide the experts with:
     a. Previous debate conclusion (what was decided and why)
     b. Adjudication feedback (what was wrong with the conclusion)
     c. Preparation Brief + research results (new evidence gathered)
   - Experts must address ALL items in the brief with concrete evidence
   - Use Skill("bts-debate") with this combined context as arguments
   - Save the follow-up debate in a new directory: `debates/{next-seq}-{topic}-followup/`

## Step 5: Max Extensions Check

Track extension count by counting follow-up debate directories in
`.bts/state/recipes/{id}/debates/` that contain "-followup" in their name.
Each follow-up debate = 1 extension. This count persists on disk and
survives session breaks.

If this is the 3rd extension (3 follow-up debates exist):

```
VERDICT: ACCEPT WITH RESERVATIONS
```

Accept the best available conclusion. Do NOT extend further — diminishing returns.

**Save reservations** to `.bts/state/recipes/{id}/reservations.md`:
```markdown
# Reservations: {debate topic}

These concerns were not fully resolved after 3 debate extensions.
Review them during implementation if working in these areas.

## Unresolved Concerns
1. [Concern with context]
2. [Concern with context]

## Affected Files/Areas
- [file or area that touches this concern]
```

Log:
```bash
bts recipe log {id} --action adjudicate --output reservations.md --result "accepted with reservations after max extensions"
```

## Output

Always output one of:
- `<bts>ADJUDICATE: ACCEPT</bts>` — conclusion is solid, proceed
- `<bts>ADJUDICATE: EXTEND N/3</bts>` — another round needed, preparation brief attached
- `<bts>ADJUDICATE: ACCEPT WITH RESERVATIONS</bts>` — max extensions reached, proceed with caveats
