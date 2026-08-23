package template

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	gitignoreComment = "# jig local data (not committed)"
	gitignorePattern = ".jig/local/"
)

// EnsureGitignore guarantees the project's .gitignore ignores jig local runtime
// data (.jig/local/) WITHOUT clobbering the user's existing rules.
//
// This is the safe replacement for shipping .gitignore as an overwritable
// template: jig must never own the user's .gitignore. Behavior:
//   - If .gitignore already ignores .jig/local/, it is left byte-for-byte
//     untouched (idempotent — never grows the file on repeated runs).
//   - Otherwise the jig block is appended, preserving every existing byte
//     (comments, blank lines, negation patterns, CRLF endings — all intact).
//   - If .gitignore does not exist, it is created with just the jig block.
func EnsureGitignore(root string) error {
	path := filepath.Join(root, ".gitignore")

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	existing := string(data)

	if gitignoreIgnoresLocal(existing) {
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

// gitignoreIgnoresLocal reports whether an exact ".jig/local/" rule is already
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
