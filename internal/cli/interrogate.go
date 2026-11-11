package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

// bts interrogate: P3 of the prove stage, after pstack's interrogate.
// Adversaries, each in a private copy of the project, try to break the
// change; a finding counts only when bts reruns its command in a fresh
// copy of the current code and the command fails again.
//
//   bts interrogate prepare <id> [--adversaries 2]   copies + seats
//   bts interrogate prepare <id> --rescope           one seat on what changed since the last round
//   bts interrogate verify <id>                      rerun every finding the current round reported
//   bts interrogate verify <id> --recheck            rerun open and fixed findings after fixes
//   bts interrogate waive <id> --finding F-001 --reason "..."
//   bts interrogate pin <id> --finding F-001 --cmd "<test>" --revert <patch> --expect "<test id>"
//                                                    a fix that refuses it another way, pinned by a test

// adversaryAngles seed independence: one model family, separate contexts,
// different angles of attack.
var adversaryAngles = []string{
	"invariants and state: order, idempotence, retries, an effect done twice or not at all",
	"inputs and boundaries: malformed or hostile input, auth, errors and odd output from external tools",
	"operations: restart, a crash mid-step, configuration and environment",
}

// rescopeAngle is the first seat of a rescoped round: it attacks what
// changed since the round before, which that round could not have seen.
const rescopeAngle = "the change since the last interrogate round (the scope lists its files): what it broke, or let through, that the earlier round could not have seen"

// expressAngle is the one seat of an express recipe's round (feature
// #132): what the change promises, attacked from the angles two or three
// seats split between them.
const expressAngle = "the change's scope: each In Scope item of scope.md and the invariants that hold it, broken by order, retries, malformed or hostile input, errors from external tools, or a restart mid-step"

func init() {
	rootCmd.AddCommand(interrogateCmd)
	interrogateCmd.AddCommand(interrogatePrepareCmd, interrogateVerifyCmd, interrogateWaiveCmd)
	interrogatePrepareCmd.Flags().Int("adversaries", 2, "Adversary seats: 2, or 3 for a contested or security-sensitive change; an express recipe takes 1 by default, up to 3 (with --rescope: 1 by default, up to 3)")
	interrogatePrepareCmd.Flags().Bool("rescope", false, "After a large change to a verified run: a new round on the files changed since the last one")
	interrogateVerifyCmd.Flags().Bool("recheck", false, "After fixes: rerun open and fixed findings on the current code")
	interrogateVerifyCmd.Flags().Int("timeout", 600, "Seconds before each run is killed")
	interrogateWaiveCmd.Flags().String("finding", "", "Finding id, e.g. F-002 (required)")
	interrogateWaiveCmd.Flags().String("reason", "", "Why it is left unfixed, as the user agreed (required)")
	_ = interrogateWaiveCmd.MarkFlagRequired("finding")
	_ = interrogateWaiveCmd.MarkFlagRequired("reason")
	interrogateCmd.AddCommand(interrogatePinCmd)
	interrogatePinCmd.Flags().String("finding", "", "Finding id, e.g. F-003 (required)")
	interrogatePinCmd.Flags().String("cmd", "", "Command that runs the regression test only (required)")
	interrogatePinCmd.Flags().String("revert", "", "Patch that reverts the fix in the code, never a test (required)")
	interrogatePinCmd.Flags().String("expect", "", "The regression test's id, which its failing run must print (required)")
	interrogatePinCmd.Flags().String("setup", "", "Command run in the copy before the test, e.g. bringing in an ignored file")
	interrogatePinCmd.Flags().Int("timeout", 600, "Seconds before each run is killed")
	for _, f := range []string{"finding", "cmd", "revert", "expect"} {
		_ = interrogatePinCmd.MarkFlagRequired(f)
	}
}

var interrogateCmd = &cobra.Command{
	Use:     "interrogate",
	Short:   "Adversaries try to break the change; only findings a rerun proves survive (prove stage)",
	GroupID: "tools",
}

