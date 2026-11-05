package hook

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
)

// Trial problem #52 at FIX DONE: a fix recipe names no code files, so the
// interrogate round records the git tree, and a new file in it after the
// adversaries' copies asks for a scoped round like an implementation does.
func TestFixDone_ANewFileAfterInterrogateNeedsARescope(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root, id := setupStopRoot(t)
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	src := filepath.Join(root, "src", "login.py")
	_ = os.MkdirAll(filepath.Dir(src), 0755)
	_ = os.WriteFile(src, []byte("ok = given == stored\n"), 0644)
	git("add", "src")
	git("commit", "-q", "-m", "base")

	recipe, _ := state.LoadRecipeState(root, id)
	recipe.Type, recipe.Phase = "fix", "test"
	_ = state.SaveRecipeState(root, recipe)
	dir := state.RecipeDir(root, id)
	_ = os.WriteFile(filepath.Join(dir, "fix-spec.md"), []byte("# Fix\n"), 0644)
	_ = os.WriteFile(src, []byte("ok = bcrypt.check(given, stored)\n"), 0644)

	seen, err := engine.SeeCode(root, id)
	if err != nil || seen == nil || !seen.Tree {
		t.Fatalf("a fix recipe records the git tree, got %+v %v", seen, err)
	}
	_ = state.SaveInterrogateRun(root, id, &state.InterrogateRun{PreparedAt: "t", VerifiedAt: "t", Round: 1, Seen: seen})
	_ = state.SaveProveRecord(root, id, &state.ProveRecord{NoSurface: "test fixture"})
	recordTests := func() {
		h, _ := state.RecipeImplHash(root, id)
		_ = os.WriteFile(filepath.Join(dir, "test-results.json"), []byte(fmt.Sprintf(
			`{"recipe_id":%q,"status":"pass","total":1,"passed":1,"recorded_by":"bts","impl_hash":%q}`, id, h)), 0644)
	}
	done := func() *HookOutput {
		t.Helper()
		recordTests()
		out, err := NewStopHandler().Handle(&HookInput{CWD: root, LastAssistantMessage: "<bts>FIX DONE</bts>"})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	// A regression test and a note are not code; the fix itself is unchanged.
	_ = os.MkdirAll(filepath.Join(root, "tests"), 0755)
	_ = os.WriteFile(filepath.Join(root, "tests", "test_login.py"), []byte("def test_x(): pass\n"), 0644)
	_ = os.WriteFile(filepath.Join(root, "NOTES.md"), []byte("why\n"), 0644)
	_ = os.WriteFile(filepath.Join(root, "src", "session.py"), []byte("TTL = 60\n"), 0644)
	out := done()
	if out.Decision != "block" || !strings.Contains(out.Reason, "src/session.py") || strings.Contains(out.Reason, "test_login.py") {
		t.Fatalf("a new code file after the copies must ask for a rescope, and only it; got %q %q", out.Decision, out.Reason)
	}

	_ = os.Remove(filepath.Join(root, "src", "session.py"))
	if out := done(); out.Decision == "block" {
		t.Fatalf("tests and notes alone keep the cheap path: %s", out.Reason)
	}
}
