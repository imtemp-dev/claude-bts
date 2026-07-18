---
name: bts-research
description: >
  Systematically research code, documentation, or external sources.
  Produces a structured research document. Use at the start of any recipe.
user-invocable: true
allowed-tools: Read Grep Glob Agent WebSearch WebFetch mcp__context7__resolve-library-id mcp__context7__get-library-docs
argument-hint: "\"topic or question\""
effort: high
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
   If `.bts/specs/recipes/` contains completed recipes, read their
   final.md to understand existing design decisions, patterns, and
   known issues that may affect this research.

2. For library/framework documentation, use Context7 MCP:
   - `mcp__context7__resolve-library-id` to find the library
   - `mcp__context7__get-library-docs` to fetch up-to-date docs and examples
   - This gives more accurate, structured results than web search for known libraries

3. If additional external research is needed, use WebSearch/WebFetch for:
   - Official documentation not covered by Context7
   - API references
   - Known issues or limitations

   Follow `.claude/rules/bts-evidence-policy.md` for source hierarchy
   (Context7 → official domains → site-filtered search). Every external
   claim recorded in the research document MUST carry a `Source:` line —
   uncited claims force /bts-verify to re-gather the same evidence
   later, one iteration per claim.

3.5 **Official Architecture Guidance (MANDATORY when the feature
   targets a framework/platform).** Identify the tech stack from
   scope.md / project-map.md, then fetch the platform vendor's OWN
   recommended architecture for this kind of feature (see the
   `## Official recommended architectures` section of
   `bts-evidence-policy.md` for examples: Android Guide to App
   Architecture, Apple SwiftUI data-flow guidance, React/Next.js docs,
   go.dev layout guidance, …).

   **Pin target versions FIRST**: read the project's dependency
   manifests (package.json, go.mod, build.gradle(.kts), Package.swift,
   pubspec.yaml, pyproject.toml / requirements.txt, Cargo.toml,
   Gemfile) and record the major version of every framework the
   guidance will cover. When resolving Context7 docs, match the
   detected major (`resolve-library-id` supports version selection).
   If the fetched guidance targets a DIFFERENT major than the project
   uses, record the mismatch explicitly — pattern advice often flips
   across majors (SwiftUI `ObservableObject` → `@Observable`, React
   class components → hooks). Never assume the pattern you remember
   matches the version the project pins.

   Record in the research document:
   - `Target versions: {framework}@{major}, … (from {manifest file})`
   - The recommended pattern's name and structure (layers, state
     ownership, data flow) with `Source:` lines on official domains,
     noting which framework major the guidance applies to
   - How it maps onto THIS feature's entities
   - Any parts of it that are overkill for the current scope
   This section is the required input for `/bts-architect` — one of its
   alternatives must be grounded in this guidance. If the platform has
   no official architecture guidance, state that explicitly with the
   queries attempted (`Gathered:` line) so architect knows the ground
   truth was checked, not skipped.

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

   ## Official Guidance
   - Target versions: {framework}@{major}, … (from {manifest file})
   - Recommended architecture: {name} for {framework}@{major} (Source: {official URL})
   - Structure: {layers / state ownership / data flow}
   - Mapping to this feature: {entities → pattern roles}
   - Not applicable parts: {list, with reasons}
   - Version mismatch: {none | guidance targets {major}, project pins {major} — implications}
   (or: "No official architecture guidance found. Gathered: [...]")
   ```

5. **Scope validation** (if inside a recipe with scope.md):
   - Compare research findings against scope.md
   - If research reveals that a scope item is infeasible, flag it:
     "[SCOPE ISSUE] Research found that {item} is not feasible because {reason}.
     Recommend scope adjustment."
   - If research reveals important items NOT in scope, note them:
     "Research suggests {item} may be needed but is currently out of scope."
   - These flags are included in the research document for /assess to act on.

6. Save to `.bts/specs/recipes/{id}/research/v1.md` if inside a recipe
   (increment to v2.md, v3.md… for follow-up research rounds). This is
   the path every downstream skill reads — blueprint resume, wireframe,
   domain-model, and architect all look for `research/v1.md`.