var interrogatePrepareCmd = &cobra.Command{
	Use:          "prepare <recipe-id>",
	Short:        "Make a private copy of the project for each adversary and record the seats",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		n, _ := cmd.Flags().GetInt("adversaries")
		rescope, _ := cmd.Flags().GetBool("rescope")
		var run *state.InterrogateRun
		if rescope {
			if !cmd.Flags().Changed("adversaries") {
				n = 1
			}
			run, err = executeInterrogateRescope(root, args[0], n)
			if err != nil {
				return err
			}
			fmt.Printf("[bts] interrogate round %d prepared for %s. Scope (changed since the last round):\n", run.CurrentRound(), args[0])
			for _, f := range run.Scope {
				fmt.Printf("  %s\n", f)
			}
		} else {
			if !cmd.Flags().Changed("adversaries") {
				n = 0 // the recipe's own default: 1 for express, else 2
			}
			run, err = executeInterrogatePrepare(root, args[0], n)
			if err != nil {
				return err
			}
			fmt.Printf("[bts] interrogate prepared for %s. Scope (the recipe's code files; tests marked):\n", args[0])
			for _, f := range engine.CodeFiles(root, args[0]) {
				if isTestPath(f) {
					fmt.Printf("  %s (test)\n", f)
				} else {
					fmt.Printf("  %s\n", f)
				}
			}
		}
		fmt.Println("[bts] seats:")
		for _, a := range run.Adversaries {
			if !a.InRound(run.CurrentRound()) {
				continue
			}
			fmt.Printf("  %s\n    angle:  %s\n    copy:   %s\n    report: %s\n", a.Name, a.Angle, a.Copy,
				filepath.Join(state.RecipeDir(root, args[0]), a.Report))
		}
		return nil
	},
}

var interrogateVerifyCmd = &cobra.Command{
	Use:          "verify <recipe-id>",
	Short:        "Rerun each reported finding in a fresh copy of the current code; keep only those that fail again",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		recheck, _ := cmd.Flags().GetBool("recheck")
		timeout, _ := cmd.Flags().GetInt("timeout")
		var run *state.InterrogateRun
		if recheck {
			run, err = executeInterrogateRecheck(root, args[0], timeout, os.Stdout)
		} else {
			run, err = executeInterrogateVerify(root, args[0], timeout, os.Stdout)
		}
		if err != nil {
			return err
		}
		printInterrogateFindings(run)
		hash, _ := engine.CodeHash(root, args[0])
		if reason := engine.CheckInterrogateClean(run, hash, args[0]); reason != "" {
			fmt.Printf("[bts] %s\n", reason)
		} else if change, ok, derr := engine.InterrogateDrift(root, args[0], run); derr == nil && ok {
			if reason := engine.CheckInterrogateRescope(change, args[0]); reason != "" {
				fmt.Printf("[bts] %s\n", reason)
			}
		}
		return nil
	},
}

var interrogateWaiveCmd = &cobra.Command{
	Use:          "waive <recipe-id>",
	Short:        "Leave a reproduced finding unfixed, with the reason the user agreed to",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("finding")
		reason, _ := cmd.Flags().GetString("reason")
		if err := executeInterrogateWaive(root, args[0], id, reason); err != nil {
			return err
		}
		fmt.Printf("[bts] %s waived → interrogate.json\n", id)
		return nil
	},
}

var interrogatePinCmd = &cobra.Command{
	Use:          "pin <recipe-id>",
	Short:        "Close a reproduced finding whose fix refuses it another way: a regression test passes now and fails with the fix reverted",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		var o pinOptions
		o.Finding, _ = cmd.Flags().GetString("finding")
		o.Command, _ = cmd.Flags().GetString("cmd")
		o.RevertFile, _ = cmd.Flags().GetString("revert")
		o.Expect, _ = cmd.Flags().GetString("expect")
		o.Setup, _ = cmd.Flags().GetString("setup")
		o.TimeoutSec, _ = cmd.Flags().GetInt("timeout")
		f, err := executeInterrogatePin(root, args[0], o, os.Stdout)
		if err != nil {
			return err
		}
		fmt.Printf("\n[bts] %s fixed, pinned by %q (fails with the fix reverted) → interrogate.json\n", f.ID, f.Pin.Command)
		return nil
	},
}

