package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

// bts prove: P2 of the prove stage, after pstack's create-verification-skill.
// The project's verify-<app> skill drives the real app the way a user does;
// bts records each journey's drive with the code hash and the evidence it
// left, so the gate app_proven judges runs, not an agent's account of them.
//
//   bts prove plan <id> --journey <name> [--feature <file>]   journeys this change touches
//   bts prove plan <id> --no-surface "<reason>"                nothing a user drives
//   bts prove run <id> --journey <name> --cmd "<drive>" [--evidence <path>]...
//   bts prove defer <id> --journey <name> --reason "..." --owner "..."
//   bts prove status <id>

func init() {
	rootCmd.AddCommand(proveCmd)
	proveCmd.AddCommand(provePlanCmd, proveRunCmd, proveDeferCmd, proveStatusCmd)
	provePlanCmd.Flags().StringSlice("journey", nil, "A user journey this change touches (repeatable)")
	provePlanCmd.Flags().String("feature", "", "The feature-map file the journeys come from")
	provePlanCmd.Flags().String("no-surface", "", "Record that the change has no user-facing surface, with the reason")
	provePlanCmd.Flags().StringSlice("inv", nil, "Invariants the named journeys' runs observe (repeatable or comma-separated); an invariant guarded by prompt text must be named on a journey")
	proveRunCmd.Flags().String("journey", "", "Planned journey (required)")
	proveRunCmd.Flags().String("cmd", "", "Drive command: launches the app, drives the journey, checks the end state, cleans up; exits non-zero on failure (required)")
	proveRunCmd.Flags().StringSlice("evidence", nil, "File the drive must leave behind, relative to the project root (repeatable)")
	proveRunCmd.Flags().String("verdict", "", `File the drive writes its checks to, relative to the project root: {"checks": [{"check": "...", "pass": true|false}]} (required)`)
	_ = proveRunCmd.MarkFlagRequired("verdict")
	proveRunCmd.Flags().Int("timeout", 900, "Seconds before the drive is killed")
	_ = proveRunCmd.MarkFlagRequired("journey")
	_ = proveRunCmd.MarkFlagRequired("cmd")
	proveDeferCmd.Flags().String("journey", "", "Planned journey (required)")
	proveDeferCmd.Flags().String("reason", "", "Why it cannot run here (required)")
	proveDeferCmd.Flags().String("owner", "", "Who runs it on the real target (required)")
	for _, f := range []string{"journey", "reason", "owner"} {
		_ = proveDeferCmd.MarkFlagRequired(f)
	}
}

var proveCmd = &cobra.Command{
	Use:     "prove",
	Short:   "Record user journeys driven through the real app (prove stage)",
	GroupID: "tools",
}

var provePlanCmd = &cobra.Command{
	Use:          "plan <recipe-id>",
	Short:        "Name the user journeys this change touches, or record that it has no user-facing surface",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		journeys, _ := cmd.Flags().GetStringSlice("journey")
		feature, _ := cmd.Flags().GetString("feature")
		noSurface, _ := cmd.Flags().GetString("no-surface")
		invs, _ := cmd.Flags().GetStringSlice("inv")
		rec, err := executeProvePlan(root, args[0], journeys, feature, noSurface, invs...)
		if err != nil {
			return err
		}
		printProveStatus(rec)
		return nil
	},
}

var proveRunCmd = &cobra.Command{
	Use:          "run <recipe-id>",
	Short:        "Drive one journey through the real app and record the run, its code hash and its evidence",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		journey, _ := cmd.Flags().GetString("journey")
		cmdStr, _ := cmd.Flags().GetString("cmd")
		evidence, _ := cmd.Flags().GetStringSlice("evidence")
		verdict, _ := cmd.Flags().GetString("verdict")
		timeout, _ := cmd.Flags().GetInt("timeout")
		j, err := executeProveRun(root, args[0], journey, cmdStr, evidence, verdict, timeout, os.Stdout)
		if err != nil {
			return err
		}
		if !j.Passed() {
			fmt.Printf("\n[bts] %s FAILED: exit=%d, checks %d, failed %v, evidence missing %v", journey, j.Run.ExitCode, j.Checks, j.ChecksFailed, j.EvidenceMissing)
			if j.VerdictError != "" {
				fmt.Printf(", verdict: %s", j.VerdictError)
			} else if j.Checks == 0 {
				fmt.Print(", verdict: no checks recorded")
			}
			fmt.Println(" → prove.json")
			os.Exit(1)
		}
		fmt.Printf("\n[bts] %s driven: exit=0, %d checks passed, evidence %v → prove.json\n", journey, j.Checks, j.Evidence)
		return nil
	},
}

