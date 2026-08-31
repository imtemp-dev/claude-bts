package cli

import (
	"github.com/spf13/pflag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imtemp-dev/claude-bts/internal/state"
)

// runRecipeLog executes `bts recipe log` against root with the given args,
// capturing stderr so the budget-drift notice can be asserted. The command
// resolves the project from the working directory, so the test chdirs.
func runRecipeLog(t *testing.T, root string, args ...string) string {
	t.Helper()
	t.Chdir(root)

	// Every test in this package drives the same rootCmd in one process,
	// so flag state survives between invocations where a fresh process
	// would start clean. Two ways that bites: pflag's StringSlice
	// APPENDS once set, so `--dimension verify` in one test arrived as
	// verify+audit+simulate in the next; and `Changed` stays true, so a
	// later call looked like it had passed --action when it had not.
	// Reset every flag to its default, the way a new process does.
	recipeLogCmd.Flags().VisitAll(func(f *pflag.Flag) {
		if sv, ok := f.Value.(interface{ Replace([]string) error }); ok {
			_ = sv.Replace(nil)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
		f.Changed = false
	})

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origErr := os.Stderr
	os.Stderr = w

	rootCmd.SetArgs(append([]string{"recipe", "log"}, args...))
	runErr := rootCmd.Execute()

	_ = w.Close()
	os.Stderr = origErr
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, rerr := r.Read(buf)
		sb.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	_ = r.Close()

	if runErr != nil {
		// A CONVERGENCE FAILED exit is a legitimate outcome for some
		// callers; surface it as stderr text rather than failing here.
		sb.WriteString(runErr.Error())
	}
	return sb.String()
}

// The round being logged must record the budget it was judged under.
// Without it the log cannot say which regime produced a given Status.
func TestRecipeLog_StampsBudget(t *testing.T) {
	root := newRecipeFixture(t, "r-b01", "draft", 0, 0, nil)
	writeProjectFile(t, root, ".bts/config/settings.yaml", "verify:\n  max_iterations: 4\n")

	runRecipeLog(t, root, "r-b01", "--iteration", "1", "--critical", "1", "--doc", "draft.md")

	entries, err := state.ReadVerifyLog(root, "r-b01")
	if err != nil {
		t.Fatalf("read verify-log: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Budget != 4 {
		t.Errorf("budget: got %d, want 4 (settings.yaml verify.max_iterations)", entries[0].Budget)
	}
}

// With no settings.yaml the default budget still gets recorded, so a
// default-configured project's log is self-describing too.
func TestRecipeLog_StampsDefaultBudget(t *testing.T) {
	root := newRecipeFixture(t, "r-b02", "draft", 0, 0, nil)

	runRecipeLog(t, root, "r-b02", "--iteration", "1", "--critical", "1", "--doc", "draft.md")

	entries, err := state.ReadVerifyLog(root, "r-b02")
	if err != nil {
		t.Fatalf("read verify-log: %v", err)
	}
	if len(entries) != 1 || entries[0].Budget != 3 {
		t.Fatalf("got budget %v, want the built-in default 3", entries)
	}
}

// Changing the budget re-judges the document's whole history on the next
// evaluation. That must be announced, not silent.
func TestRecipeLog_AnnouncesBudgetDrift(t *testing.T) {
	root := newRecipeFixture(t, "r-b03", "draft", 0, 1, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 1, Doc: "draft.md", Status: "continue", Budget: 3},
	})
	writeProjectFile(t, root, ".bts/config/settings.yaml", "verify:\n  max_iterations: 6\n")

	out := runRecipeLog(t, root, "r-b03", "--iteration", "2", "--critical", "1", "--doc", "draft.md")

	if !strings.Contains(out, "max_iterations changed 3 → 6") {
		t.Errorf("expected a drift notice naming both budgets, got:\n%s", out)
	}
	if !strings.Contains(out, "draft.md") {
		t.Errorf("drift notice must name the document, got:\n%s", out)
	}
}

// An unchanged budget must stay quiet — the notice is for regime changes,
// not for every round.
func TestRecipeLog_NoDriftNoticeWhenBudgetUnchanged(t *testing.T) {
	root := newRecipeFixture(t, "r-b04", "draft", 0, 1, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 1, Doc: "draft.md", Status: "continue", Budget: 3},
	})

	out := runRecipeLog(t, root, "r-b04", "--iteration", "2", "--critical", "1", "--doc", "draft.md")

	if strings.Contains(out, "max_iterations changed") {
		t.Errorf("unchanged budget must not announce drift, got:\n%s", out)
	}
}

// A pre-budget log (legacy entries, no budget field) must not be reported
// as drift — there is no recorded regime to have changed.
func TestRecipeLog_LegacyLogIsNotDrift(t *testing.T) {
	root := newRecipeFixture(t, "r-b05", "draft", 0, 1, []state.VerifyLogEntry{
		{Iteration: 1, Critical: 1, Doc: "draft.md", Status: "continue"},
	})
	writeProjectFile(t, root, ".bts/config/settings.yaml", "verify:\n  max_iterations: 6\n")

	out := runRecipeLog(t, root, "r-b05", "--iteration", "2", "--critical", "1", "--doc", "draft.md")

	if strings.Contains(out, "max_iterations changed") {
		t.Errorf("legacy log has no recorded budget, so nothing drifted; got:\n%s", out)
	}
}

// The settings template must not advertise gate knobs that no code reads.
// The completion gate is fixed in the stop hook.
func TestSettingsTemplate_HasNoInertConvergenceKnobs(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "template", "templates", ".bts", "config", "settings.yaml"))
	if err != nil {
		t.Fatalf("read settings template: %v", err)
	}
	for _, knob := range []string{"require_zero_critical:", "require_zero_major:", "allow_minor:"} {
		if strings.Contains(string(data), knob) {
			t.Errorf("settings.yaml still advertises %q, but nothing reads it — the gate is hardcoded in internal/hook/stop.go", knob)
		}
	}
}

