package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

// The build recipe's evidence commands. Each runs a command itself and
// records the exit code with the code hash it ran on, so the stop hook
// judges runs rather than an agent's reading of them.
//
//   bts check build <id> --cmd "<typecheck>"              skeleton compiles
//   bts falsifier run <id> --inv INV-NNN --cmd "<test>"   fails on the skeleton
//                                                         (passes, for a [carried] invariant)
//   bts falsifier check <id> [--red]                      reruns every falsifier

func init() {
	checkCmd.AddCommand(checkBuildCmd)
	checkBuildCmd.Flags().String("cmd", "", "Typecheck or build command, run via sh -c at the project root (required)")
	_ = checkBuildCmd.MarkFlagRequired("cmd")
	checkBuildCmd.Flags().Int("timeout", 600, "Seconds before the run is killed")

	rootCmd.AddCommand(falsifierCmd)
	falsifierCmd.AddCommand(falsifierRunCmd, falsifierCheckCmd)
	falsifierRunCmd.Flags().String("inv", "", "Invariant id from domain.md § 2, e.g. INV-003, without a [carried] tag (required)")
	_ = falsifierRunCmd.MarkFlagRequired("inv")
	falsifierRunCmd.Flags().String("cmd", "", "Command that runs this invariant's test only (required)")
	_ = falsifierRunCmd.MarkFlagRequired("cmd")
	falsifierRunCmd.Flags().Int("timeout", 600, "Seconds before the run is killed")
	falsifierCheckCmd.Flags().Bool("red", false, "Expect every falsifier to fail (the skeleton), instead of to pass; a [carried] invariant's is still expected to pass")
	falsifierCheckCmd.Flags().Int("timeout", 600, "Seconds before each run is killed")
}

var checkBuildCmd = &cobra.Command{
	Use:   "build <recipe-id>",
	Short: "Run the skeleton's typecheck or build and record the result (build recipe)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		cmdStr, _ := cmd.Flags().GetString("cmd")
		timeout, _ := cmd.Flags().GetInt("timeout")
		run, err := executeBuildCheck(root, args[0], cmdStr, timeout, os.Stdout)
		if err != nil {
			return err
		}
		fmt.Printf("\n[bts] skeleton build recorded: exit=%d → build-evidence.json\n", run.ExitCode)
		if !run.Passed() {
			os.Exit(1)
		}
		return nil
	},
}

var falsifierCmd = &cobra.Command{
	Use:     "falsifier",
	Short:   "Record and rerun the tests that would prove each invariant false (build recipe)",
	GroupID: "tools",
}

var falsifierRunCmd = &cobra.Command{
	Use:   "run <recipe-id>",
	Short: "Run one invariant's test on the skeleton; it must fail (pass, if carried), and the run is recorded",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		inv, _ := cmd.Flags().GetString("inv")
		cmdStr, _ := cmd.Flags().GetString("cmd")
		timeout, _ := cmd.Flags().GetInt("timeout")
		run, err := executeFalsifierRed(root, args[0], inv, cmdStr, timeout, os.Stdout)
		if err != nil {
			return err
		}
		if run.Passed() {
			fmt.Printf("\n[bts] %s (carried) green on the current code → build-evidence.json\n", inv)
			return nil
		}
		fmt.Printf("\n[bts] %s red on the skeleton (exit=%d) → build-evidence.json\n", inv, run.ExitCode)
		return nil
	},
}

var falsifierCheckCmd = &cobra.Command{
	Use:   "check <recipe-id>",
	Short: "Rerun every recorded falsifier: expect green (or red with --red) and record the runs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		red, _ := cmd.Flags().GetBool("red")
		timeout, _ := cmd.Flags().GetInt("timeout")
		results, err := executeFalsifierCheck(root, args[0], red, timeout, os.Stdout)
		if err != nil {
			return err
		}
		want := "green"
		if red {
			want = "red"
		}
		carried := carriedInvariants(root, args[0])
		bad := 0
		fmt.Printf("\n[bts] falsifiers, expecting %s:\n", want)
		for _, id := range sortedKeys(results) {
			r := results[id]
			mark := "ok"
			if !falsifierRunAsExpected(r, carried[id], red) {
				mark = "WRONG"
				bad++
			}
			if carried[id] && red {
				mark += " (carried, expects green)"
			}
			fmt.Printf("  %-10s exit=%-3d %s\n", id, r.ExitCode, mark)
		}
		if bad > 0 {
			fmt.Printf("[bts] %d of %d not %s\n", bad, len(results), want)
			os.Exit(1)
		}
		return nil
	},
}