type pinOptions struct {
	Finding, Command, RevertFile, Expect, Setup string
	TimeoutSec                                  int
}

// executeInterrogatePin closes a reproduced finding whose fix refuses the
// input another way than the adversary's command asserts, so that command
// keeps failing (trial problem #83).  The regression test must pass on the
// current code and, in a copy with the revert patch applied, fail and print
// its own id: that is what shows it pins this fix, not merely passes.  The
// patch changes code files only, never a test.  A refused pin writes
// nothing; a recheck afterwards reruns the pinned test.
func executeInterrogatePin(root, recipeID string, o pinOptions, sink io.Writer) (*state.InterrogateFinding, error) {
	o.Expect = strings.TrimSpace(o.Expect)
	if strings.TrimSpace(o.Command) == "" || o.RevertFile == "" || o.Expect == "" {
		return nil, errors.New("--cmd, --revert and --expect are required")
	}
	run, err := state.LoadInterrogateRun(root, recipeID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, fmt.Errorf("no interrogate run for %s", recipeID)
	}
	var f *state.InterrogateFinding
	for _, cand := range run.Findings {
		if cand.ID == o.Finding {
			f = cand
		}
	}
	if f == nil {
		return nil, fmt.Errorf("no finding %s in %s's interrogate run", o.Finding, recipeID)
	}
	if f.Status != state.InterrogateReproduced {
		return nil, fmt.Errorf("%s is %s; only a reproduced finding can be pinned", f.ID, f.Status)
	}
	patch, err := os.ReadFile(o.RevertFile)
	if err != nil {
		return nil, fmt.Errorf("read --revert: %w", err)
	}
	files, err := checkMutationPatch(root, recipeID, patch)
	if err != nil {
		return nil, fmt.Errorf("--revert: %w", err)
	}
	cr, err := runRecorded(root, recipeID, o.Command, o.TimeoutSec, "interrogate-pin-"+f.ID+".log", sink)
	if err != nil {
		return nil, err
	}
	if !cr.Passed() {
		return nil, fmt.Errorf("%s: the regression test fails on the current code (exit %d); pin a test that passes with the fix in place", f.ID, cr.ExitCode)
	}
	rel := filepath.ToSlash(filepath.Join("interrogate", "pins", f.ID+".patch"))
	m, err := runMutation(root, recipeID, f.ID, o.Command, patch, rel, files, o.Expect, o.Setup, o.TimeoutSec, sink)
	if err != nil {
		return nil, err
	}
	switch {
	case m.TimedOut:
		return nil, fmt.Errorf("%s: the regression test timed out with the fix reverted; it must fail, not hang", f.ID)
	case m.ExitCode == 0:
		return nil, fmt.Errorf("%s: the regression test still passes with the fix reverted, so it does not pin this fix", f.ID)
	case !m.Matched:
		return nil, fmt.Errorf("%s: the regression test failed with the fix reverted, but %q was not in its output; name the test's own id with --expect, and revert only the fix", f.ID, o.Expect)
	}
	dir := filepath.Join(state.RecipeDir(root, recipeID), "interrogate", "pins")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(state.RecipeDir(root, recipeID), filepath.FromSlash(rel)), patch, 0644); err != nil {
		return nil, fmt.Errorf("store the revert patch: %w", err)
	}
	// Read again after the runs (trial problem #117): what another command
	// recorded meanwhile is kept, and a finding waived or pinned meanwhile
	// is no longer the one this pin was checked against.
	findingID := f.ID
	err = state.UpdateInterrogateRun(root, recipeID, func(cur *state.InterrogateRun) error {
		f = nil
		for _, cand := range cur.Findings {
			if cand.ID == findingID {
				f = cand
			}
		}
		if f == nil || f.Status != state.InterrogateReproduced {
			return fmt.Errorf("%s changed while its pin ran; nothing was recorded", findingID)
		}
		f.Status, f.Recheck = state.InterrogateFixed, cr
		f.Pin = &state.InterrogatePin{Command: o.Command, Expect: o.Expect, Revert: m}
		return nil
	})
	if err != nil {
		return nil, err
	}
	appendEvidenceChangelog(root, recipeID, "interrogate", fmt.Sprintf("pinned %s by %s (fix reverted: exit=%d)", f.ID, o.Command, m.ExitCode), cr.CodeHash)
	return f, nil
}

