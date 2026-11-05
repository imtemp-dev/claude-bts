---
description: Run a bts recipe (analyze, design, blueprint, build, fix, debug)
argument-hint: "<type> \"description\""
---

Parse the first argument as recipe type: analyze, design, blueprint, build, fix, or debug.
Use Skill("bts-recipe-{type}") with remaining arguments.

If no type is given, or the first argument is not one of them, choose the
type with the table in `.claude/rules/bts-recipe-protocol.md` § Choosing a
recipe type: read it, say in one line which type and which row decided it,
then dispatch.  A change to committed code that adds or alters behaviour is
build, not fix.

Examples:
  /recipe analyze "auth system"       → Skill("bts-recipe-analyze")
  /recipe design "OAuth2 login"       → Skill("bts-recipe-design")
  /recipe blueprint "API endpoints"   → Skill("bts-recipe-blueprint")
  /recipe build "Slack relay"         → Skill("bts-recipe-build")
  /recipe fix "login bcrypt error"    → Skill("bts-recipe-fix")
  /recipe debug "session drops after 5min" → Skill("bts-recipe-debug")
  /recipe "accept @team-ai mentions in the relay" → build (a behaviour change to committed code)
