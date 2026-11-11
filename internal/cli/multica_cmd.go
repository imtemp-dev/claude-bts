package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/multica"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

var multicaCmd = &cobra.Command{
	Use:     "multica",
	Short:   "Connect bts features to a Multica workspace",
	GroupID: "tools",
}

var multicaSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Install the bts-feature-manager and bts-feature-worker skills in the Multica workspace, give them to the two agents, and record the agents for the feature store",
	RunE: func(cmd *cobra.Command, args []string) error {
		mName, _ := cmd.Flags().GetString("manager")
		wName, _ := cmd.Flags().GetString("worker")
		if mName == "" || wName == "" {
			return fmt.Errorf("name both agents: --manager <name|id> --worker <name|id>")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		manager, err := multica.ResolveAgent(ctx, mName)
		if err != nil {
			return err
		}
		worker, err := multica.ResolveAgent(ctx, wName)
		if err != nil {
			return err
		}
		ids := map[string]string{}
		for _, s := range multica.Skills {
			id, created, err := multica.UpsertSkill(ctx, s)
			if err != nil {
				return fmt.Errorf("skill %s: %w", s.Name, err)
			}
			ids[s.Name] = id
			verb := "updated"
			if created {
				verb = "created"
			}
			fmt.Printf("Skill %s %s (%s)\n", s.Name, verb, id)
		}
		if err := multica.AttachSkill(ctx, manager.ID, ids["bts-feature-manager"]); err != nil {
			return err
		}
		if err := multica.AttachSkill(ctx, worker.ID, ids["bts-feature-worker"]); err != nil {
			return err
		}
		cfg := &feature.MulticaConfig{Manager: manager.ID, Worker: worker.ID}
		if old, err := feature.LoadMulticaConfig(); err == nil {
			cfg.BranchPrefix = old.BranchPrefix
		}
		if cmd.Flags().Changed("branch-prefix") {
			cfg.BranchPrefix, _ = cmd.Flags().GetString("branch-prefix")
		}
		if err := feature.SaveMulticaConfig(cfg); err != nil {
			return err
		}
		if cfg.BranchPrefix != "" {
			fmt.Printf("Item branches: %s<ticket>-<issue key>-<slug>\n", cfg.BranchPrefix)
		}
		fmt.Printf("Manager %s and worker %s recorded for the feature store.\n", manager.Name, worker.Name)
		return nil
	},
}

var featurePublishCmd = &cobra.Command{
	Use:   "publish <feature>",
	Short: "Create the feature's parent issue on Multica, assigned to the manager agent, once the user said to start",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := feature.Find(args[0])
		if err != nil {
			return err
		}
		manager, _ := cmd.Flags().GetString("manager")
		worker, _ := cmd.Flags().GetString("worker")
		if manager == "" || worker == "" {
			cfg, err := feature.LoadMulticaConfig()
			if err != nil {
				return err
			}
			if manager == "" {
				manager = cfg.Manager
			}
			if worker == "" {
				worker = cfg.Worker
			}
		}
		link, existed, err := feature.Publish(dir, manager, worker)
		if err != nil {
			return err
		}
		if existed {
			fmt.Printf("Already published as %s.\n", link.ParentKey)
		} else {
			fmt.Printf("Published as %s; the manager's run there dispatches the first items.\n", link.ParentKey)
		}
		return nil
	},
}

var featureDispatchCmd = &cobra.Command{
	Use:   "dispatch <feature>",
	Short: "Keep the feature's Multica sub-issues in step with the confirmed plan and start the ready items (manager)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := feature.Find(args[0])
		if err != nil {
			return err
		}
		actions, err := feature.Dispatch(dir)
		for _, a := range actions {
			if a.Item != "" {
				fmt.Printf("%s %s: %s\n", a.Item, a.Issue, a.What)
			} else {
				fmt.Printf("%s: %s\n", a.Issue, a.What)
			}
		}
		if err != nil {
			return err
		}
		if len(actions) == 0 {
			fmt.Println("In step; nothing to do.")
		}
		return nil
	},
}

// itemArgs resolves <feature> <item>, or the worktree's newest bound recipe.
func itemArgs(args []string) (root, fid, item string, err error) {
	root = cwdRoot()
	if root == "" {
		return "", "", "", fmt.Errorf("not a bts project: run this in the item's worktree")
	}
	if len(args) == 2 {
		return root, args[0], args[1], nil
	}
	recipes, err := state.ListRecipes(root)
	if err != nil {
		return "", "", "", err
	}
	var best *state.RecipeState
	for _, r := range recipes {
		if r.Feature != "" && (best == nil || r.StartedAt > best.StartedAt) {
			best = r
		}
	}
	if best == nil {
		return "", "", "", fmt.Errorf("this worktree has no feature item's recipe; name it: <feature> <item>")
	}
	return root, best.Feature, best.FeatureItem, nil
}

var featurePRCmd = &cobra.Command{
	Use:   "pr [feature item]",
	Short: "Push the item's branch and open its draft pull request, or push to the one open (worker, after the recipe completes)",
	Long: `Renames a branch Multica named (agent/…) to <prefix><ticket>-<issue key>-<slug>, pushes it,
and opens a draft pull request against the item's base: the feature's base branch, or the
branch of the unmerged item it is stacked on.  Run again, it pushes new commits to the same
pull request.  It never marks a pull request ready, merges, or pushes another branch.`,
	Args: cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, fid, item, err := itemArgs(args)
		if err != nil {
			return err
		}
		pr, created, err := feature.OpenDraftPR(root, fid, item)
		if err != nil {
			cmd.SilenceUsage = true
			return err
		}
		verb := "Pushed to"
		if created {
			verb = "Opened draft"
		}
		fmt.Fprintf(os.Stderr, "[bts] %s %s (%s → %s)\n", verb, pr.URL, pr.Branch, pr.Base)
		fmt.Println(pr.URL)
		return nil
	},
}

var featureRestackCmd = &cobra.Command{
	Use:   "restack [feature item]",
	Short: "Move the item's branch onto the base branch after the item it was stacked on merged, and point its pull request there (worker)",
	Args:  cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, fid, item, err := itemArgs(args)
		if err != nil {
			return err
		}
		rs, err := feature.Restack(root, fid, item)
		if err != nil {
			cmd.SilenceUsage = true
			return err
		}
		fmt.Printf("Restacked onto %s (fork %s → %s); head %s.\n", rs.ToRef, shortSHA(rs.OldFork), shortSHA(rs.NewFork), shortSHA(rs.HeadSHA))
		return nil
	},
}

func shortSHA(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

func init() {
	rootCmd.AddCommand(multicaCmd)
	multicaCmd.AddCommand(multicaSetupCmd)
	multicaSetupCmd.Flags().String("manager", "", "The manager agent (name or id)")
	multicaSetupCmd.Flags().String("worker", "", "The worker agent (name or id)")
	multicaSetupCmd.Flags().String("branch-prefix", "", "Prefix for item branches, e.g. Your-Name/ (kept from the last setup when not given)")
	featureCmd.AddCommand(featurePRCmd, featureRestackCmd)
	featurePublishCmd.Flags().String("manager", "", "Manager agent id (default: the store's, from bts multica setup)")
	featurePublishCmd.Flags().String("worker", "", "Worker agent id (default: the store's)")
	featureCmd.AddCommand(featurePublishCmd, featureDispatchCmd)
}
