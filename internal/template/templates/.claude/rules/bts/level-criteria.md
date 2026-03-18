---
paths:
  - ".bts/**"
---

# BTS Level Criteria

## Level 1: Understanding
A document achieves Level 1 when it demonstrates understanding of the system:
- [ ] Main components/modules are listed and described
- [ ] Relationships between components are explained
- [ ] Technology stack is specified (language, framework, database, etc.)
- [ ] Core data model or schema is outlined

## Level 2: Design
A document achieves Level 2 when it provides a concrete design:
- [ ] All Level 1 criteria met
- [ ] New components to build are defined with responsibilities
- [ ] Data flow is specified (input → processing → output for each major flow)
- [ ] Error handling strategy is defined (not just "handle errors" but how)
- [ ] Key interfaces (API endpoints, function contracts) are described
- [ ] Technology choices have stated rationale (why this over alternatives)

## Level 3: Implementation-Ready
A document achieves Level 3 when an AI can implement directly from it:
- [ ] All Level 2 criteria met
- [ ] Every file path is specified (create new / modify existing)
- [ ] Every function has signature (name, typed parameters, return type)
- [ ] Data types and interfaces are formally defined
- [ ] Connection points are specific (which file, which function, which line)
- [ ] Every error case is enumerated with handling strategy
- [ ] Edge cases are listed (empty, null, concurrent, large data, etc.)
- [ ] Code scaffolding is included (skeleton structure showing how pieces connect)
- [ ] Test scenarios are defined (happy path + error paths + edge cases)

## Level Assessment

Run `bts verify <file>` to get an automated level assessment.
The score is approximate — /assess provides a more nuanced evaluation
by reading the document in context.
