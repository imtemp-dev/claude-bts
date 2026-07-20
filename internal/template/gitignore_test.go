package template

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realistic user .gitignore including order-sensitive negation patterns.
const userGitignore = `# Build
bin/
dist/

# IDE
.idea/
.vscode/

# Exception: keep a template shipped by bts
!internal/template/templates/.vscode/
!internal/template/templates/.vscode/**

# Environment
.env
`

func writeGitignore(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(content), 0644); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}
}

func readGitignore(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	return string(data)
}

// The core regression: bts must never destroy the user's existing rules.
func TestEnsureGitignore_PreservesExistingRules(t *testing.T) {
	root := t.TempDir()
	writeGitignore(t, root, userGitignore)

	if err := EnsureGitignore(root); err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}

	got := readGitignore(t, root)

	// Every original line must survive, in order.
	if !strings.HasPrefix(got, userGitignore) {
		t.Fatalf("existing content was not preserved verbatim.\n--- got ---\n%s", got)
	}
	// Order-sensitive negation patterns must still be present.
	for _, want := range []string{
		"bin/", "dist/", ".idea/", ".env",
		"!internal/template/templates/.vscode/",
		"!internal/template/templates/.vscode/**",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lost user rule %q after EnsureGitignore", want)
		}
	}
	// And the bts rule must now be present exactly once.
	if n := strings.Count(got, "\n"+gitignorePattern+"\n"); n != 1 {
		t.Errorf("expected exactly one %q line, got %d.\n%s", gitignorePattern, n, got)
	}
}

// Running it repeatedly (every SessionStart / update) must not grow the file.
func TestEnsureGitignore_Idempotent(t *testing.T) {
	root := t.TempDir()
	writeGitignore(t, root, userGitignore)

	if err := EnsureGitignore(root); err != nil {
		t.Fatalf("first EnsureGitignore: %v", err)
	}
	after1 := readGitignore(t, root)

	for i := 0; i < 5; i++ {
		if err := EnsureGitignore(root); err != nil {
			t.Fatalf("EnsureGitignore run %d: %v", i+2, err)
		}
	}
	after2 := readGitignore(t, root)

	if after1 != after2 {
		t.Errorf("EnsureGitignore is not idempotent.\n--- after 1 ---\n%s\n--- after 6 ---\n%s", after1, after2)
	}
	if n := strings.Count(after2, gitignorePattern); n != 1 {
		t.Errorf("pattern duplicated after repeated runs: %d occurrences", n)
	}
}

// A file that already ignores .bts/local/ must be left byte-for-byte untouched.
func TestEnsureGitignore_AlreadyPresentUntouched(t *testing.T) {
	root := t.TempDir()
	original := userGitignore + "\n# bts local data (not committed)\n" + gitignorePattern + "\n"
	writeGitignore(t, root, original)

	if err := EnsureGitignore(root); err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}

	if got := readGitignore(t, root); got != original {
		t.Errorf("file with existing rule was modified.\n--- want ---\n%s\n--- got ---\n%s", original, got)
	}
}

// A tab-indented / whitespace-padded existing rule still counts as present.
func TestEnsureGitignore_DetectsPaddedRule(t *testing.T) {
	root := t.TempDir()
	original := "node_modules/\n  " + gitignorePattern + "  \n"
	writeGitignore(t, root, original)

	if err := EnsureGitignore(root); err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}
	if got := readGitignore(t, root); got != original {
		t.Errorf("padded existing rule not detected; file changed.\n--- got ---\n%s", got)
	}
}

// No .gitignore yet → create one containing just the bts block.
func TestEnsureGitignore_CreatesWhenMissing(t *testing.T) {
	root := t.TempDir()

	if err := EnsureGitignore(root); err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}

	got := readGitignore(t, root)
	want := gitignoreComment + "\n" + gitignorePattern + "\n"
	if got != want {
		t.Errorf("created .gitignore mismatch.\n--- want ---\n%q\n--- got ---\n%q", want, got)
	}
	// No leading blank line when the file was empty/absent.
	if strings.HasPrefix(got, "\n") {
		t.Errorf("created .gitignore starts with a blank line: %q", got)
	}
}

// Existing content without a trailing newline must not have its last line merged.
func TestEnsureGitignore_NoTrailingNewline(t *testing.T) {
	root := t.TempDir()
	writeGitignore(t, root, "node_modules/\n.env") // no trailing newline

	if err := EnsureGitignore(root); err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}

	got := readGitignore(t, root)
	if strings.Contains(got, ".env"+gitignorePattern) || strings.Contains(got, ".env# bts") {
		t.Errorf("last user line was merged with bts block: %q", got)
	}
	if !strings.Contains(got, "\n.env\n") {
		t.Errorf("expected .env preserved on its own line: %q", got)
	}
	if !strings.Contains(got, "\n"+gitignorePattern+"\n") {
		t.Errorf("bts rule not appended cleanly: %q", got)
	}
}

// CRLF files must be preserved verbatim — proving we never rewrite/normalize
// (the old bufio.Scanner path silently converted CRLF→LF on every line).
func TestEnsureGitignore_PreservesCRLF(t *testing.T) {
	root := t.TempDir()
	crlf := "bin/\r\ndist/\r\n"
	writeGitignore(t, root, crlf)

	if err := EnsureGitignore(root); err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}

	got := readGitignore(t, root)
	if !strings.HasPrefix(got, crlf) {
		t.Errorf("CRLF content was normalized/clobbered.\n--- got ---\n%q", got)
	}
}

// Guard: bts must not ship a .gitignore as an overwritable template again.
// Shipping it would let DeployForce clobber the user's real .gitignore.
func TestEmbeddedTemplatesHasNoGitignore(t *testing.T) {
	tmplFS, err := EmbeddedTemplates()
	if err != nil {
		t.Fatalf("EmbeddedTemplates: %v", err)
	}

	err = fs.WalkDir(tmplFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Base(path) == ".gitignore" {
			t.Errorf("templates ship an overwritable .gitignore at %q; it would clobber the user's file on DeployForce", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk templates: %v", err)
	}
}
