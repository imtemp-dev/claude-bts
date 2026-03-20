---
description: Run a bts recipe (analyze, design, blueprint, fix, debug)
argument-hint: "<type> \"description\""
---

Parse the first argument as recipe type: analyze, design, blueprint, fix, or debug.
Use Skill("bts-recipe-{type}") with remaining arguments.

Examples:
  /recipe analyze "auth system"       → Skill("bts-recipe-analyze")
  /recipe design "OAuth2 login"       → Skill("bts-recipe-design")
  /recipe blueprint "API endpoints"   → Skill("bts-recipe-blueprint")
  /recipe fix "login bcrypt error"    → Skill("bts-recipe-fix")
  /recipe debug "session drops after 5min" → Skill("bts-recipe-debug")