// adversaryReport is what an adversary writes: findings it proved by a
// run in its own copy.
type adversaryReport struct {
	Adversary string `json:"adversary"`
	Findings  []struct {
		Title      string   `json:"title"`
		Command    string   `json:"command"`
		Expected   string   `json:"expected"`
		Observed   string   `json:"observed"`
		ReproFiles []string `json:"repro_files"`
	} `json:"findings"`
}

// executeInterrogatePrepare seats n adversaries; n 0 is the recipe's
// default, one seat for an express recipe and two for any other.
func executeInterrogatePrepare(root, recipeID string, n int) (*state.InterrogateRun, error) {
	recipe, err := state.LoadRecipeState(root, recipeID)
	if err != nil {
		return nil, fmt.Errorf("load recipe %s: %w", recipeID, err)
	}
	least := 2
	if recipe.IsExpress() {
		least = 1
	}
	if n == 0 {
		n = least
	}
	if n < least || n > len(adversaryAngles) {
		if recipe.IsExpress() {
			return nil, fmt.Errorf("--adversaries must be 1 to 3 for an express recipe, got %d", n)
		}
		return nil, fmt.Errorf("--adversaries must be 2 or 3, got %d; one seat is for an express build recipe", n)
	}
	if existing, err := state.LoadInterrogateRun(root, recipeID); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, fmt.Errorf("%s already has an interrogate run (prepared %s); verify it, recheck it after fixes, "+
			"or after a large change run `bts interrogate prepare %s --rescope`", recipeID, existing.PreparedAt, recipeID)
	}
	run := &state.InterrogateRun{PreparedAt: time.Now().UTC().Format(time.RFC3339), Round: 1}
	angles := adversaryAngles[:n]
	if n == 1 {
		angles = []string{expressAngle}
	}
	if err := seatAdversaries(root, recipeID, run, angles); err != nil {
		return nil, err
	}
	if err := state.SaveInterrogateRun(root, recipeID, run); err != nil {
		return nil, err
	}
	hash, _ := engine.CodeHash(root, recipeID)
	appendEvidenceChangelog(root, recipeID, "interrogate", fmt.Sprintf("prepared %d adversaries", n), hash)
	return run, nil
}

