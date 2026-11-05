package engine

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// InterrogateRescopeFiles is how many of the files the adversaries saw
// may change before the change counts as large (trial problem #52). A
// fix for a reproduced finding, or a review fix, touches one or two code
// files; a rework touches more.
const InterrogateRescopeFiles = 2

// SeeCode records the recipe's code as an interrogate round's adversaries
// copy it: the files CodeHash covers, or the git tree when the recipe
// names none (a fix recipe). nil outside a git repository for the latter.
func SeeCode(root, recipeID string) (*state.CodeSeen, error) {
	if files := CodeFiles(root, recipeID); len(files) > 0 {
		return state.SeeFiles(root, files)
	}
	return state.SeeTree(root)
}

// InterrogateDrift is what changed in the recipe's code since the current
// interrogate round's adversaries took their copies, counting only files
// that can carry behaviour: tests are left out (the interrogate skill asks
// for a regression test per fix) and so are documentation files. ok=false
// when the run recorded nothing to compare with: a run written before the
// record existed, or one taken outside a git repository for a recipe that
// names no code files.
func InterrogateDrift(root, recipeID string, run *state.InterrogateRun) (state.CodeChange, bool, error) {
	if run == nil || run.Seen == nil {
		return state.CodeChange{}, false, nil
	}
	c, err := run.Seen.Since(root, CodeFiles(root, recipeID))
	if err != nil {
		return state.CodeChange{}, false, err
	}
	return state.CodeChange{Added: rescopeCounted(c.Added), Changed: rescopeCounted(c.Changed)}, true, nil
}

// CheckInterrogateRescope is the freshness half of gate interrogate_clean
// (trial problem #52): when the code changed beyond the rule since the
// current round's copies were made, a scoped round must attack the change
// before the gate passes. The rule: a code file the adversaries did not
// see (a new one), or more than InterrogateRescopeFiles changed code
// files. A smaller change keeps the cheap path, the tests, falsifiers and
// drives rerun on the current code. It returns "" when no rescope is
// needed.
func CheckInterrogateRescope(change state.CodeChange, recipeID string) string {
	if len(change.Added) == 0 && len(change.Changed) <= InterrogateRescopeFiles {
		return ""
	}
	var parts []string
	if len(change.Added) > 0 {
		parts = append(parts, fmt.Sprintf("new code file(s) the adversaries never saw: %s", strings.Join(change.Added, ", ")))
	}
	if len(change.Changed) > InterrogateRescopeFiles {
		parts = append(parts, fmt.Sprintf("%d changed code files (more than %d): %s",
			len(change.Changed), InterrogateRescopeFiles, strings.Join(change.Changed, ", ")))
	}
	return fmt.Sprintf("The code changed too much since the adversaries' copies to rely on that interrogate run: %s. "+
		"Run a scoped one on the changed files: `bts interrogate prepare %s --rescope` (one adversary), spawn it, "+
		"then `bts interrogate verify %s`.", strings.Join(parts, "; "), recipeID, recipeID)
}

// rescopeCounted keeps the paths that count toward a rescope.
func rescopeCounted(paths []string) []string {
	var out []string
	for _, p := range paths {
		if !IsTestPath(p) && !isDocPath(p) {
			out = append(out, p)
		}
	}
	return out
}

// isDocPath reports whether a path is documentation, not code.
func isDocPath(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".md", ".mdx", ".markdown", ".rst", ".adoc", ".txt":
		return true
	}
	return false
}

// IsTestPath reports whether a path looks like a test or test support file.
//
// A directory counts when one of its dot-separated parts is a test word in
// any case, or a PascalCase name ending in Tests: .NET test projects are
// named `Work.Module.Tests.Unit` or `MyApp.UnitTests`, and Carepatron keeps
// its suites under `tests.component`.  A C# file named `*Tests.cs` counts
// too; `*Test.cs` does not, since `ABTest.cs` is ordinary code.  Without
// these, .NET unit tests read as production code to interrogate and
// mutation (trial problem #79, AGEN-53).
func IsTestPath(p string) bool {
	base := filepath.Base(p)
	for _, seg := range strings.Split(filepath.ToSlash(filepath.Dir(p)), "/") {
		for _, part := range strings.Split(seg, ".") {
			switch strings.ToLower(part) {
			case "test", "tests", "__tests__", "spec", "specs", "testdata":
				return true
			}
			if len(part) > len("Tests") && strings.HasSuffix(part, "Tests") {
				return true
			}
		}
	}
	switch {
	case base == "conftest.py",
		strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"),
		strings.HasSuffix(base, "_test.py"),
		strings.HasSuffix(base, "_test.go"),
		strings.HasSuffix(base, "Tests.cs"):
		return true
	}
	for _, mid := range []string{".test.", ".spec."} {
		if strings.Contains(base, mid) {
			return true
		}
	}
	return false
}

// CheckInterrogateClean is gate interrogate_clean: an interrogate run was
// prepared and verified, and every finding that failed again on a fresh
// copy is fixed on the current code or waived with a reason. It returns
// "" when the gate is met.
func CheckInterrogateClean(run *state.InterrogateRun, currentHash, recipeID string) string {
	if run == nil {
		return fmt.Sprintf("No interrogate run. Adversaries must try to break the change before it is done: "+
			"run /bts-interrogate %s (bts interrogate prepare, the adversaries, then bts interrogate verify).", recipeID)
	}
	if run.VerifiedAt == "" {
		return fmt.Sprintf("The interrogate run was prepared but not verified. Run `bts interrogate verify %s` once every adversary has written its report.", recipeID)
	}
	var open, stale []string
	for _, f := range run.Findings {
		switch f.Status {
		case state.InterrogateReproduced:
			open = append(open, f.ID)
		case state.InterrogateFixed:
			if currentHash != "" && (f.Recheck == nil || f.Recheck.CodeHash != currentHash) {
				stale = append(stale, f.ID)
			}
		}
	}
	var parts []string
	if len(open) > 0 {
		parts = append(parts, fmt.Sprintf("%s reproduced and not fixed", strings.Join(open, ", ")))
	}
	if len(stale) > 0 {
		parts = append(parts, fmt.Sprintf("the code changed after %s was rechecked", strings.Join(stale, ", ")))
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("Interrogate findings are open: %s. Fix each and run `bts interrogate verify %s --recheck`; "+
		"leave one unfixed only with the user's agreement, through `bts interrogate waive %s --finding <id> --reason \"...\"`.",
		strings.Join(parts, "; "), recipeID, recipeID)
}
