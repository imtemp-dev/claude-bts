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
//   bts interrogate verify <id>                      rerun every reported finding
//   bts interrogate verify <id> --recheck            rerun open and fixed findings after fixes
//   bts interrogate waive <id> --finding F-001 --reason "..."

// adversaryAngles seed independence: one model family, separate contexts,
// different angles of attack.
var adversaryAngles = []string{
	"invariants and state: order, idempotence, retries, an effect done twice or not at all",
	"inputs and boundaries: malformed or hostile input, auth, errors and odd output from external tools",
	"operations: restart, a crash mid-step, configuration and environment",
}

func init() {
	rootCmd.AddCommand(interrogateCmd)
	interrogateCmd.AddCommand(interrogatePrepareCmd, interrogateVerifyCmd, interrogateWaiveCmd)
	interrogatePrepareCmd.Flags().Int("adversaries", 2, "Adversary seats: 2, or 3 for a contested or security-sensitive change")
	interrogateVerifyCmd.Flags().Bool("recheck", false, "After fixes: rerun open and fixed findings on the current code")
	interrogateVerifyCmd.Flags().Int("timeout", 600, "Seconds before each run is killed")
	interrogateWaiveCmd.Flags().String("finding", "", "Finding id, e.g. F-002 (required)")
	interrogateWaiveCmd.Flags().String("reason", "", "Why it is left unfixed, as the user agreed (required)")
	_ = interrogateWaiveCmd.MarkFlagRequired("finding")
	_ = interrogateWaiveCmd.MarkFlagRequired("reason")
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
		run, err := executeInterrogatePrepare(root, args[0], n)
		if err != nil {
			return err
		}
		fmt.Printf("[bts] interrogate prepared for %s. Scope (the recipe's code files):\n", args[0])
		for _, f := range engine.CodeFiles(root, args[0]) {
			fmt.Printf("  %s\n", f)
		}
		fmt.Println("[bts] seats:")
		for _, a := range run.Adversaries {
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

func executeInterrogatePrepare(root, recipeID string, n int) (*state.InterrogateRun, error) {
	if _, err := state.LoadRecipeState(root, recipeID); err != nil {
		return nil, fmt.Errorf("load recipe %s: %w", recipeID, err)
	}
	if n < 2 || n > len(adversaryAngles) {
		return nil, fmt.Errorf("--adversaries must be 2 or 3, got %d", n)
	}
	if existing, err := state.LoadInterrogateRun(root, recipeID); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, fmt.Errorf("%s already has an interrogate run (prepared %s); verify it, or recheck it after fixes", recipeID, existing.PreparedAt)
	}
	if err := os.MkdirAll(filepath.Join(state.RecipeDir(root, recipeID), "interrogate"), 0755); err != nil {
		return nil, err
	}
	run := &state.InterrogateRun{PreparedAt: time.Now().UTC().Format(time.RFC3339)}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("a%d", i+1)
		dir, err := os.MkdirTemp("", "bts-adversary-"+recipeID+"-"+name+"-")
		if err != nil {
			return nil, err
		}
		if _, err := copyProjectTo(root, dir); err != nil {
			return nil, fmt.Errorf("copy project for %s: %w", name, err)
		}
		run.Adversaries = append(run.Adversaries, &state.Adversary{
			Name: name, Angle: adversaryAngles[i], Copy: dir,
			Report: filepath.ToSlash(filepath.Join("interrogate", name+".json")),
		})
	}
	if err := state.SaveInterrogateRun(root, recipeID, run); err != nil {
		return nil, err
	}
	hash, _ := engine.CodeHash(root, recipeID)
	appendEvidenceChangelog(root, recipeID, "interrogate", fmt.Sprintf("prepared %d adversaries", n), hash)
	return run, nil
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
	code := map[string]bool{}
	for _, f := range engine.CodeFiles(root, recipeID) {
		code[f] = true
	}
	var reports []*adversaryReport
	for _, a := range run.Adversaries {
		data, err := os.ReadFile(filepath.Join(recipeDir, filepath.FromSlash(a.Report)))
		if err != nil {
			return nil, fmt.Errorf("adversary %s wrote no report at %s: %w", a.Name, a.Report, err)
		}
		var rep adversaryReport
		if err := json.Unmarshal(data, &rep); err != nil {
			return nil, fmt.Errorf("adversary %s report is not valid JSON: %w", a.Name, err)
		}
		rep.Adversary = a.Name
		reports = append(reports, &rep)
	}
	n := 0
	for i, rep := range reports {
		adv := run.Adversaries[i]
		for _, r := range rep.Findings {
			n++
			f := &state.InterrogateFinding{
				ID: fmt.Sprintf("F-%03d", n), Adversary: adv.Name, Title: r.Title,
				Command: strings.TrimSpace(r.Command), Expected: r.Expected, Observed: r.Observed,
			}
			run.Findings = append(run.Findings, f)
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
	run.VerifiedAt = time.Now().UTC().Format(time.RFC3339)
	if err := state.SaveInterrogateRun(root, recipeID, run); err != nil {
		return nil, err
	}
	for _, a := range run.Adversaries {
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
	fixed, open := 0, 0
	for _, f := range run.Findings {
		if f.Status != state.InterrogateReproduced && f.Status != state.InterrogateFixed {
			continue
		}
		fmt.Fprintf(sink, "\n[bts] recheck %s: %s\n", f.ID, f.Command)
		cr, err := rerunFinding(root, recipeID, f, timeoutSec, sink)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.ID, err)
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
	if err := state.SaveInterrogateRun(root, recipeID, run); err != nil {
		return nil, err
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
		if f.Status != state.InterrogateReproduced {
			return fmt.Errorf("%s is %s; only a reproduced finding can be waived", findingID, f.Status)
		}
		f.Status, f.WaiveReason = state.InterrogateWaived, strings.TrimSpace(reason)
		if err := state.SaveInterrogateRun(root, recipeID, run); err != nil {
			return err
		}
		hash, _ := engine.CodeHash(root, recipeID)
		appendEvidenceChangelog(root, recipeID, "interrogate", fmt.Sprintf("waived %s: %s", findingID, f.WaiveReason), hash)
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
	sr, err := runShellCommand(dir, f.Command, timeoutSec, sink)
	if err != nil {
		return nil, fmt.Errorf("run %q: %w", f.Command, err)
	}
	logDir := filepath.Join(state.LocalPath(root), "recipes", recipeID)
	if err := os.MkdirAll(logDir, 0755); err == nil {
		_ = os.WriteFile(filepath.Join(logDir, "interrogate-"+f.ID+".log"), sr.Output, 0644)
	}
	hash, _ := engine.CodeHash(root, recipeID)
	return &state.CommandRun{
		Command: f.Command, ExitCode: sr.ExitCode, TimedOut: sr.TimedOut,
		CodeHash: hash, RunAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
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
