---
name: bts-research
description: >
  Systematically research code, documentation, or external sources.
  Produces a structured research document. Use at the start of any recipe.
user-invocable: true
allowed-tools: Read Grep Glob Agent WebSearch WebFetch mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"topic or question\""
---

# Systematic Research

Research the given topic and produce a structured document.

## Steps

1. Spawn Agent(Explore) to investigate the codebase:
   ```
   Thoroughly explore the codebase related to: $ARGUMENTS

   Find:
   - Relevant files and their roles
   - Key functions, types, and interfaces
   - Dependencies and import relationships
   - Existing patterns and conventions
   - Configuration and environment requirements
   ```

2. For library/framework documentation, use Context7 MCP:
   - `mcp__context7__resolve-library-id` to find the library
   - `mcp__context7__get-library-docs` to fetch up-to-date docs and examples
   - This gives more accurate, structured results than web search for known libraries

3. If additional external research is needed, use WebSearch/WebFetch for:
   - Official documentation not covered by Context7
   - API references
   - Known issues or limitations

4. Synthesize findings into a structured document:
   ```markdown
   # Research: [topic]

   ## Current State
   - What exists now

   ## Key Components
   - Files, functions, types involved

   ## Dependencies
   - What depends on what

   ## Constraints
   - Limitations discovered

   ## Patterns
   - Conventions to follow
   ```

5. **Scope validation** (if inside a recipe with scope.md):
   - Compare research findings against scope.md
   - If research reveals that a scope item is infeasible, flag it:
     "[SCOPE ISSUE] Research found that {item} is not feasible because {reason}.
     Recommend scope adjustment."
   - If research reveals important items NOT in scope, note them:
     "Research suggests {item} may be needed but is currently out of scope."
   - These flags are included in the research document for /assess to act on.

6. Save to `.bts/state/{recipe-id}/01-research.md` if inside a recipe
