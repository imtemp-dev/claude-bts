# claude-bts

Working on bts itself, not using it in a project.

- `make install` copies this checkout's binary to `~/.local/bin/bts`, which every project's hooks run.  Installing a branch puts its templates in front of every project on its next `bts init` or `bts update`; return with `git switch main && make install`.
- Check with `go vet ./...` and `go test ./...` (`make test` adds `-race`), and run `go run ./cmd/bts docs check` after changing the READMEs, `llms.txt`, `docs/` or the template inventory.
- Never run `bts init .` here: it deploys the `bts-*` templates into this repository's `.claude/`, where they duplicate in autocomplete (the reason for `1af0433`).
- `.claude/skills/` holds two maintainer skills that never ship:
  - `/reflect <project> <recipe-id>` after a real recipe run, to turn what went wrong or cost too much into changes, routed to a Go check before prose.
  - `/template-eval "<what changed>"` before installing a template change, to compare it with the base ref on the same task in scratch projects.  It runs several headless Opus sessions, so it asks before spending.
- Research notes and designs go in `docs/research/`, which `bts docs check` does not maintain.