// executeInterrogateRescope opens a new round on a verified run, scoped to
// the files changed since the round before (trial problem #52): one seat
// by default, attacking the change. The run's earlier findings stay, so a
// fix they needed still has to hold on the current code.
func executeInterrogateRescope(root, recipeID string, n int) (*state.InterrogateRun, error) {
	if _, err := state.LoadRecipeState(root, recipeID); err != nil {
		return nil, fmt.Errorf("load recipe %s: %w", recipeID, err)
	}
	if n < 1 || n > len(adversaryAngles) {
		return nil, fmt.Errorf("--adversaries with --rescope must be 1 to 3, got %d", n)
	}
	run, err := state.LoadInterrogateRun(root, recipeID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, fmt.Errorf("no interrogate run to rescope; start with `bts interrogate prepare %s`", recipeID)
	}
	if run.VerifiedAt == "" {
		return nil, fmt.Errorf("round %d is prepared but not verified; run `bts interrogate verify %s` first", run.CurrentRound(), recipeID)
	}
	change, ok, err := engine.InterrogateDrift(root, recipeID, run)
	if err != nil {
		return nil, fmt.Errorf("what changed since round %d: %w", run.CurrentRound(), err)
	}
	if !ok {
		return nil, fmt.Errorf("round %d recorded no copy of the code to compare with, so bts cannot tell what changed; "+
			"interrogate_clean does not ask this run for a rescope", run.CurrentRound())
	}
	scope := change.Files()
	if len(scope) == 0 {
		return nil, fmt.Errorf("no code file changed since round %d's copies were made; no rescope is needed", run.CurrentRound())
	}
	angles := append([]string{rescopeAngle}, adversaryAngles...)[:n]
	run.Round = run.CurrentRound() + 1
	run.Scope = scope
	run.VerifiedAt = ""
	if err := seatAdversaries(root, recipeID, run, angles); err != nil {
		return nil, err
	}
	if err := state.SaveInterrogateRun(root, recipeID, run); err != nil {
		return nil, err
	}
	hash, _ := engine.CodeHash(root, recipeID)
	appendEvidenceChangelog(root, recipeID, "interrogate",
		fmt.Sprintf("rescoped round %d: %d seat(s) on %s", run.Round, n, strings.Join(scope, ", ")), hash)
	return run, nil
}

// seatAdversaries records what the code is now (CodeSeen) and gives each
// angle a seat in run.CurrentRound() with its own copy of the project.
func seatAdversaries(root, recipeID string, run *state.InterrogateRun, angles []string) error {
	if err := os.MkdirAll(filepath.Join(state.RecipeDir(root, recipeID), "interrogate"), 0755); err != nil {
		return err
	}
	seen, err := engine.SeeCode(root, recipeID)
	if err != nil {
		return fmt.Errorf("record the code the adversaries see: %w", err)
	}
	run.Seen = seen
	for _, angle := range angles {
		name := fmt.Sprintf("a%d", len(run.Adversaries)+1)
		dir, err := os.MkdirTemp("", "bts-adversary-"+recipeID+"-"+name+"-")
		if err != nil {
			return err
		}
		if _, err := copyProjectTo(root, dir); err != nil {
			return fmt.Errorf("copy project for %s: %w", name, err)
		}
		run.Adversaries = append(run.Adversaries, &state.Adversary{
			Name: name, Angle: angle, Copy: dir, Round: run.CurrentRound(),
			Report: filepath.ToSlash(filepath.Join("interrogate", name+".json")),
		})
	}
	return nil
}

