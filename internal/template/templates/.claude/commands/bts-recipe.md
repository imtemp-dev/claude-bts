---
description: Run a bts recipe (analyze, design, blueprint, fix)
argument-hint: "<type> \"description\""
---

Parse the first argument as recipe type: analyze, design, blueprint, or fix.
Use Skill("bts-recipe-{type}") with remaining arguments.

Examples:
  /recipe analyze "auth system"     → Skill("bts-recipe-analyze")
  /recipe design "OAuth2 login"     → Skill("bts-recipe-design")
  /recipe blueprint "API endpoints" → Skill("bts-recipe-blueprint")
  /recipe fix "login bcrypt error"  → Skill("bts-recipe-fix")
