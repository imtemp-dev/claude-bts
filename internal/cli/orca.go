package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/orca"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

// `bts orca` starts Claude Code sessions in Orca's terminals.  Continue
// hands a recipe to a fresh session in the same worktree: bts's records
// already survive a change of session, so the new one needs only a prompt
// naming the skill and the recipe (docs/research/orca-orchestration-design.md).

func init() {
	rootCmd.AddCommand(orcaCmd)
	orcaCmd.AddCommand(orcaStatusCmd, orcaContinueCmd)
	orcaContinueCmd.Flags().String("skill", "", "Skill the new session runs (default: the one the recipe's phase calls for)")
	orcaContinueCmd.Flags().String("agent", "claude", "Orca agent id to launch")
	orcaContinueCmd.Flags().String("model", "", "Model for the new session (default: the person's Orca settings)")
	orcaContinueCmd.Flags().Bool("yes", false, "Launch: the user agreed (needed while orca.launch is ask)")
	orcaContinueCmd.Flags().Bool("dry-run", false, "Show what would be launched and launch nothing")
}

var orcaCmd = &cobra.Command{
	Use:   "orca",
	Short: "Start and continue sessions through Orca",
}

var orcaStatusCmd = &cobra.Command{
	Use:          "status",
	Short:        "Show whether the Orca CLI and app are reachable and what orca.launch allows",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		settings, err := engine.LoadSettings(root)
		if err != nil {
			return err
		}
		fmt.Printf("orca.launch:  %s\n", settings.Orca.LaunchPolicy())
		fmt.Printf("this session: %s\n", map[bool]string{true: "in an Orca terminal", false: "outside Orca"}[orca.InOrca()])
		bin, err := orca.Binary()
		if err != nil {
			fmt.Printf("CLI:          %v\n", err)
			return nil
		}
		fmt.Printf("CLI:          %s\n", bin)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		st, err := orca.GetStatus(ctx, root)
		if err != nil {
			fmt.Printf("app:          %v\n", err)
			return nil
		}
		fmt.Printf("app:          running=%t runtime reachable=%t version=%s\n", st.Running, st.Reachable, st.Version)
		return nil
	},
}

// continueSkill is the skill a fresh session runs to pick a recipe up
// where it stands.
func continueSkill(r *state.RecipeState) (string, error) {
	switch {
	case r.Phase == "complete" || r.Phase == "cancelled":
		return "", fmt.Errorf("recipe %s is %s: there is nothing to continue", r.ID, r.Phase)
	case r.Type == "fix":
		return "bts-recipe-fix", nil
	case r.Phase == "finalize" || state.IsImplementPhase(r.Phase):
		if r.Type == "design" || r.Type == "analyze" {
			return "", fmt.Errorf("a %s recipe ends at its finalized document: %s has nothing left to continue", r.Type, r.ID)
		}
		return "bts-implement", nil
	case r.Type == "":
		return "bts-recipe-blueprint", nil
	}
	return "bts-recipe-" + r.Type, nil
}

// continuePrompt is what the fresh session is told.  It claims the recipe
// first: until then bts treats it as an observer and holds it to no gate.
func continuePrompt(id, skill string) string {
	return fmt.Sprintf("Run `bts recipe claim %s` first, then run /%s %s. "+
		"You are a fresh session taking recipe %s over from an earlier one; `bts recipe status` shows where it stands.",
		id, skill, id, id)
}

var orcaContinueCmd = &cobra.Command{
	Use:   "continue [recipe-id]",
	Short: "Start a fresh Claude Code session in Orca that takes the recipe over",
	Long: `Starts a Claude Code session in a new terminal of the Orca worktree that
holds this project and tells it to claim the recipe and run the skill its
phase calls for (/bts-implement after a finalized spec, the recipe's own
skill before).  The current session should end its turn afterwards.

orca.launch decides: ask (the default) shows what would start and launches
only with --yes, once the user agreed; auto launches without --yes; off
refuses.  The launch is recorded in the recipe's changelog.`,
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _, root, err := resolveRecipe(args)
		if err != nil {
			return err
		}
		r, err := state.LoadRecipeState(root, id)
		if err != nil {
			return err
		}
		skill, _ := cmd.Flags().GetString("skill")
		skill = strings.TrimPrefix(strings.TrimSpace(skill), "/")
		if skill == "" {
			if skill, err = continueSkill(r); err != nil {
				return err
			}
		}
		agent, _ := cmd.Flags().GetString("agent")
		model, _ := cmd.Flags().GetString("model")
		yes, _ := cmd.Flags().GetBool("yes")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		settings, err := engine.LoadSettings(root)
		if err != nil {
			return err
		}
		policy := settings.Orca.LaunchPolicy()
		if policy == engine.OrcaLaunchOff {
			return errors.New("orca.launch is off in .bts/config/settings.yaml: bts starts no Orca session")
		}
		prompt := continuePrompt(id, skill)
		fmt.Printf("[bts] a fresh %s session in Orca would take %s over:\n  runs:   /%s %s\n  prompt: %s\n", agent, id, skill, id, prompt)
		if dryRun {
			fmt.Println("dry run: nothing launched")
			return nil
		}
		if policy == engine.OrcaLaunchAsk && !yes {
			fmt.Printf("orca.launch is ask: ask the user, and on their yes run `bts orca continue %s --yes`", id)
			if skill != "" && cmd.Flags().Changed("skill") {
				fmt.Printf(" (with --skill %s)", skill)
			}
			fmt.Println(".")
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		st, err := orca.GetStatus(ctx, root)
		if err != nil {
			return fmt.Errorf("cannot reach Orca: %w", err)
		}
		if !st.Running || !st.Reachable {
			return errors.New("the Orca app is not running (or its runtime is not reachable): open Orca and run this again")
		}
		l, err := orca.LaunchAgent(ctx, root, agent, model, prompt)
		if err != nil {
			var oe *orca.Error
			if errors.As(err, &oe) {
				return fmt.Errorf("%w\nIf Orca does not know this project, add it in Orca first (`orca repo add --path %s`)", err, root)
			}
			return err
		}
		where := l.Terminal
		if where == "" {
			where = l.Kind
		}
		_ = state.AppendChangelog(root, id, &state.ChangelogEntry{
			Action: "continue",
			Output: "/" + skill,
			Result: fmt.Sprintf("launched %s in Orca %s (pane %s, operation %s)", agent, where, l.PaneKey, l.OperationID),
		})
		fmt.Printf("[bts] launched in Orca %s; it claims %s and runs /%s.\nEnd this turn now: once the new session claims the recipe, this one is an observer.\n", where, id, skill)
		return nil
	},
}