func executeInterrogateVerify(root, recipeID string, timeoutSec int, sink io.Writer) (*state.InterrogateRun, error) {
	run, err := state.LoadInterrogateRun(root, recipeID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, fmt.Errorf("no interrogate run; start with `bts interrogate prepare %s`", recipeID)
	}
	if run.VerifiedAt != "" {
		return nil, fmt.Errorf("already verified at %s; after fixes run `bts interrogate verify %s --recheck`", run.VerifiedAt, recipeID)
	}
	recipeDir := state.RecipeDir(root, recipeID)
	// The code under test: the recipe's code files without its tests, which
	// tasks.json lists too. A repro may add a case to a test file.
	code := map[string]bool{}
	for _, f := range engine.CodeFiles(root, recipeID) {
		if !isTestPath(f) {
			code[f] = true
		}
	}
	// Only the current round's seats report now; a rescoped round's
	// findings are numbered after the earlier rounds'.
	var seats []*state.Adversary
	var reports []*adversaryReport
	for _, a := range run.Adversaries {
		if !a.InRound(run.CurrentRound()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(recipeDir, filepath.FromSlash(a.Report)))
		if err != nil {
			return nil, fmt.Errorf("adversary %s wrote no report at %s: %w", a.Name, a.Report, err)
		}
		var rep adversaryReport
		if err := json.Unmarshal(data, &rep); err != nil {
			return nil, fmt.Errorf("adversary %s report is not valid JSON: %w", a.Name, err)
		}
		rep.Adversary = a.Name
		seats = append(seats, a)
		reports = append(reports, &rep)
	}
	before := len(run.Findings)
	n := before
	var found []*state.InterrogateFinding
	for i, rep := range reports {
		adv := seats[i]
		for _, r := range rep.Findings {
			n++
			f := &state.InterrogateFinding{
				ID: fmt.Sprintf("F-%03d", n), Adversary: adv.Name, Title: r.Title,
				Command: strings.TrimSpace(r.Command), Expected: r.Expected, Observed: r.Observed,
			}
			found = append(found, f)
			if f.Command == "" {
				f.Status, f.DropReason = state.InterrogateDropped, "no command to rerun"
				continue
			}
			if reason := storeReproFiles(root, recipeID, f, adv.Copy, r.ReproFiles, code); reason != "" {
				f.Status, f.DropReason = state.InterrogateDropped, reason
				continue
			}
			fmt.Fprintf(sink, "\n[bts] %s (%s): %s\n", f.ID, adv.Name, f.Command)
			cr, err := rerunFinding(root, recipeID, f, timeoutSec, sink)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", f.ID, err)
			}
			f.Verify = cr
			switch {
			case cr.TimedOut:
				f.Status, f.DropReason = state.InterrogateDropped, "timed out on a fresh copy"
			case cr.ExitCode == 0:
				f.Status, f.DropReason = state.InterrogateDropped, "passed on a fresh copy of the current code"
			default:
				f.Status = state.InterrogateReproduced
			}
		}
	}
	// The reruns can take minutes, so interrogate.json is read again and a
	// waiver or pin recorded meanwhile is kept (trial problem #117).
	round := run.CurrentRound()
	err = state.UpdateInterrogateRun(root, recipeID, func(cur *state.InterrogateRun) error {
		if cur.VerifiedAt != "" || cur.CurrentRound() != round || len(cur.Findings) != before {
			return fmt.Errorf("round %d was verified or rescoped while this verify ran; nothing was recorded", round)
		}
		cur.Findings = append(cur.Findings, found...)
		cur.VerifiedAt = time.Now().UTC().Format(time.RFC3339)
		run = cur
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, a := range seats {
		_ = os.RemoveAll(a.Copy)
	}
	reproduced, dropped := countFindings(run)
	hash, _ := engine.CodeHash(root, recipeID)
	appendEvidenceChangelog(root, recipeID, "interrogate", fmt.Sprintf("verified: %d reproduced, %d dropped", reproduced, dropped), hash)
	return run, nil
}

func executeInterrogateRecheck(root, recipeID string, timeoutSec int, sink io.Writer) (*state.InterrogateRun, error) {
	run, err := state.LoadInterrogateRun(root, recipeID)
	if err != nil {
		return nil, err
	}
	if run == nil || run.VerifiedAt == "" {
		return nil, fmt.Errorf("no verified interrogate run to recheck; run `bts interrogate verify %s` first", recipeID)
	}
	reruns := map[string]*state.CommandRun{}
	ranPin := map[string]string{} // the pinned test a rerun ran; none for a repro
	for _, f := range run.Findings {
		if f.Status != state.InterrogateReproduced && f.Status != state.InterrogateFixed {
			continue
		}
		var cr *state.CommandRun
		if f.Pin != nil {
			// A pinned finding's own command asserts a refusal the fix
			// replaced; its regression test is what tells (trial problem #83).
			fmt.Fprintf(sink, "\n[bts] recheck %s (pinned): %s\n", f.ID, f.Pin.Command)
			cr, err = runRecorded(root, recipeID, f.Pin.Command, timeoutSec, "interrogate-pin-"+f.ID+".log", io.Discard)
			if err == nil {
				printRunLine(sink, root, f.ID, cr.ExitCode, cr.TimedOut,
					filepath.Join(state.LocalPath(root), "recipes", recipeID, "interrogate-pin-"+f.ID+".log"))
			}
		} else {
			fmt.Fprintf(sink, "\n[bts] recheck %s: %s\n", f.ID, f.Command)
			cr, err = rerunFinding(root, recipeID, f, timeoutSec, sink)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.ID, err)
		}
		reruns[f.ID] = cr
		if f.Pin != nil {
			ranPin[f.ID] = f.Pin.Command
		}
	}
	// The reruns can take minutes, so interrogate.json is read again (trial
	// problem #117): a finding waived or pinned meanwhile keeps what it has.
	fixed, open := 0, 0
	var changed []string
	err = state.UpdateInterrogateRun(root, recipeID, func(cur *state.InterrogateRun) error {
		for _, f := range cur.Findings {
			cr, ok := reruns[f.ID]
			if !ok {
				continue
			}
			pin := ""
			if f.Pin != nil {
				pin = f.Pin.Command
			}
			if (f.Status != state.InterrogateReproduced && f.Status != state.InterrogateFixed) || pin != ranPin[f.ID] {
				changed = append(changed, f.ID)
				continue
			}
			f.Recheck = cr
			if cr.Passed() {
				f.Status = state.InterrogateFixed
				fixed++
			} else {
				f.Status = state.InterrogateReproduced
				open++
			}
		}
		run = cur
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(changed) > 0 {
		fmt.Fprintf(sink, "\n[bts] %s waived or pinned while the recheck ran; the reruns here were not recorded\n", strings.Join(changed, ", "))
	}
	hash, _ := engine.CodeHash(root, recipeID)
	appendEvidenceChangelog(root, recipeID, "interrogate", fmt.Sprintf("recheck: %d fixed, %d open", fixed, open), hash)
	return run, nil
}

func executeInterrogateWaive(root, recipeID, findingID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("--reason is required: why the user agreed to leave it unfixed")
	}
	run, err := state.LoadInterrogateRun(root, recipeID)
	if err != nil {
		return err
	}
	if run == nil {
		return fmt.Errorf("no interrogate run for %s", recipeID)
	}
	for _, f := range run.Findings {
		if f.ID != findingID {
			continue
		}
		// A waived finding's reason can be restated: a later change can make
		// it stale, as the PR review's changes did for true-shark's F-003
		// and F-005 (trial problem #91).  The changelog keeps the old one.
		restated := f.Status == state.InterrogateWaived
		if f.Status != state.InterrogateReproduced && !restated {
			return fmt.Errorf("%s is %s; only a reproduced finding can be waived, or a waived one restated", findingID, f.Status)
		}
		old := f.WaiveReason
		f.Status, f.WaiveReason = state.InterrogateWaived, strings.TrimSpace(reason)
		if err := state.SaveInterrogateRun(root, recipeID, run); err != nil {
			return err
		}
		hash, _ := engine.CodeHash(root, recipeID)
		entry := fmt.Sprintf("waived %s: %s", findingID, f.WaiveReason)
		if restated {
			entry = fmt.Sprintf("waiver of %s restated: %s (was: %s)", findingID, f.WaiveReason, old)
		}
		appendEvidenceChangelog(root, recipeID, "interrogate", entry, hash)
		return nil
	}
	return fmt.Errorf("no finding %s in %s's interrogate run", findingID, recipeID)
}

// storeReproFiles keeps the files a finding's command needs under
// interrogate/repro/<id>/, and returns a drop reason when one is unsafe or
// missing. A repro file may add a test or a script; it may not replace the
// recipe's code, or the "bug" would be one the adversary wrote.
func storeReproFiles(root, recipeID string, f *state.InterrogateFinding, advCopy string, files []string, code map[string]bool) string {
	for _, rel := range files {
		clean := filepath.ToSlash(filepath.Clean(rel))
		if filepath.IsAbs(rel) || clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
			return fmt.Sprintf("repro file %q is outside the project", rel)
		}
		if code[clean] {
			return fmt.Sprintf("repro file %s is one of the recipe's code files; a repro may add files, not change the code under test", clean)
		}
		dst := filepath.Join(state.RecipeDir(root, recipeID), "interrogate", "repro", f.ID, filepath.FromSlash(clean))
		if err := copyOne(filepath.Join(advCopy, filepath.FromSlash(clean)), dst); err != nil {
			return fmt.Sprintf("repro file %s could not be kept: %v", clean, err)
		}
		if _, err := os.Stat(dst); err != nil {
			return fmt.Sprintf("repro file %s is missing from the adversary's copy", clean)
		}
		f.ReproFiles = append(f.ReproFiles, clean)
	}
	return ""
}

