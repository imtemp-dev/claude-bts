package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/orca"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/pflag"
)

func TestContinueSkill(t *testing.T) {
	cases := []struct {
		typ, phase, want string
	}{
		{"build", "finalize", "bts-implement"},
		{"blueprint", "implement", "bts-implement"},
		{"debug", "test", "bts-implement"},
		{"build", "wireframe", "bts-recipe-build"},
		{"blueprint", "draft", "bts-recipe-blueprint"},
		{"fix", "draft", "bts-recipe-fix"},
		{"fix", "implement", "bts-recipe-fix"},
		{"", "draft", "bts-recipe-blueprint"},
	}
	for _, c := range cases {
		got, err := continueSkill(&state.RecipeState{ID: "r-1", Type: c.typ, Phase: c.phase})
		if err != nil || got != c.want {
			t.Errorf("%s/%s: %q, %v; want %q", c.typ, c.phase, got, err, c.want)
		}
	}
	for _, r := range []*state.RecipeState{
		{ID: "r-1", Type: "build", Phase: "complete"},
		{ID: "r-1", Type: "build", Phase: "cancelled"},
		{ID: "r-1", Type: "design", Phase: "finalize"},
	} {
		if _, err := continueSkill(r); err == nil {
			t.Errorf("%s/%s has nothing to continue", r.Type, r.Phase)
		}
	}
}

// runOrcaContinue runs `bts orca continue` in root with fresh flags and
// returns what it printed, its error included.
func runOrcaContinue(t *testing.T, root string, args ...string) string {
	t.Helper()
	t.Chdir(root)
	orcaContinueCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	rootCmd.SetArgs(append([]string{"orca", "continue"}, args...))
	runErr := rootCmd.Execute()
	_ = w.Close()
	os.Stdout = orig
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, rerr := r.Read(buf)
		sb.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	if runErr != nil {
		sb.WriteString(runErr.Error())
	}
	return sb.String()
}

// fakeOrca answers status and launch, and records each command run.
func fakeOrca(t *testing.T) *[][]string {
	t.Helper()
	var calls [][]string
	t.Cleanup(orca.SetRunner(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls = append(calls, args)
		switch args[0] {
		case "status":
			return []byte(`{"ok":true,"result":{"app":{"running":true},"runtime":{"reachable":true,"appVersion":"1.4.262"}}}`), nil
		case "agent":
			return []byte(`{"ok":true,"result":{"outcome":{"kind":"terminal","handle":"term_7","paneKey":"pk:7"},"worktreeId":"w","operationId":"op-7"}}`), nil
		}
		return []byte(`{"ok":false,"error":{"code":"unknown","message":"unexpected"}}`), nil
	}))
	return &calls
}

func launched(calls [][]string) []string {
	for _, c := range calls {
		if c[0] == "agent" {
			return c
		}
	}
	return nil
}

// orca.launch ask (the default) shows the launch and starts nothing until
// --yes; --yes launches, tells the new session to claim the recipe, and
// records the launch in the changelog.
func TestOrcaContinue_AskThenYes(t *testing.T) {
	id := "r-oc1"
	root := editGoFixture(t, id, "build", "finalize", "")
	calls := fakeOrca(t)

	out := runOrcaContinue(t, root, id)
	if launched(*calls) != nil {
		t.Fatalf("ask must not launch without --yes, calls %q", *calls)
	}
	if !strings.Contains(out, "/bts-implement "+id) || !strings.Contains(out, "bts orca continue "+id+" --yes") {
		t.Errorf("ask must show the skill and the --yes command, got:\n%s", out)
	}

	out = runOrcaContinue(t, root, id, "--yes")
	l := launched(*calls)
	if l == nil {
		t.Fatalf("--yes must launch, got:\n%s", out)
	}
	prompt := l[7]
	if !strings.HasPrefix(prompt, "Run `bts recipe claim "+id+"` first, then run /bts-implement "+id) {
		t.Errorf("the prompt must claim first and name the skill, got %q", prompt)
	}
	data, err := os.ReadFile(filepath.Join(state.RecipeDir(root, id), "changelog.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"action":"continue"`) || !strings.Contains(string(data), "term_7") {
		t.Errorf("the launch must be in the changelog, got:\n%s", data)
	}
}

func TestOrcaContinue_AutoOffAndDryRun(t *testing.T) {
	calls := fakeOrca(t)
	auto := editGoFixture(t, "r-oc2", "blueprint", "draft", "orca:\n  launch: auto\n")
	if out := runOrcaContinue(t, auto, "r-oc2"); launched(*calls) == nil {
		t.Fatalf("auto must launch without --yes, got:\n%s", out)
	} else if l := launched(*calls); !strings.Contains(l[7], "/bts-recipe-blueprint r-oc2") {
		t.Errorf("a draft blueprint resumes its own skill, prompt %q", l[7])
	}

	*calls = nil
	off := editGoFixture(t, "r-oc3", "build", "finalize", "orca:\n  launch: off\n")
	if out := runOrcaContinue(t, off, "r-oc3", "--yes"); !strings.Contains(out, "orca.launch is off") || len(*calls) != 0 {
		t.Errorf("off must refuse before calling Orca, got %q, calls %q", out, *calls)
	}

	dry := editGoFixture(t, "r-oc4", "build", "finalize", "orca:\n  launch: auto\n")
	if out := runOrcaContinue(t, dry, "r-oc4", "--dry-run", "--skill", "/bts-status"); !strings.Contains(out, "/bts-status r-oc4") ||
		!strings.Contains(out, "nothing launched") || len(*calls) != 0 {
		t.Errorf("--dry-run must launch nothing and honour --skill, got %q, calls %q", out, *calls)
	}
}
