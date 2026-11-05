package hook

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/imtemp-dev/claude-bts/internal/template"
	"github.com/imtemp-dev/claude-bts/pkg/version"
)

func templateVersion(t *testing.T, root string) string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(root, ".bts", "config", ".template-version"))
	return strings.TrimSpace(string(data))
}

// Trial problem #55: a second worker's session start moved r-003's
// templates from 944b3e7 to 8bfa2c8 in the middle of the recipe, and
// true-shark's moved at its architect step.  While a recipe is active the
// session start deploys nothing and says, in one line, that the templates
// are behind the binary and how to move them.  The idempotent cleanup still
// runs, and the hook's stdout stays the JSON alone.
func TestSessionStart_HoldsTemplatesWhileARecipeIsActive(t *testing.T) {
	root, id := setupStopRoot(t) // blueprint in phase verify; templates recorded as "test"
	retired := filepath.Join(root, ".claude", "agents", "bts-simulator.md")
	_ = os.MkdirAll(filepath.Dir(retired), 0755)
	_ = os.WriteFile(retired, []byte("retired\n"), 0644)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	out, herr := NewSessionStartHandler().Handle(&HookInput{CWD: root, SessionID: ownerSID, Source: "startup"})
	os.Stdout = saved
	_ = w.Close()
	printed, _ := io.ReadAll(r)
	if herr != nil {
		t.Fatal(herr)
	}
	if len(printed) != 0 {
		t.Errorf("the hook must print nothing to stdout itself, got %q", printed)
	}

	if got := templateVersion(t, root); got != "test" {
		t.Errorf("templates must stay at the recorded version while %s is active, now %q", id, got)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "bts-prove", "SKILL.md")); !os.IsNotExist(err) {
		t.Errorf("no template may be deployed while a recipe is active (err=%v)", err)
	}
	if _, err := os.Stat(retired); !os.IsNotExist(err) {
		t.Errorf("the idempotent cleanup must still run (err=%v)", err)
	}

	if out.HookSpecificOutput == nil {
		t.Fatal("want session-start context")
	}
	ctx := out.HookSpecificOutput.AdditionalContext
	for _, want := range []string{"test", version.GetTemplateVersion(), id, "bts update"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("the context must name %q, got:\n%s", want, ctx)
		}
	}
	if strings.Contains(ctx, "Templates updated") {
		t.Errorf("nothing was updated, got:\n%s", ctx)
	}

	// `bts update` run by hand still deploys while the recipe is active.
	res, uerr := template.Update(root, version.GetTemplateVersion())
	if uerr != nil || !res.Deployed || templateVersion(t, root) != version.GetTemplateVersion() {
		t.Fatalf("bts update must deploy as before, got deployed=%v version=%q err=%v",
			res.Deployed, templateVersion(t, root), uerr)
	}
}

// An observer session holds the templates too: they are the project's, and
// the owner is mid-recipe on them.
func TestSessionStart_HoldsTemplatesForAnObserverToo(t *testing.T) {
	root, _ := setupStopRoot(t)
	setOwner(t, root, "r-001-test", ownerSID)
	out, err := NewSessionStartHandler().Handle(&HookInput{CWD: root, SessionID: observerSID, Source: "startup"})
	if err != nil {
		t.Fatal(err)
	}
	if got := templateVersion(t, root); got != "test" {
		t.Errorf("templates must not move for an observer session either, now %q", got)
	}
	if ctx := out.HookSpecificOutput.AdditionalContext; !strings.Contains(ctx, "bts update") || !strings.Contains(ctx, "is worked by session") {
		t.Errorf("the observer must get the hold line and its own notice, got:\n%s", ctx)
	}
}

// With no recipe in progress the session start deploys as before.
func TestSessionStart_DeploysWhenNoRecipeIsActive(t *testing.T) {
	root, id := setupStopRoot(t)
	r, _ := state.LoadRecipeState(root, id)
	r.Phase = "complete"
	_ = state.SaveRecipeState(root, r)

	out, err := NewSessionStartHandler().Handle(&HookInput{CWD: root, SessionID: ownerSID, Source: "startup"})
	if err != nil {
		t.Fatal(err)
	}
	if got := templateVersion(t, root); got != version.GetTemplateVersion() {
		t.Errorf("with no active recipe the templates must move to %q, got %q", version.GetTemplateVersion(), got)
	}
	if out.HookSpecificOutput == nil || !strings.Contains(out.HookSpecificOutput.AdditionalContext, "Templates updated") {
		t.Errorf("the deploy must be reported, got %+v", out.HookSpecificOutput)
	}
}

// The stop hook's phase changes are recorded with the bts version as well
// (trial problem #55), so the record covers every phase, not only those a
// skill entered with `bts recipe log --phase`.
func TestStop_CompletionRecordsThePhaseAndTheBtsVersion(t *testing.T) {
	root, id := setupImplementRoot(t)
	out, err := NewStopHandler().Handle(&HookInput{CWD: root, SessionID: ownerSID, LastAssistantMessage: "<bts>IMPLEMENT DONE</bts>"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision == "block" {
		t.Fatalf("the fixture must complete: %s", out.Reason)
	}
	entries, err := state.ReadChangelog(root, id)
	if err != nil {
		t.Fatalf("the completion must be written to the changelog: %v", err)
	}
	data, _ := os.ReadFile(state.ChangelogPath(root, id))
	last := entries[len(entries)-1]
	if last.Action != "phase" || !strings.Contains(last.Result, "complete") ||
		!strings.Contains(string(data), `"bts_version":"`+version.GetTemplateVersion()+`"`) {
		t.Fatalf("want a phase entry for complete with bts_version, got:\n%s", data)
	}
}
