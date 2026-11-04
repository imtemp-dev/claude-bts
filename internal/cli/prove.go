package cli

import (
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
	proveRunCmd.Flags().String("journey", "", "Planned journey (required)")
	proveRunCmd.Flags().String("cmd", "", "Drive command: launches the app, drives the journey, checks the end state, cleans up; exits non-zero on failure (required)")
	proveRunCmd.Flags().StringSlice("evidence", nil, "File the drive must leave behind, relative to the project root (repeatable)")
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
		rec, err := executeProvePlan(root, args[0], journeys, feature, noSurface)
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
		timeout, _ := cmd.Flags().GetInt("timeout")
		j, err := executeProveRun(root, args[0], journey, cmdStr, evidence, timeout, os.Stdout)
		if err != nil {
			return err
		}
		if !j.Passed() {
			fmt.Printf("\n[bts] %s FAILED: exit=%d, evidence missing: %v → prove.json\n", journey, j.Run.ExitCode, j.EvidenceMissing)
			os.Exit(1)
		}
		fmt.Printf("\n[bts] %s driven: exit=0, evidence %v → prove.json\n", journey, j.Evidence)
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

func executeProvePlan(root, recipeID string, journeys []string, feature, noSurface string) (*state.ProveRecord, error) {
	rec, err := loadOrNewProve(root, recipeID)
	if err != nil {
		return nil, err
	}
	noSurface = strings.TrimSpace(noSurface)
	switch {
	case noSurface != "" && len(journeys) > 0:
		return nil, errors.New("--no-surface and --journey exclude each other")
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

func executeProveRun(root, recipeID, journey, cmdStr string, evidence []string, timeoutSec int, sink io.Writer) (*state.Journey, error) {
	rec, err := loadOrNewProve(root, recipeID)
	if err != nil {
		return nil, err
	}
	j := rec.Journeys[journey]
	if j == nil {
		return nil, fmt.Errorf("%s is not a planned journey; plan it first with `bts prove plan %s --journey %s`", journey, recipeID, journey)
	}
	for _, e := range evidence {
		if filepath.IsAbs(e) || strings.HasPrefix(filepath.ToSlash(filepath.Clean(e)), "../") {
			return nil, fmt.Errorf("evidence %q must be a path inside the project", e)
		}
	}
	run, err := runRecorded(root, recipeID, cmdStr, timeoutSec, "prove-"+journey+".log", sink)
	if err != nil {
		return nil, err
	}
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
	verdict := "passed"
	if !j.Passed() {
		verdict = "failed"
	}
	appendEvidenceChangelog(root, recipeID, "prove",
		fmt.Sprintf("run %s %s: exit=%d, evidence %d/%d", journey, verdict, run.ExitCode, len(evidence)-len(missing), len(evidence)), run.CodeHash)
	return j, nil
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
		case j.Run != nil:
			status = "failed"
		}
		fmt.Printf("  %-24s %s\n", n, status)
	}
}
