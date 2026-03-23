---
name: bts-verify
description: >
  Verify a document for logical errors, contradictions, and unsupported claims.
  Use when you need to check if a spec or design document is logically sound.
user-invocable: true
allowed-tools: Read Grep Glob Agent
argument-hint: "[file-path]"
context: fork
effort: max
---

# Logical Verification

Verify the specified document for logical correctness.

## Settings

Read `.bts/config/settings.yaml`. If `agents.verifier` is set, pass that model
when spawning Agent(verifier) below (e.g., `model: opus`).

## Steps

1. Read the target document fully
2. Spawn Agent(verifier) with the following prompt:

   ```
   You are a logical verification specialist. Read the document at $ARGUMENTS and check for:

   - Contradictions: Does the document make conflicting claims?
   - Unsupported conclusions: Are conclusions drawn from insufficient evidence?
   - Causal errors: Are cause-effect relationships correctly established?
   - Missing premises: Are there hidden assumptions not stated?
   - Circular reasoning: Does any argument reference itself?

   For each issue found, classify severity:
   - critical: Factually impossible or self-contradicting
   - major: Logically flawed but not impossible
   - minor: Ambiguous or imprecise but not wrong

   Output your findings as a numbered list with severity tags.
   ```

3. Collect the verifier's findings
4. Report results to the user with severity counts