// rerunFinding runs a finding's command in a fresh copy of the current
// project with its stored repro files, and records the run with the code
// hash of the project on disk.
func rerunFinding(root, recipeID string, f *state.InterrogateFinding, timeoutSec int, sink io.Writer) (*state.CommandRun, error) {
	dir, err := os.MkdirTemp("", "bts-interrogate-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if _, err := copyProjectTo(root, dir); err != nil {
		return nil, fmt.Errorf("copy project: %w", err)
	}
	store := filepath.Join(state.RecipeDir(root, recipeID), "interrogate", "repro", f.ID)
	for _, rel := range f.ReproFiles {
		if err := copyOne(filepath.Join(store, filepath.FromSlash(rel)), filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			return nil, fmt.Errorf("restore repro file %s: %w", rel, err)
		}
	}
	// The run's output goes to its log only: on true-shark every repro's
	// full build output came before the findings summary (trial problem
	// #87).  The terminal gets one line per finding.
	sr, err := runShellCommand(dir, f.Command, timeoutSec, io.Discard)
	if err != nil {
		return nil, fmt.Errorf("run %q: %w", f.Command, err)
	}
	logDir := filepath.Join(state.LocalPath(root), "recipes", recipeID)
	logPath := filepath.Join(logDir, "interrogate-"+f.ID+".log")
	if err := os.MkdirAll(logDir, 0755); err == nil {
		_ = os.WriteFile(logPath, sr.Output, 0644)
	}
	printRunLine(sink, root, f.ID, sr.ExitCode, sr.TimedOut, logPath)
	hash, _ := engine.CodeHash(root, recipeID)
	return &state.CommandRun{
		Command: f.Command, ExitCode: sr.ExitCode, TimedOut: sr.TimedOut,
		CodeHash: hash, RunAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// printRunLine is the one line a rerun prints: its exit and where its
// output went.
func printRunLine(sink io.Writer, root, id string, exit int, timedOut bool, logPath string) {
	rel, err := filepath.Rel(root, logPath)
	if err != nil {
		rel = logPath
	}
	if timedOut {
		fmt.Fprintf(sink, "[bts]   %s: timed out, output in %s\n", id, rel)
		return
	}
	fmt.Fprintf(sink, "[bts]   %s: exit=%d, output in %s\n", id, exit, rel)
}

func countFindings(run *state.InterrogateRun) (reproduced, dropped int) {
	for _, f := range run.Findings {
		switch f.Status {
		case state.InterrogateReproduced:
			reproduced++
		case state.InterrogateDropped:
			dropped++
		}
	}
	return
}

func printInterrogateFindings(run *state.InterrogateRun) {
	fmt.Println("\n[bts] interrogate findings:")
	if len(run.Findings) == 0 {
		fmt.Println("  none reported")
		return
	}
	for _, f := range run.Findings {
		line := fmt.Sprintf("  %s %-10s %s — %s", f.ID, f.Status, f.Adversary, f.Title)
		if f.DropReason != "" {
			line += " (" + f.DropReason + ")"
		}
		fmt.Println(line)
	}
}
