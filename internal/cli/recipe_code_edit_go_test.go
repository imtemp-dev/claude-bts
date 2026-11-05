package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// editGoFixture is a recipe of the given type and phase in a project whose
// settings.yaml holds settings (none when empty).
func editGoFixture(t *testing.T, id, typ, phase, settings string) string {
	t.Helper()
	root := newRecipeFixture(t, id, phase, 1.0, 1, nil)
	r, _ := state.LoadRecipeState(root, id)
	r.Type = typ
	if err := state.SaveRecipeState(root, r); err != nil {
		t.Fatal(err)
	}
	if settings != "" {
		if err := os.WriteFile(filepath.Join(root, ".bts", "config", "settings.yaml"), []byte(settings), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func phaseOf(t *testing.T, root, id string) string {
	t.Helper()
	r, err := state.LoadRecipeState(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return r.Phase
}

const editGoOn = "implement:\n  require_edit_go: true\n"

// Trial problem #67: jongyoon's "wait for approval before editing" held on
// true-shark only because the agent asked.  With implement.require_edit_go
// on, a build recipe cannot enter skeleton, its first product code, until
// the user's go-ahead is a resolved decision code-edit-go; --force does not
// pass it, and the refusal names the commands that record it.
func TestRecipeLogPhase_RequireEditGo_HoldsTheBuildSkeleton(t *testing.T) {
	id := "r-067-build"
	root := editGoFixture(t, id, "build", "wireframe", editGoOn)

	out := runRecipeLog(t, root, id, "--phase", "skeleton")
	if !strings.Contains(out, "bts recipe decision hold "+id+" --key code-edit-go") ||
		!strings.Contains(out, "bts recipe decision resolve "+id+" code-edit-go") {
		t.Fatalf("the refusal must name the commands that record the go-ahead, got:\n%s", out)
	}
	if got := phaseOf(t, root, id); got != "wireframe" {
		t.Fatalf("the phase must not move without the go-ahead, now %q", got)
	}
	if _ = runRecipeLog(t, root, id, "--phase", "skeleton", "--force"); phaseOf(t, root, id) != "wireframe" {
		t.Fatal("--force must not pass the user's go-ahead")
	}

	// Held but not answered is still no go-ahead.
	if _, err := state.HoldDecision(root, id, &state.DecisionEvent{Key: "code-edit-go", Question: "Start writing code?"}); err != nil {
		t.Fatal(err)
	}
	if out := runRecipeLog(t, root, id, "--phase", "skeleton"); !strings.Contains(out, "bts recipe decision resolve "+id+" code-edit-go") {
		t.Fatalf("an open decision must be refused with the resolve command, got:\n%s", out)
	}
	if got := phaseOf(t, root, id); got != "wireframe" {
		t.Fatalf("an open decision is not a go-ahead, phase now %q", got)
	}

	if err := state.ResolveDecision(root, id, "code-edit-go", "go"); err != nil {
		t.Fatal(err)
	}
	_ = runRecipeLog(t, root, id, "--phase", "skeleton")
	if got := phaseOf(t, root, id); got != "skeleton" {
		t.Fatalf("a resolved go-ahead must let the skeleton start, phase %q", got)
	}
}

// For any other type the gate stands before the first implement phase.  A
// build recipe already passed it at skeleton, so its implement is not held.
func TestRecipeLogPhase_RequireEditGo_HoldsTheFirstImplementPhase(t *testing.T) {
	id := "r-067-blueprint"
	root := editGoFixture(t, id, "blueprint", "finalize", editGoOn)
	if out := runRecipeLog(t, root, id, "--phase", "implement"); !strings.Contains(out, "code-edit-go") {
		t.Fatalf("a blueprint's implement must be held, got:\n%s", out)
	}
	if got := phaseOf(t, root, id); got != "finalize" {
		t.Fatalf("phase must stay finalize, now %q", got)
	}

	build := "r-067-build-impl"
	broot := editGoFixture(t, build, "build", "finalize", editGoOn)
	_ = runRecipeLog(t, broot, build, "--phase", "implement")
	if got := phaseOf(t, broot, build); got != "implement" {
		t.Fatalf("a build recipe's implement is not its first code write, phase %q", got)
	}
}

// Off by default: a project with no setting moves as it always did.
func TestRecipeLogPhase_RequireEditGo_OffByDefault(t *testing.T) {
	id := "r-067-off"
	root := editGoFixture(t, id, "build", "wireframe", "")
	_ = runRecipeLog(t, root, id, "--phase", "skeleton")
	if got := phaseOf(t, root, id); got != "skeleton" {
		t.Fatalf("with the setting absent the skeleton must start, phase %q", got)
	}
	explicitOff := editGoFixture(t, "r-067-off2", "blueprint", "finalize", "implement:\n  require_edit_go: false\n")
	_ = runRecipeLog(t, explicitOff, "r-067-off2", "--phase", "implement")
	if got := phaseOf(t, explicitOff, "r-067-off2"); got != "implement" {
		t.Fatalf("with the setting false implement must start, phase %q", got)
	}
}

// bts doctor must know the key, or turning the gate on reads as a dead knob.
func TestCheckUnreadSettings_KnowsRequireEditGo(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".bts", "config"), 0755)
	_ = os.WriteFile(filepath.Join(root, ".bts", "config", "settings.yaml"), []byte(editGoOn), 0644)
	if issues := checkUnreadSettings(root); len(issues) != 0 {
		t.Fatalf("implement.require_edit_go is read, doctor reports %+v", issues)
	}
}
