# Changelog

## v0.15.0 — renamed to jig

bts is now **jig**. A jig is the fixture that holds the work and guides the
cut, which is what this tool does for a spec: build it before you cut the
code, so the errors are text edits instead of debugging sessions.

Upgrading is the whole migration. The first `jig` command in a project adopts
what is already there — see "Upgrading from bts" in the README.

### Renamed

- Binary `bts` → `jig`; state directory `.bts/` → `.jig/`
- Skills, agents, rules and hooks: `bts-*` → `jig-*`
- Recipe commands lose the `recipe-` infix, and two are renamed so the type
  and the skill suffix are the same string:

  | Before | After | Level |
  |--------|-------|-------|
  | `/bts-recipe-analyze` | `/jig-map` | 1 — understanding |
  | `/bts-recipe-design` | `/jig-design` | 2 — design |
  | `/bts-recipe-blueprint` | `/jig-spec` | 3 — implementation |
  | `/bts-recipe-fix` | `/jig-fix` | — |
  | `/bts-recipe-debug` | `/jig-debug` | — |

- Recipe types stored in `recipe.json`: `blueprint` → `spec`, `analyze` → `map`

### Added

- Top-level shortcuts for the commands typed most often. The nested forms
  stay, so existing scripts keep working.

  | Shortcut | Long form |
  |----------|-----------|
  | `jig` | `jig recipe status` |
  | `jig ls` | `jig recipe list` |
  | `jig new` | `jig recipe create` |
  | `jig log` | `jig recipe log` |
  | `jig ask` | `jig recipe decision hold` |
  | `jig ans` | `jig recipe decision resolve` |

- Homebrew formula is now pinned to `jig`, so `brew install jig` matches the
  binary name and the docs.
- The repository and Go module drop the `claude-` prefix
  (`github.com/imtemp-dev/jig`). jig only drives Claude Code today, but the
  name no longer forecloses other hosts. The goreleaser project name is
  pinned rather than inferred from the repo, so the archive names install.sh
  downloads cannot drift with it.

### Migration

Handled automatically, with no manual step:

- `.bts/` is renamed to `.jig/` on the first command, and `.gitignore` is
  repointed at it — the same path `.forge/` took in an earlier rename.
- Retired recipe types normalize on load. Without this a legacy `blueprint`
  would stop matching the `spec` branch and silently lose its domain-model,
  wireframe and simulate preconditions.
- Template files under a retired prefix are removed before the current set
  deploys. This runs on the session-start auto-update path too, not just
  `jig update` — the binary is usually upgraded out from under a project.
- Hook commands in `.claude/settings.local.json` are repointed at the
  `jig-handle-*` scripts. These are absolute paths, and a hook that cannot
  run is a gate that fails open.

Documents already written keep working. `<bts-findings>` and
`<bts-decision>` blocks, `<bts>DONE</bts>` markers and `[!BTS-COMMENT]` /
`[!BTS-BLOCK]` / `[!BTS-Q]` review comments are all still read, so a recipe
in flight does not need to be finished or restarted before upgrading.

### Fixed

- The end-to-end suite stopped at test 33 and reported success for the run
  up to that point. Three checks still asserted the pre-v0.14 completion
  contract — a single clean round finalized, and a finding's absence closed
  it — which v0.14.0 deliberately replaced with replicated passes across
  every dimension, and with `unreported` as a distinct state from `fixed`.
  All 46 tests now run.
