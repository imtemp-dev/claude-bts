package template

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// excludeMarker opens the block EnsureExclude writes.  It is the comment the
// maintainer's own Carepatron-App checkout already carries, so a repository
// set up by hand is recognised and left alone.
const excludeMarker = "# claude-bts (bts) project files: kept out of git in every worktree of this repo"

// excludePatterns keep every file `bts init --exclude` writes out of git.  The
// first seven are the hand-written block; settings.local.json is where the
// hooks are registered, and a repository that does not already ignore it
// would otherwise show it as untracked.
var excludePatterns = []string{
	"/.bts/",
	"/.claude/agents/bts-*",
	"/.claude/hooks/bts-*",
	"/.claude/rules/bts-*",
	"/.claude/skills/bts-*/",
	"/.claude/commands/bts-*",
	"/.verify/",
	"/.claude/settings.local.json",
}

// ExcludePath returns the info/exclude file git reads for the repository at
// root.  For a linked worktree it is the shared one in the common git
// directory, so one block covers every worktree of that clone, including a
// Multica task's worktree, whose clone is the daemon's bare cache.
func ExcludePath(root string) (string, error) {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude").Output()
	if err != nil {
		return "", fmt.Errorf("%s is not inside a git repository", root)
	}
	return strings.TrimSpace(string(out)), nil
}

// EnsureExclude keeps bts's files out of git for the repository at root
// without touching a tracked file: it appends the bts block to the
// repository's info/exclude unless the block, or a "/.bts/" rule, is there.
// It reports whether it wrote anything.
//
// It runs before EnsureGitignore, which then finds .bts/local/ already
// ignored and leaves .gitignore alone (gitIgnoresLocal).
func EnsureExclude(root string) (bool, error) {
	path, err := ExcludePath(root)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	existing := string(data)
	for _, line := range strings.Split(existing, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == excludeMarker || line == "/.bts/" {
			return false, nil
		}
	}

	var b strings.Builder
	b.WriteString(existing)
	if existing != "" {
		if !strings.HasSuffix(existing, "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	b.WriteString(excludeMarker)
	b.WriteByte('\n')
	for _, p := range excludePatterns {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		return false, err
	}
	return true, nil
}

// excludeModeSkips are template paths `bts init --exclude` does not deploy.
// Both are conveniences for a repository that adopts bts (Context7 for the
// research skills, Markdown snippets for VS Code); in a product repository
// bts only visits, each would be a new file at the repository root.
var excludeModeSkips = []string{".mcp.json", ".vscode/"}

// SkipInExcludeMode reports whether `bts init --exclude` leaves a template
// path out.
func SkipInExcludeMode(path string) bool {
	for _, s := range excludeModeSkips {
		if path == s || (strings.HasSuffix(s, "/") && strings.HasPrefix(path+"/", s)) {
			return true
		}
	}
	return false
}
