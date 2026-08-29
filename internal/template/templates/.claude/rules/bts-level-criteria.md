---
paths:
  - ".bts/**"
authoritative_for:
  - level_criteria_l1
  - level_criteria_l2
  - level_criteria_l3
---

# BTS Level Criteria

A level says what a document is, not how much of it there is.

Every criterion below is checked structurally by
`internal/engine/level3_structural.go` and every threshold **saturates**:
once met, more text cannot raise the score. That is deliberate. The
criteria used to be keyword counts over an unbounded document, so the
only way to raise a level was to write more — and `/bts-assess` turns
every unmet criterion into an IMPROVE instruction. One measured recipe
reached 2,184 lines and 17 verify rounds against a budget of 3 with that
pressure behind it.

**Delegation.** A recipe is a chain: `scope.md` holds the boundaries,
`domain.md` the invariants, `wireframe.md` the decomposition, the flow
and the recorded architect decision. A Level 1 or Level 2 criterion whose
content has a home upstream is satisfied by **naming that home** —
"the flow is in `wireframe.md` §3" locates the flow, it does not lose it.
Level 3 criteria never delegate: they are the blueprint's own job.

## Level 1: Understanding

- [ ] **components_listed** — at least two distinct parts are named
      (file paths or backticked identifiers). *Delegable.*
- [ ] **relationships_described** — the parts are related to each other:
      a dependency column, flow arrows, or an ordered sequence. A list
      without relations is a glossary. *Delegable.*
- [ ] **tech_stack_specified** — two or more distinct source-file kinds
      among the paths named, or a named technology in prose. The
      extensions of the files a spec touches are evidence; a word list
      is an assertion. *Delegable.*

## Level 2: Design

- [ ] All Level 1 criteria met
- [ ] **data_flow_defined** — input reaches output visibly: two or more
      flow arrows, or a mermaid flow diagram. *Delegable.*
- [ ] **error_strategy_defined** — a failure is named **and** so is what
      happens next (a status code, a fallback, a rejection). "Handle
      errors" names the failure and no disposition.
- [ ] **interfaces_described** — something at a boundary is named.
- [ ] **tech_choices_rationale** — a choice is recorded as a choice: an
      `architect-decision` block, a stated reason, or the alternative it
      was chosen over. *Delegable.*

## Level 3: Blueprint

A Level 3 document carries the part **code cannot cheaply falsify**.

Function signatures, type definitions and code scaffolding are not on
this list. A compiler produces them for free and settles them in seconds;
arguing about them in prose costs a verify round each and leaves a claim
behind that the next round has to re-check.

- [ ] All Level 2 criteria met
- [ ] **file_paths_specified** — at least three distinct units are
      named. Named means a file with an extension, or a backticked path
      for a directory: `read/write` and `client/server` are prose, and
      three such phrases must not read as three units.
- [ ] **invariants_owned** — **every** `INV-NNN` the document declares
      appears, **in the invariants section**, on a line that also names
      the file that keeps it. An invariant without an owner is one
      nobody keeps, or two places keeping it differently. The section
      matters because an owner row and a falsifier row look identical to
      a checker — both name a file — so an unscoped check let §6 answer
      for §2. A design that genuinely has no invariant passes by saying
      so under an invariants heading, the same rule
      `uncertainties_declared` uses. Having no such section does not
      pass: silence here is not the same claim, and it must not be the
      cheap way past this criterion and `falsifiers_assigned`.
- [ ] **boundary_contracts** — a boundary is named (contract, wire,
      schema, payload, DTO, endpoint, migration) and its shape is pinned
      in a table or a fence. What crosses a boundary is the expensive
      thing to get wrong: both sides get rebuilt, and once shipped a
      migration is involved.
- [ ] **irreversible_order** — two or more ordered steps **and** what
      undoes them. The steps have to be steps: a numbered list, numbered
      table rows, or numbered `###` sub-headings. Numbering the
      document's own top-level sections is a table of contents, and
      counting it meant the skeleton satisfied this criterion before
      anyone wrote an order. A wrong order here is not a code fix; it is
      a production incident with no rollback.
- [ ] **falsifiers_assigned** — **every** declared invariant appears on a
      line that also names what would prove it false: a test, a spec, a
      probe, an observation. The name has to be a named artifact — a
      file with an extension, a backticked identifier, or a backticked
      command — not a word like "tests" and a slash somewhere in the
      same row. A test file's own name counts as naming a test:
      `foo_test.go`, `test_foo.py`, `user_spec.rb`. Names only: what the
      assertion should contain is decided while writing the test, not
      here. A document with no invariants passes as above.
- [ ] **uncertainties_declared** — a `## Known Uncertainties` section
      exists, and every `### U-NNN` entry carries a `Why-deferred:` or
      `Opens-with:` line. A section declaring nothing open passes;
      having no section does not. Silence is what lets an unopened
      question read as a settled one.

### Why falsifiers, and not test scenarios

A claim in a document about code that does not exist yet has no truth
value until something executes. Writing the expected threshold into the
spec does not settle it — it just moves the argument earlier, where it
costs more and where three verify rounds can disagree about it.

So the blueprint names **what would open the box** and stops there. The
inverse matters too: a load-bearing claim with no falsifier is not
verified because three agents read it and agreed. It is unopened.

## Level Assessment

`bts verify <file>` reports the level and the unmet criteria. The score
is structural and cheap; `/bts-assess` weighs it in context.
