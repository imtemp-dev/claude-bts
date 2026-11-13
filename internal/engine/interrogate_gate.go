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
// for a regression test per fix) and so are documentation files.  When the
// run kept the code it saw (a git repository, bts 0.25 on), a file whose
// change is comments only, or only what a rebase or merge brought in from
// upstream, is set aside in CommentOnly or Upstream and does not count
// (trial problems #139 and #140), and each counted file carries the line
// ranges that changed.  ok=false when the run recorded nothing to compare
// with: a run written before the record existed, or one taken outside a
// git repository for a recipe that names no code files.
func InterrogateDrift(root, recipeID string, run *state.InterrogateRun) (state.CodeChange, bool, error) {
	if run == nil || run.Seen == nil {
		return state.CodeChange{}, false, nil
	}
	c, err := run.Seen.Since(root, CodeFiles(root, recipeID))
	if err != nil {
		return state.CodeChange{}, false, err
	}
	raw := state.CodeChange{Added: rescopeCounted(c.Added), Changed: rescopeCounted(c.Changed)}
	return refineDrift(root, run.Seen, raw), true, nil
}

// RescopeVerdict is what the freshness half of interrogate_clean says
// about a run.  Block is set when a scoped round must attack the change
// before the gate passes; Notice when the change is as large but the
// recipe has held its budget of scoped rounds, so the gate passes without
// one and says so.  Both are "" for a small change.
type RescopeVerdict struct {
	Block  string
	Notice string
}

// CheckInterrogateRescope is the freshness half of gate interrogate_clean
// (trial problem #52): when the code changed beyond the rule since the
// current round's copies were made, a scoped round must attack the change
// before the gate passes. The rule: a code file the adversaries did not
// see (a new one), or more than InterrogateRescopeFiles changed code
// files. A smaller change keeps the cheap path, the tests, falsifiers and
// drives rerun on the current code.
//
// budget is interrogate.max_rescope_rounds: once the run has held that
// many scoped rounds, a large change no longer requires another (trial
// problem #140: every fix round touched the same three files, so each
// forced the next and the recipe did not converge).  The tests, falsifiers
// and drives still rerun on the current code, and the Notice says that the
// budget, not a round, let the change through.
func CheckInterrogateRescope(change state.CodeChange, run *state.InterrogateRun, budget int, recipeID string) RescopeVerdict {
	if len(change.Added) == 0 && len(change.Changed) <= InterrogateRescopeFiles {
		return RescopeVerdict{}
	}
	var parts []string
	if len(change.Added) > 0 {
		parts = append(parts, fmt.Sprintf("new code file(s) the adversaries never saw: %s", strings.Join(change.Added, ", ")))
	}
	if len(change.Changed) > InterrogateRescopeFiles {
		parts = append(parts, fmt.Sprintf("%d changed code files (more than %d): %s",
			len(change.Changed), InterrogateRescopeFiles, strings.Join(change.Changed, ", ")))
	}
	what := strings.Join(parts, "; ") + notCounted(change)
	round := 1
	if run != nil {
		round = run.CurrentRound()
	}
	if held := round - 1; budget > 0 && held >= budget {
		return RescopeVerdict{Notice: fmt.Sprintf("The code changed beyond the rescope rule since round %d's copies (%s), "+
			"but %s has held %d scoped round(s), its budget (interrogate.max_rescope_rounds: %d), so no further round is required: "+
			"the tests, falsifiers and drives rerun on the current code instead. A scoped round is still open to you: "+
			"`bts interrogate prepare %s --rescope`.", round, what, recipeID, held, budget, recipeID)}
	}
	return RescopeVerdict{Block: fmt.Sprintf("The code changed too much since the adversaries' copies to rely on that interrogate run: %s. "+
		"Run a scoped one on the changed files: `bts interrogate prepare %s --rescope` (one adversary), spawn it, "+
		"then `bts interrogate verify %s`.", what, recipeID, recipeID)}
}

// notCounted names the files a change set aside, for a message.
func notCounted(c state.CodeChange) string {
	var parts []string
	if len(c.CommentOnly) > 0 {
		parts = append(parts, "comments only: "+strings.Join(c.CommentOnly, ", "))
	}
	if len(c.Upstream) > 0 {
		parts = append(parts, "changed only upstream: "+strings.Join(c.Upstream, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (not counted, " + strings.Join(parts, "; ") + ")"
}

// InterrogateRescope reads a run's drift and the project's budget and
// applies the rule.  ok=false when the run recorded nothing to compare
// with, as InterrogateDrift.
func InterrogateRescope(root, recipeID string, run *state.InterrogateRun) (RescopeVerdict, bool, error) {
	change, ok, err := InterrogateDrift(root, recipeID, run)
	if err != nil || !ok {
		return RescopeVerdict{}, ok, err
	}
	budget := DefaultSettings().Interrogate.MaxRescopeRounds
	if st, serr := LoadSettings(root); serr == nil {
		budget = st.Interrogate.MaxRescopeRounds
	}
	return CheckInterrogateRescope(change, run, budget, recipeID), true, nil
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
		"a fix that refuses the input another way than the finding's command asserts is closed by its regression test, through "+
		"`bts interrogate pin %s --finding <id> --cmd \"<test>\" --revert <patch> --expect \"<test id>\"`; "+
		"leave one unfixed only with the user's agreement, through `bts interrogate waive %s --finding <id> --reason \"...\"`.",
		strings.Join(parts, "; "), recipeID, recipeID, recipeID)
}
