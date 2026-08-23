---
description: Run a jig recipe (map, design, spec, fix, debug)
argument-hint: "<type> \"description\""
---

Parse the first argument as recipe type: map, design, spec, fix, or debug.
Use Skill("jig-{type}") with remaining arguments.

Each recipe also has its own slash command — `/jig-spec` is identical to
`/jig spec`. Use whichever reads better; this dispatcher exists so `/jig`
alone lists the whole set.

| Type | Level | Produces |
|------|-------|----------|
| `map` | 1 — understanding | Verified analysis of an existing system |
| `design` | 2 — design | Verified design document |
| `spec` | 3 — implementation | Level 3 spec → code → tests |
| `fix` | — | Fix spec for a known bug → code → tests |
| `debug` | — | Root cause for an unknown bug → fix → code → tests |

Examples:
  /jig map "auth system"                 → Skill("jig-map")
  /jig design "OAuth2 login"             → Skill("jig-design")
  /jig spec "API endpoints"              → Skill("jig-spec")
  /jig fix "login bcrypt error"          → Skill("jig-fix")
  /jig debug "session drops after 5min"  → Skill("jig-debug")
