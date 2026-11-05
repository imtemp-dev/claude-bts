package template

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	gitignoreComment = "# bts local data (not committed)"
	gitignorePattern = ".bts/local/"
)

// EnsureGitignore guarantees the project's .gitignore ignores bts local runtime
// data (.bts/local/) WITHOUT clobbering the user's existing rules.
//
// This is the safe replacement for shipping .gitignore as an overwritable
// template: bts must never own the user's .gitignore. Behavior:
//   - If .gitignore already ignores .bts/local/, it is left byte-for-byte
//     untouched (idempotent — never grows the file on repeated runs).
//   - If git already ignores .bts/local/ by any other source (a parent
//     .gitignore, .git/info/exclude, a global excludes file), .gitignore is
//     not touched either.  A shared repository keeps bts out of git through
//     an exclude file, and a line in its tracked .gitignore would put bts
//     into a pull request.
//   - Otherwise the bts block is appended, preserving every existing byte
//     (comments, blank lines, negation patterns, CRLF endings — all intact).
//   - If .gitignore does not exist, it is created with just the bts block.
func EnsureGitignore(root string) error {
	path := filepath.Join(root, ".gitignore")

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	existing := string(data)

	if gitignoreIgnoresLocal(existing) || gitIgnoresLocal(root) {
		return nil // already ignored — leave the file untouched
	}

	var b strings.Builder
	b.WriteString(existing)
	if existing != "" {
		if !strings.HasSuffix(existing, "\n") {
			b.WriteByte('\n') // finish the user's unterminated last line
		}
		b.WriteByte('\n') // blank line separating our block from theirs
	}
	b.WriteString(gitignoreComment)
	b.WriteByte('\n')
	b.WriteString(gitignorePattern)
	b.WriteByte('\n')

	return os.WriteFile(path, []byte(b.String()), 0644)
}

// gitignoreIgnoresLocal reports whether an exact ".bts/local/" rule is already
// present on its own line. It splits on "\n" (not bufio.Scanner) so a
// pathologically long line can never truncate the check, and trims trailing
// "\r" so CRLF files are matched correctly.
func gitignoreIgnoresLocal(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == gitignorePattern {
			return true
		}
	}
	return false
}

// gitIgnoresLocal reports whether git, through any of its ignore sources,
// already ignores .bts/local/ under root.  Outside a git repository, or
// without git, it reports false and EnsureGitignore writes the rule as
// before.
func gitIgnoresLocal(root string) bool {
	return exec.Command("git", "-C", root, "check-ignore", "-q", gitignorePattern+"probe").Run() == nil
}