// Two rules write Status "failed" and their remedies are opposites, so
// the round has to say which one fired. Here every round improves — the
// progress budget is never touched — and the cap fires on round count
// alone. Without FailedBy the stop hook read this as an exhausted
// verify.max_iterations and told the agent to hold a decision.
func TestRecipeLog_StampsRoundCapAsTheCause(t *testing.T) {
	var prior []state.VerifyLogEntry
	for i := 0; i < 5; i++ {
		prior = append(prior, state.VerifyLogEntry{
			Iteration: i + 1, Critical: 0, Major: 10 - i, Doc: "draft.md",
			Dimensions: []string{"audit", "simulate", "verify"}, FullPass: true,
			Status: "continue", Budget: 3, RoundCap: 6,
		})
	}
	root := newRecipeFixture(t, "r-b06", "draft", 0, 5, prior)
	writeProjectFile(t, root, ".bts/config/settings.yaml",
		"verify:\n  max_iterations: 3\n  max_rounds: 6\n")

	runRecipeLog(t, root, "r-b06", "--iteration", "6", "--critical", "0",
		"--major", "5", "--doc", "draft.md", "--scope", "full",
		"--dimension", "verify,audit,simulate")

	entries, err := state.ReadVerifyLog(root, "r-b06")
	if err != nil {
		t.Fatalf("read verify-log: %v", err)
	}
	last := entries[len(entries)-1]
	if last.Status != "failed" {
		t.Fatalf("status = %q, want failed — six rounds against max_rounds=6", last.Status)
	}
	if last.FailedBy != state.FailedByRoundCap {
		t.Errorf("failed_by = %q, want %q — no round ever stagnated",
			last.FailedBy, state.FailedByRoundCap)
	}
	if last.RoundCap != 6 {
		t.Errorf("round_cap = %d, want 6", last.RoundCap)
	}
}

// The blueprint skill says "All three instruments, every round" in bold,
// and the first recipe to run under that rule spent three of six rounds
// on one instrument each — a pattern that can never satisfy completion
// and costs the same budget as a full round. Prose the model is expected
// to honour is the failure this project keeps rediscovering, so the
// accounting is printed where the cost is paid.
func TestRecipeLog_PartialDimensionRoundReportsTheCost(t *testing.T) {
	root := newRecipeFixture(t, "r-b07", "draft", 0, 0, nil)
	writeProjectFile(t, root, ".bts/config/settings.yaml",
		"verify:\n  max_iterations: 3\n  max_rounds: 6\n")

	out := runRecipeLog(t, root, "r-b07", "--iteration", "1", "--critical", "1",
		"--doc", "draft.md", "--scope", "full", "--dimension", "verify")

	if !strings.Contains(out, "1 of 3 dimensions") {
		t.Errorf("the note must name how much of the measurement ran, got: %s", out)
	}
	if !strings.Contains(out, "0 of them qualifying") {
		t.Errorf("the note must say how many rounds so far can count, got: %s", out)
	}

	// A round that ran everything is not nagged.
	quiet := runRecipeLog(t, root, "r-b07", "--iteration", "2", "--critical", "1",
		"--doc", "draft.md", "--scope", "full",
		"--dimension", "verify,audit,simulate")
	if strings.Contains(quiet, "of 3 dimensions") {
		t.Errorf("a complete round must not be nagged, got: %s", quiet)
	}
}

// bts-recipe-fix prescribed `--phase verify --action verify --result
// "critical=N, major=N"` directly under the sentence "Enforced in code,
// not by self-counting". Those flags select the changelog and
// phase-change modes, so the round was never written: verify-log
// unchanged, findings ledger untouched, exit 0. A fix recipe therefore
// had no convergence budget at all.
func TestVerifyCountsRefuseChangelogAndPhaseModes(t *testing.T) {
	for _, mode := range [][]string{
		{"--action", "verify"},
		{"--phase", "verify"},
		{"--action", "verify", "--phase", "verify"},
	} {
		root := newRecipeFixture(t, "r-f01", "draft", 0, 0, nil)
		args := append([]string{"r-f01"}, mode...)
		args = append(args, "--critical", "9", "--major", "99")
		out := runRecipeLog(t, root, args...)
		if !strings.Contains(out, "cannot be combined with --action/--phase") {
			t.Fatalf("%v: want a refusal, got: %s", mode, out)
		}
		if entries, err := state.ReadVerifyLog(root, "r-f01"); err == nil && len(entries) > 0 {
			t.Fatalf("%v: the round must not be recorded when the call is refused (%d entries)", mode, len(entries))
		}
	}
}

// The same flags without --action/--phase are the supported form.
func TestVerifyCountsRecordedWithoutModeFlags(t *testing.T) {
	root := newRecipeFixture(t, "r-f02", "draft", 0, 0, nil)

	out := runRecipeLog(t, root, "r-f02", "--doc", "draft.md", "--dimension", "verify",
		"--scope", "full", "--critical", "9", "--major", "99")
	if strings.Contains(out, "cannot be combined") {
		t.Fatalf("the supported form must not be refused: %s", out)
	}
	entries, err := state.ReadVerifyLog(root, "r-f02")
	if err != nil || len(entries) != 1 {
		t.Fatalf("verify-log not written: %v (%d entries)", err, len(entries))
	}
	if entries[0].Critical != 9 {
		t.Fatalf("counts not recorded: %+v", entries[0])
	}
}