var proveDeferCmd = &cobra.Command{
	Use:          "defer <recipe-id>",
	Short:        "Record that a journey cannot run here, why, and who runs it on the real target",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		journey, _ := cmd.Flags().GetString("journey")
		reason, _ := cmd.Flags().GetString("reason")
		owner, _ := cmd.Flags().GetString("owner")
		if err := executeProveDefer(root, args[0], journey, reason, owner); err != nil {
			return err
		}
		fmt.Printf("[bts] %s deferred to %s → prove.json\n", journey, owner)
		return nil
	},
}

var proveStatusCmd = &cobra.Command{
	Use:          "status <recipe-id>",
	Short:        "Show the planned journeys and whether each is driven, deferred or open",
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		rec, err := state.LoadProveRecord(root, args[0])
		if err != nil {
			return err
		}
		if rec == nil {
			fmt.Println("[bts] no drive record yet")
			return nil
		}
		printProveStatus(rec)
		hash, _ := engine.CodeHash(root, args[0])
		if reason := engine.CheckAppProven(rec, hash, args[0]); reason != "" {
			fmt.Printf("[bts] %s\n", reason)
		}
		return nil
	},
}

func loadOrNewProve(root, recipeID string) (*state.ProveRecord, error) {
	if _, err := state.LoadRecipeState(root, recipeID); err != nil {
		return nil, fmt.Errorf("load recipe %s: %w", recipeID, err)
	}
	rec, err := state.LoadProveRecord(root, recipeID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		rec = &state.ProveRecord{Journeys: map[string]*state.Journey{}}
	}
	return rec, nil
}

func executeProvePlan(root, recipeID string, journeys []string, feature, noSurface string, invs ...string) (*state.ProveRecord, error) {
	rec, err := loadOrNewProve(root, recipeID)
	if err != nil {
		return nil, err
	}
	noSurface = strings.TrimSpace(noSurface)
	switch {
	case noSurface != "" && len(journeys) > 0:
		return nil, errors.New("--no-surface and --journey exclude each other")
	case len(invs) > 0 && len(journeys) == 0:
		return nil, errors.New("--inv names what a --journey observes; name the journey too")
	case noSurface != "":
		rec.NoSurface = noSurface
	case len(journeys) == 0:
		return nil, errors.New("name at least one --journey, or record --no-surface \"<reason>\"")
	default:
		rec.NoSurface = ""
		for _, name := range journeys {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if rec.Journeys[name] == nil {
				rec.Journeys[name] = &state.Journey{}
			}
			if feature != "" {
				rec.Journeys[name].Feature = feature
			}
			rec.Journeys[name].Invariants = mergeInvariants(rec.Journeys[name].Invariants, invs)
		}
	}
	if err := state.SaveProveRecord(root, recipeID, rec); err != nil {
		return nil, err
	}
	hash, _ := engine.CodeHash(root, recipeID)
	result := fmt.Sprintf("plan: %s", strings.Join(sortedJourneys(rec), ", "))
	if rec.NoSurface != "" {
		result = "plan: no user-facing surface: " + rec.NoSurface
	}
	appendEvidenceChangelog(root, recipeID, "prove", result, hash)
	return rec, nil
}

