package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/jlim/claude-forge/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(debateCmd)
	debateCmd.AddCommand(debateListCmd, debateExportCmd, debateLogCmd, debateResumeCmd)

	debateLogCmd.Flags().String("topic", "", "Debate topic (required for first round)")
	debateLogCmd.Flags().Int("round", 0, "Round number")
	debateLogCmd.Flags().String("content", "", "Round content summary")
	debateLogCmd.Flags().String("id", "", "Debate ID (auto-generated if empty)")
}

var debateCmd = &cobra.Command{
	Use:     "debate",
	Short:   "Manage debate state",
	GroupID: "tools",
}

var debateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all debates",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a forge project: %w", err)
		}

		debates, err := state.ListDebates(root)
		if err != nil {
			return fmt.Errorf("list: %w", err)
		}

		if len(debates) == 0 {
			fmt.Println("No debates found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tTopic\tRounds\tDecided\tUpdated")
		for _, d := range debates {
			fmt.Fprintf(w, "%s\t%s\t%d\t%v\t%s\n",
				d.ID, truncate(d.Topic, 30), d.Rounds, d.Decided, d.UpdatedAt)
		}
		w.Flush()
		return nil
	},
}

var debateLogCmd = &cobra.Command{
	Use:   "log",
	Short: "Record a debate round (creates debate if new)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a forge project: %w", err)
		}

		topic, _ := cmd.Flags().GetString("topic")
		round, _ := cmd.Flags().GetInt("round")
		content, _ := cmd.Flags().GetString("content")
		debateID, _ := cmd.Flags().GetString("id")

		if topic == "" && debateID == "" {
			return fmt.Errorf("--topic or --id required")
		}

		// Find or create debate
		if debateID == "" {
			// Look for existing debate with same topic
			debates, _ := state.ListDebates(root)
			for _, d := range debates {
				if d.Topic == topic && !d.Decided {
					debateID = d.ID
					break
				}
			}
			if debateID == "" {
				debateID = state.NewDebateID()
			}
		}

		// Create debate directory
		debateDir := state.DebateDir(root, debateID)
		if err := os.MkdirAll(debateDir, 0755); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}

		// Load or create state
		ds, err := state.LoadDebateState(root, debateID)
		if err != nil {
			ds = &state.DebateState{
				ID:    debateID,
				Topic: topic,
			}
		}
		if topic != "" {
			ds.Topic = topic
		}
		if round > ds.Rounds {
			ds.Rounds = round
		}

		// Save round content
		if round > 0 && content != "" {
			roundPath := fmt.Sprintf("%s/round-%d.md", debateDir, round)
			if err := os.WriteFile(roundPath, []byte(content), 0644); err != nil {
				return fmt.Errorf("write round: %w", err)
			}
		}

		if err := state.SaveDebateState(root, ds); err != nil {
			return fmt.Errorf("save: %w", err)
		}

		fmt.Printf("Debate %s: round %d logged (topic: %s)\n", debateID, round, ds.Topic)
		return nil
	},
}

var debateResumeCmd = &cobra.Command{
	Use:   "resume <debate-id>",
	Short: "Show previous debate rounds for continuation",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a forge project: %w", err)
		}

		ds, err := state.LoadDebateState(root, args[0])
		if err != nil {
			return fmt.Errorf("load debate: %w", err)
		}

		fmt.Printf("# Debate: %s\n", ds.Topic)
		fmt.Printf("Rounds completed: %d | Decided: %v\n\n", ds.Rounds, ds.Decided)

		for i := 1; i <= ds.Rounds; i++ {
			roundPath := fmt.Sprintf("%s/round-%d.md", state.DebateDir(root, args[0]), i)
			data, err := os.ReadFile(roundPath)
			if err == nil {
				fmt.Printf("## Round %d\n%s\n\n", i, string(data))
			}
		}

		if ds.Conclusion != "" {
			fmt.Printf("## Current Conclusion\n%s\n\n", ds.Conclusion)
		}

		fmt.Printf("Continue with round %d.\n", ds.Rounds+1)
		return nil
	},
}

var debateExportCmd = &cobra.Command{
	Use:   "export <debate-id>",
	Short: "Export debate as markdown",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a forge project: %w", err)
		}

		ds, err := state.LoadDebateState(root, args[0])
		if err != nil {
			return fmt.Errorf("load debate: %w", err)
		}

		fmt.Printf("# Debate: %s\n\n", ds.Topic)
		fmt.Printf("Rounds: %d | Decided: %v\n\n", ds.Rounds, ds.Decided)
		if ds.Conclusion != "" {
			fmt.Printf("## Conclusion\n%s\n\n", ds.Conclusion)
		}

		// Print round files if they exist
		for i := 1; i <= ds.Rounds; i++ {
			roundPath := fmt.Sprintf("%s/round-%d.md", state.DebateDir(root, args[0]), i)
			data, err := os.ReadFile(roundPath)
			if err == nil {
				fmt.Printf("## Round %d\n%s\n\n", i, string(data))
			}
		}

		return nil
	},
}
