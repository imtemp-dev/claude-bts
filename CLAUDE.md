# claude-bts

Working on bts itself, not using it in a project.

- `~/.local/bin/bts` runs every project's hooks, so install only from `main`, and only with a version bump.
  - Merge the working branch into local `main` after each change: `git fetch . <branch>:main` moves it only by fast-forward.
  - A release is an annotated tag on `main` in the existing style: first line `vX.Y.Z — <headline>`, then one paragraph; minor for features, patch for fixes.  The version string comes only from `git describe --tags`, so a build without a new tag reports the old version.
  - Build on `main` and swap the binary in atomically, since hooks may be running it: `git switch main && make build && install -m 0755 bin/bts ~/.local/bin/.bts.new && mv -f ~/.local/bin/.bts.new ~/.local/bin/bts`, then switch back.  `make install` copies over the running binary in place.
  - The new templates reach every project with no active recipe at its next session start, `bts update` or `bts recipe create`.  To go back, build and install the previous tag the same way.
- Check with `go vet ./...` and `go test ./...` (`make test` adds `-race`), and run `go run ./cmd/bts docs check` after changing the READMEs, `llms.txt`, `docs/` or the template inventory.
- Never run `bts init .` here: it deploys the `bts-*` templates into this repository's `.claude/`, where they duplicate in autocomplete (the reason for `1af0433`).
- `.claude/skills/` holds two maintainer skills that never ship:
  - `/reflect <project> <recipe-id>` after a real recipe run, to turn what went wrong or cost too much into changes, routed to a Go check before prose.
  - `/template-eval "<what changed>"` before installing a template change, to compare it with the base ref on the same task in scratch projects.  It runs several headless Opus sessions, so it asks before spending.
- Research notes and designs go in `docs/research/`, which `bts docs check` does not maintain.