// mergeInvariants adds the comma- or flag-separated invariant ids in add
// to have, trimmed, de-duplicated and sorted.
func mergeInvariants(have []string, add []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{have, add} {
		for _, raw := range list {
			for _, id := range strings.Split(raw, ",") {
				id = strings.TrimSpace(id)
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	sort.Strings(out)
	return out
}

func executeProveRun(root, recipeID, journey, cmdStr string, evidence []string, verdict string, timeoutSec int, sink io.Writer) (*state.Journey, error) {
	rec, err := loadOrNewProve(root, recipeID)
	if err != nil {
		return nil, err
	}
	j := rec.Journeys[journey]
	if j == nil {
		return nil, fmt.Errorf("%s is not a planned journey; plan it first with `bts prove plan %s --journey %s`", journey, recipeID, journey)
	}
	if strings.TrimSpace(verdict) == "" {
		return nil, errors.New("--verdict is required: the file where the drive writes each check and whether it passed")
	}
	for _, e := range append(append([]string{}, evidence...), verdict) {
		if filepath.IsAbs(e) || strings.HasPrefix(filepath.ToSlash(filepath.Clean(e)), "../") {
			return nil, fmt.Errorf("evidence %q must be a path inside the project", e)
		}
	}
	started := time.Now()
	run, err := runRecorded(root, recipeID, cmdStr, timeoutSec, "prove-"+journey+".log", sink)
	if err != nil {
		return nil, err
	}
	j.Verdict = verdict
	j.Checks, j.ChecksFailed, j.VerdictError = readVerdict(filepath.Join(root, verdict), started)
	var missing []string
	for _, e := range evidence {
		info, serr := os.Stat(filepath.Join(root, e))
		if serr != nil || info.Size() == 0 {
			missing = append(missing, e)
		}
	}
	j.Run, j.Evidence, j.EvidenceMissing, j.Deferred = run, evidence, missing, nil
	if err := state.SaveProveRecord(root, recipeID, rec); err != nil {
		return nil, err
	}
	result := "passed"
	if !j.Passed() {
		result = "failed"
	}
	appendEvidenceChangelog(root, recipeID, "prove",
		fmt.Sprintf("run %s %s: exit=%d, checks %d (%d failed), evidence %d/%d", journey, result, run.ExitCode,
			j.Checks, len(j.ChecksFailed), len(evidence)-len(missing), len(evidence)), run.CodeHash)
	return j, nil
}

// readVerdict reads the checks a drive wrote: {"checks": [{"check": "<what
// it checked>", "pass": true|false, ...}]}, with "name" and "passed" taken
// as the same fields.  It returns how many checks there were, the names of
// the failed ones, and why the file could not be used: missing, left from
// an earlier run (written before this one started), not JSON, or a check
// with no pass value.  A top-level "pass": false counts as a failed check.
func readVerdict(path string, started time.Time) (int, []string, string) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, nil, "the drive wrote no verdict file"
	}
	if info.ModTime().Before(started.Add(-2 * time.Second)) {
		return 0, nil, "the verdict file was not written by this run (it is older than the drive)"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, nil, err.Error()
	}
	var v struct {
		Pass   *bool            `json:"pass"`
		Passed *bool            `json:"passed"`
		Checks []map[string]any `json:"checks"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return 0, nil, "the verdict file is not JSON: " + err.Error()
	}
	var failed []string
	for i, c := range v.Checks {
		name, _ := c["check"].(string)
		if name == "" {
			name, _ = c["name"].(string)
		}
		if name == "" {
			name = fmt.Sprintf("check %d", i+1)
		}
		ok, has := c["pass"].(bool)
		if !has {
			ok, has = c["passed"].(bool)
		}
		if !has {
			return len(v.Checks), nil, fmt.Sprintf("%s has no pass value", name)
		}
		if !ok {
			failed = append(failed, name)
		}
	}
	for _, top := range []*bool{v.Pass, v.Passed} {
		if top != nil && !*top {
			failed = append(failed, "verdict: pass=false")
			break
		}
	}
	return len(v.Checks), failed, ""
}

func executeProveDefer(root, recipeID, journey, reason, owner string) error {
	if strings.TrimSpace(reason) == "" || strings.TrimSpace(owner) == "" {
		return errors.New("--reason and --owner are both required: a deferral says why and who runs it")
	}
	rec, err := loadOrNewProve(root, recipeID)
	if err != nil {
		return err
	}
	j := rec.Journeys[journey]
	if j == nil {
		return fmt.Errorf("%s is not a planned journey; plan it first with `bts prove plan %s --journey %s`", journey, recipeID, journey)
	}
	j.Deferred = &state.Deferral{Reason: strings.TrimSpace(reason), Owner: strings.TrimSpace(owner), At: time.Now().UTC().Format(time.RFC3339)}
	if err := state.SaveProveRecord(root, recipeID, rec); err != nil {
		return err
	}
	hash, _ := engine.CodeHash(root, recipeID)
	appendEvidenceChangelog(root, recipeID, "prove", fmt.Sprintf("deferred %s to %s: %s", journey, j.Deferred.Owner, j.Deferred.Reason), hash)
	return nil
}

func sortedJourneys(rec *state.ProveRecord) []string {
	names := make([]string, 0, len(rec.Journeys))
	for n := range rec.Journeys {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func printProveStatus(rec *state.ProveRecord) {
	if rec.NoSurface != "" {
		fmt.Printf("[bts] no user-facing surface: %s\n", rec.NoSurface)
		return
	}
	fmt.Println("[bts] journeys:")
	for _, n := range sortedJourneys(rec) {
		j := rec.Journeys[n]
		status := "open"
		switch {
		case j.Deferred != nil:
			status = fmt.Sprintf("deferred to %s (%s)", j.Deferred.Owner, j.Deferred.Reason)
		case j.Run != nil && j.Passed():
			status = "driven"
			if j.Verdict != "" {
				status = fmt.Sprintf("driven, %d checks passed", j.Checks)
			}
		case j.Run != nil:
			status = "failed"
			switch {
			case j.VerdictError != "":
				status += ": " + j.VerdictError
			case len(j.ChecksFailed) > 0:
				status += ": " + strings.Join(j.ChecksFailed, "; ")
			case j.Verdict != "" && j.Checks == 0 && j.Run.Passed() && len(j.EvidenceMissing) == 0:
				status += ": no checks recorded"
			}
		}
		if len(j.Invariants) > 0 {
			status += " [observes " + strings.Join(j.Invariants, ", ") + "]"
		}
		fmt.Printf("  %-24s %s\n", n, status)
	}
}
