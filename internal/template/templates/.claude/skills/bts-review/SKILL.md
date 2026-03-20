---
name: bts-review
description: >
  Review implementation code for quality, security, and patterns.
  Basic review by default, or focused review with category argument
  (security, performance, patterns).
user-invocable: true
allowed-tools: Read Grep Glob Agent
argument-hint: "[security|performance|patterns] [file-path]"
---

# Code Review

Review code for: $ARGUMENTS

## Step 1: Determine Review Mode

Parse $ARGUMENTS:
- If first word is `security`, `performance`, or `patterns` → **specialized mode**
  Remaining words = file scope (or all if empty)
- Otherwise → **basic mode**, arguments = file scope (or all if empty)

## Step 2: Identify Review Scope

**If inside a recipe** (check `.bts/state/recipes/` for active recipe with tasks.json):
- Read tasks.json for the list of implemented files
- If file scope given → filter to matching files only
- If no scope → review all files from tasks.json

**If standalone** (no recipe context):
- If file scope given → review those files/directories
- If no scope → ask user which files to review

## Step 3: Review

Spawn Agent(reviewer) with the appropriate prompt based on mode:

### Basic Mode (no category argument)

```
You are a code quality reviewer. Read the specified files and check for:

**Error Handling**
- Uncaught exceptions, ignored errors, empty catch blocks
- Generic catch-all without specific handling
- Missing error propagation

**Input Validation**
- Unvalidated user input at API boundaries
- Missing boundary checks (array length, number range)
- Type coercion issues

**Resource Management**
- Unclosed connections, file handles, streams
- Missing defer/finally/cleanup
- Memory leak patterns (event listeners not removed, growing caches)

**Code Smells**
- Dead code (unreachable, unused exports)
- Deep nesting (>3 levels)
- Magic numbers/strings that should be constants
- Significant code duplication

**Null Safety**
- Missing null/undefined/nil checks before access
- Optional chaining opportunities missed

**Logging**
- Error paths without logging
- Sensitive data in log output (passwords, tokens, PII)

For each finding:
- Classify as: critical / major / minor / info
- Tag with ID: [CRT-001], [MAJ-001], [MIN-001], [INF-001]
- Include file path and line context
- Provide a specific fix suggestion
```

### Security Mode (`security`)

```
You are a security code reviewer. Read the specified files and check for:

**Injection**
- SQL injection (string concatenation in queries)
- Command injection (unsanitized input in exec/spawn)
- Path traversal (user input in file paths without validation)
- Template injection (user input in template strings)

**Authentication & Authorization**
- Authentication bypass opportunities
- Missing authorization checks on endpoints
- Token validation gaps (expiry, signature, scope)

**Data Exposure**
- Secrets hardcoded in source (API keys, passwords, tokens)
- Sensitive data in API responses (internal IDs, stack traces)
- Sensitive data in logs
- Missing data sanitization before storage

**Input Sanitization**
- XSS (unsanitized HTML output)
- CSRF (missing token validation on state-changing requests)
- Header injection
- Open redirect

**Cryptography**
- Weak algorithms (MD5, SHA1 for security purposes)
- Hardcoded encryption keys
- Predictable random values for security tokens

Classify, tag, and suggest fixes for each finding.
```

### Performance Mode (`performance`)

```
You are a performance code reviewer. Read the specified files and check for:

**Database/Query**
- N+1 query patterns (loop with individual queries)
- Missing index hints on frequent queries
- Unbounded SELECT (no LIMIT on potentially large tables)
- Unnecessary eager loading

**Memory**
- Unnecessary object allocations in hot paths
- Large object copies where references would suffice
- Growing collections without bounds (caches, buffers)
- Missing object pooling for frequently created objects

**I/O**
- Blocking I/O in async context
- Missing connection pooling
- Synchronous operations where async is available
- Missing request/response streaming for large payloads

**Algorithm**
- O(n²) or worse complexity where O(n log n) or O(n) is possible
- Unnecessary iterations (multiple passes where one suffices)
- Missing early returns/short circuits

Classify, tag, and suggest fixes for each finding.
```

### Patterns Mode (`patterns`)

```
You are a code patterns reviewer. Read the specified files.
If `.bts/state/layers/{name}.md` exists for the relevant layer,
read its "Key Patterns" section for established conventions.
Otherwise scan the existing codebase for patterns. Then check for:

**Naming Conventions**
- Variable/function/class naming consistency with rest of project
- File naming conventions (kebab-case, camelCase, etc.)

**File Structure**
- File organization alignment with project structure
- Import ordering consistency
- Module boundary respect

**Error Patterns**
- Error handling approach consistent with project's pattern
- Custom error types used where project defines them

**API Consistency**
- Endpoint naming patterns (RESTful conventions)
- Response format consistency (envelope, error format)
- Function signature patterns (parameter ordering, return types)

Classify, tag, and suggest fixes for each finding.
```

## Step 4: Generate Report

Format findings as review.md:

```markdown
# Code Review: {topic or scope}

Generated: {ISO8601}
Recipe: {id} (if inside recipe)
Mode: {basic|security|performance|patterns}
Scope: {file list or "all implemented files"}

## Summary
- Critical: N
- Major: N
- Minor: N
- Info: N

## Findings

### Critical
1. [CRT-001] **{title}** in `{file}:{line}`
   {code context}
   → {fix suggestion}

### Major
...

### Minor
...

### Info
...
```

If inside a recipe, save to `.bts/state/recipes/{id}/review.md`:
```bash
bts recipe log {id} --action review --output review.md --result "N critical, N major, N minor"
```

If standalone, output directly to user.

Review is a **report**, not a gate. It does not block completion.
Findings are recommendations for the developer to address.
