---
name: forge-verify
description: >
  Verify a document for logical errors, contradictions, and unsupported claims.
  Includes mermaid flow path enumeration to find unspecified execution paths.
user-invocable: true
allowed-tools: Read Grep Glob Agent
argument-hint: "[file-path]"
context: fork
effort: max
---

# Logical Verification

Verify the specified document for logical correctness.

## Settings

Verification is the core quality gate — it uses the main session model by default.
If `agents.verifier` is explicitly set in `.forge/config/settings.yaml`, use that model instead.

## Steps

1. Read the target document fully

2. Spawn Agent(verifier) with the following prompt:

   ```
   You are a logical verification specialist. Read the document at $ARGUMENTS and check for:

   **Text-level verification:**
   - Contradictions: Does the document make conflicting claims?
   - Unsupported conclusions: Are conclusions drawn from insufficient evidence?
   - Causal errors: Are cause-effect relationships correctly established?
   - Missing premises: Are there hidden assumptions not stated?
   - Circular reasoning: Does any argument reference itself?

   **Flow-level verification (mermaid diagrams):**
   If the document contains mermaid diagrams (stateDiagram, flowchart, etc.):
   - Enumerate ALL possible paths from start to end in each diagram
   - For EACH path: is the behavior fully specified in the document text?
   - Flag paths where behavior is unspecified as GAPs
   - Check for dead-end states (states with no exit transition)
   - Check for orphan states (states with no entry transition)
   - Check that every error/failure state has a recovery or terminal path
   - Check for missing transitions: at each state, what happens on
     timeout? invalid input? resource exhaustion? concurrent access?

   **Report format:**
   For each issue found, classify severity:
   - critical: Factually impossible, self-contradicting, or execution path leads to undefined behavior
   - major: Logically flawed, or important execution path not specified
   - minor: Ambiguous or imprecise but not wrong

   Output your findings as a numbered list with severity tags.
   Include a summary: "Text issues: N. Flow path issues: N. Total paths analyzed: N."
   ```

3. Collect the verifier's findings
4. Report results to the user with severity counts