// runRecorded runs cmdStr at the project root, keeps its output in
// .bts/local/recipes/<id>/<logName>, and returns the run stamped with the
// code hash at the time.
func runRecorded(root, recipeID, cmdStr string, timeoutSec int, logName string, sink io.Writer) (*state.CommandRun, error) {
	if cmdStr == "" {
		return nil, errors.New("--cmd is required")
	}
	before, _ := engine.CodeHash(root, recipeID)
	run, err := runShellCommand(root, cmdStr, timeoutSec, sink)
	if err != nil {
		return nil, fmt.Errorf("run %q: %w", cmdStr, err)
	}
	logDir := filepath.Join(state.LocalPath(root), "recipes", recipeID)
	if err := os.MkdirAll(logDir, 0755); err == nil {
		_ = os.WriteFile(filepath.Join(logDir, logName), run.Output, 0644)
	}
	hash, herr := engine.CodeHash(root, recipeID)
	if herr != nil {
		fmt.Fprintf(os.Stderr, "[bts] warning: code fingerprint: %v (this run records none)\n", herr)
	}
	warnIfRunMovedFingerprint(before, hash)
	return &state.CommandRun{
		Command:  cmdStr,
		ExitCode: run.ExitCode,
		TimedOut: run.TimedOut,
		CodeHash: hash,
		RunAt:    time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// warnIfRunMovedFingerprint warns when a recorded command changed the code
// fingerprint itself. The run is stamped with the code as it left it, but
// a command that writes into the fingerprinted files moves the fingerprint
// on every run, so each recorded run makes the others stale.
func warnIfRunMovedFingerprint(before, after string) {
	if before != "" && after != "" && before != after {
		fmt.Fprintf(os.Stderr, "[bts] warning: this command changed files the code fingerprint covers. "+
			"Keep what it writes under .verify/ or a gitignored path, or every other run compared with it reads as stale.\n")
	}
}

func executeBuildCheck(root, recipeID, cmdStr string, timeoutSec int, sink io.Writer) (*state.CommandRun, error) {
	if _, err := state.LoadRecipeState(root, recipeID); err != nil {
		return nil, fmt.Errorf("load recipe %s: %w", recipeID, err)
	}
	run, err := runRecorded(root, recipeID, cmdStr, timeoutSec, "skeleton-build.log", sink)
	if err != nil {
		return nil, err
	}
	ev, err := state.LoadBuildEvidence(root, recipeID)
	if err != nil {
		return nil, fmt.Errorf("load build evidence: %w", err)
	}
	ev.Skeleton = run
	if err := state.SaveBuildEvidence(root, recipeID, ev); err != nil {
		return nil, fmt.Errorf("save build evidence: %w", err)
	}
	appendEvidenceChangelog(root, recipeID, "skeleton", fmt.Sprintf("build exit=%d", run.ExitCode), run.CodeHash)
	return run, nil
}

// errFalsifierPassed is returned, and nothing recorded, when a falsifier
// passes on the skeleton: a test that cannot fail before the code exists
// does not test the invariant. The message names the way out for an
// invariant the code already keeps, the case that hit this wall in every
// change recipe tried (trial problem #41).
var errFalsifierPassed = errors.New("the test passed on the skeleton, so it does not test the invariant yet; make it assert the invariant's outcome. " +
	"If the invariant is one earlier work established and this change keeps, mark it `[carried]` in its domain.md § 2 ID cell; its test is then recorded passing")

// errCarriedFailed is returned, and nothing recorded, when a carried
// invariant's test fails: a carried invariant is one the code already
// keeps, so its test passes before the change and must keep passing.
var errCarriedFailed = errors.New("the invariant is carried, so its test must pass on the code as it is now; it failed. " +
	"Point --cmd at the existing test that holds it, or restore the behaviour the skeleton broke")

// executeFalsifierRed records an invariant's falsifier. For an invariant
// the recipe introduces the run must fail, and is kept as the red run. For
// one domain.md marks `[carried]` it must pass, and is kept as the green
// run: the code already keeps that invariant, so a red-first rule could
// only refuse its test (trial problem #41).
func executeFalsifierRed(root, recipeID, inv, cmdStr string, timeoutSec int, sink io.Writer) (*state.CommandRun, error) {
	if _, err := state.LoadRecipeState(root, recipeID); err != nil {
		return nil, fmt.Errorf("load recipe %s: %w", recipeID, err)
	}
	introduced, carried, err := engine.DomainInvariantKinds(filepath.Join(state.RecipeDir(root, recipeID), "domain.md"))
	if err != nil {
		return nil, fmt.Errorf("read domain.md invariants: %w", err)
	}
	isCarried := containsString(carried, inv)
	if !isCarried && !containsString(introduced, inv) {
		return nil, fmt.Errorf("%s is not an invariant in domain.md § 2 (declared: %v)", inv, append(introduced, carried...))
	}
	run, err := runRecorded(root, recipeID, cmdStr, timeoutSec, "falsifier-"+inv+".log", sink)
	if err != nil {
		return nil, err
	}
	switch {
	case isCarried && !run.Passed():
		return nil, fmt.Errorf("%s (exit %d): %w", inv, run.ExitCode, errCarriedFailed)
	case !isCarried && run.Passed():
		return nil, fmt.Errorf("%s: %w", inv, errFalsifierPassed)
	}
	ev, err := state.LoadBuildEvidence(root, recipeID)
	if err != nil {
		return nil, fmt.Errorf("load build evidence: %w", err)
	}
	result := fmt.Sprintf("%s red exit=%d", inv, run.ExitCode)
	if isCarried {
		ev.Falsifiers[inv] = &state.Falsifier{Command: cmdStr, Green: run}
		result = fmt.Sprintf("%s carried green exit=%d", inv, run.ExitCode)
	} else {
		ev.Falsifiers[inv] = &state.Falsifier{Command: cmdStr, Red: run}
	}
	if err := state.SaveBuildEvidence(root, recipeID, ev); err != nil {
		return nil, fmt.Errorf("save build evidence: %w", err)
	}
	appendEvidenceChangelog(root, recipeID, "falsify", result, run.CodeHash)
	return run, nil
}

func executeFalsifierCheck(root, recipeID string, red bool, timeoutSec int, sink io.Writer) (map[string]*state.CommandRun, error) {
	if _, err := state.LoadRecipeState(root, recipeID); err != nil {
		return nil, fmt.Errorf("load recipe %s: %w", recipeID, err)
	}
	ev, err := state.LoadBuildEvidence(root, recipeID)
	if err != nil {
		return nil, fmt.Errorf("load build evidence: %w", err)
	}
	if len(ev.Falsifiers) == 0 {
		return nil, fmt.Errorf("no falsifiers recorded; record each with `bts falsifier run %s --inv <id> --cmd \"<test>\"`", recipeID)
	}
	// A carried invariant's test is expected to pass on the skeleton too,
	// so under --red its rerun is still its green run (trial problem #41).
	carried := carriedInvariants(root, recipeID)
	results := map[string]*state.CommandRun{}
	passed := 0
	for _, id := range engine.FalsifierIDs(ev) {
		f := ev.Falsifiers[id]
		fmt.Fprintf(sink, "\n[bts] %s: %s\n", id, f.Command)
		run, err := runRecorded(root, recipeID, f.Command, timeoutSec, "falsifier-"+id+".log", sink)
		if err != nil {
			return nil, err
		}
		if red && !carried[id] {
			f.Red = run
		} else {
			f.Green = run
		}
		results[id] = run
		if run.Passed() {
			passed++
		}
	}
	if err := state.SaveBuildEvidence(root, recipeID, ev); err != nil {
		return nil, fmt.Errorf("save build evidence: %w", err)
	}
	want := "green"
	if red {
		want = "red"
	}
	result := fmt.Sprintf("expect %s: %d of %d passed", want, passed, len(results))
	if n := countCarried(results, carried); red && n > 0 {
		result += fmt.Sprintf(" (%d carried, expected green)", n)
	}
	hash, _ := engine.CodeHash(root, recipeID)
	appendEvidenceChangelog(root, recipeID, "falsifier-check", result, hash)
	return results, nil
}

// carriedInvariants is the set of ids domain.md § 2 marks `[carried]`.
// An unreadable domain.md gives the empty set, so every falsifier is
// judged as an introduced one, as before carried rows existed.
func carriedInvariants(root, recipeID string) map[string]bool {
	set := map[string]bool{}
	_, carried, err := engine.DomainInvariantKinds(filepath.Join(state.RecipeDir(root, recipeID), "domain.md"))
	if err != nil {
		return set
	}
	for _, id := range carried {
		set[id] = true
	}
	return set
}

func countCarried(results map[string]*state.CommandRun, carried map[string]bool) int {
	n := 0
	for id := range results {
		if carried[id] {
			n++
		}
	}
	return n
}

// falsifierRunAsExpected reports whether a rerun came out as its invariant
// expects: an introduced invariant's test fails on the skeleton (--red) and
// passes on the implementation; a carried one's passes on both.
func falsifierRunAsExpected(run *state.CommandRun, carried, red bool) bool {
	if carried {
		return run.Passed()
	}
	return run.Passed() != red
}

func appendEvidenceChangelog(root, recipeID, action, result, hash string) {
	if err := state.AppendChangelog(root, recipeID, &state.ChangelogEntry{
		Action: action, Result: result, ImplHash: hash,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: append changelog: %v\n", err)
	}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]*state.CommandRun) []string {
	ev := &state.BuildEvidence{Falsifiers: map[string]*state.Falsifier{}}
	for k := range m {
		ev.Falsifiers[k] = nil
	}
	return engine.FalsifierIDs(ev)
}
