package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/multica"
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
		if err := feature.SaveMulticaConfig(&feature.MulticaConfig{Manager: manager.ID, Worker: worker.ID}); err != nil {
			return err
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

func init() {
	rootCmd.AddCommand(multicaCmd)
	multicaCmd.AddCommand(multicaSetupCmd)
	multicaSetupCmd.Flags().String("manager", "", "The manager agent (name or id)")
	multicaSetupCmd.Flags().String("worker", "", "The worker agent (name or id)")
	featurePublishCmd.Flags().String("manager", "", "Manager agent id (default: the store's, from bts multica setup)")
	featurePublishCmd.Flags().String("worker", "", "Worker agent id (default: the store's)")
	featureCmd.AddCommand(featurePublishCmd, featureDispatchCmd)
}
