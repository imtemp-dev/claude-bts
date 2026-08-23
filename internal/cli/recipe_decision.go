package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/imtemp-dev/jig/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	recipeCmd.AddCommand(recipeDecisionCmd)
	recipeDecisionCmd.AddCommand(
		recipeDecisionHoldCmd, recipeDecisionListCmd,
		recipeDecisionResolveCmd, recipeDecisionDropCmd,
	)

	recipeDecisionHoldCmd.Flags().String("key", "", "Stable identity for this decision (e.g. token-storage)")
	recipeDecisionHoldCmd.Flags().String("question", "", "What the user must decide")
	recipeDecisionHoldCmd.Flags().String("doc", "", "Document this decision blocks")
	recipeDecisionHoldCmd.Flags().StringArray("option", nil, "A candidate answer (repeatable)")
	recipeDecisionHoldCmd.Flags().StringArray("blocks", nil, "Finding or task ID held by this decision (repeatable)")
	_ = recipeDecisionHoldCmd.MarkFlagRequired("key")
	_ = recipeDecisionHoldCmd.MarkFlagRequired("question")

	recipeDecisionListCmd.Flags().Bool("open", false, "Only decisions still waiting on the user")
	recipeDecisionListCmd.Flags().Bool("json", false, "Emit JSON")

	recipeDecisionResolveCmd.Flags().String("answer", "", "The user's decision")
	_ = recipeDecisionResolveCmd.MarkFlagRequired("answer")

	recipeDecisionDropCmd.Flags().String("reason", "", "Why the question no longer needs an answer")
	_ = recipeDecisionDropCmd.MarkFlagRequired("reason")
}

var recipeDecisionCmd = &cobra.Command{
	Use:   "decision",
	Short: "Record and resolve questions that only the user can answer",
	Long: `A decision hold is the durable half of "ask the user for guidance".

When the verify loop exhausts its convergence budget, or a finding turns out
to need a product call rather than a spec edit, the question and its answer
otherwise live only in the conversation — a compaction or a new session loses
both, and the recipe cannot tell "waiting on a person" apart from "still
working".

  jig recipe decision hold <id> --key token-storage \
      --question "Refresh tokens in the keychain or an httpOnly cookie?" \
      --option keychain --option cookie --doc draft.md

While a decision is open the recipe is blocked: the completion gate refuses to
finalize, session start surfaces it, and jig doctor reports it. Recording the
answer clears the block and keeps it in the spec's tracked provenance:

  jig recipe decision resolve <id> token-storage --answer "httpOnly cookie"`,
}

var recipeDecisionHoldCmd = &cobra.Command{
	Use:   "hold [recipe-id]",
	Short: "Record a question for the user and block the recipe on it",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, recipeID, err := resolveRootAndRecipe(args)
		if err != nil {
			return err
		}
		key, _ := cmd.Flags().GetString("key")
		question, _ := cmd.Flags().GetString("question")
		doc, _ := cmd.Flags().GetString("doc")
		options, _ := cmd.Flags().GetStringArray("option")
		blocks, _ := cmd.Flags().GetStringArray("blocks")

		created, err := state.HoldDecision(root, recipeID, &state.DecisionEvent{
			Key: key, Question: question, Doc: doc, Options: options, Blocks: blocks,
		})
		if err != nil {
			return err
		}
		if !created {
			fmt.Printf("Decision %q already open — unchanged.\n", key)
			return nil
		}
		fmt.Printf("Decision %q held. %s is blocked until it is resolved:\n  jig recipe decision resolve %s %s --answer \"...\"\n",
			key, recipeID, recipeID, key)
		return nil
	},
}

var recipeDecisionListCmd = &cobra.Command{
	Use:   "list [recipe-id]",
	Short: "Show decisions and their current state",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, recipeID, err := resolveRootAndRecipe(args)
		if err != nil {
			return err
		}
		decisions, err := state.LoadDecisions(root, recipeID)
		if err != nil {
			return err
		}
		onlyOpen, _ := cmd.Flags().GetBool("open")
		if onlyOpen {
			var filtered []state.DecisionState
			for _, d := range decisions {
				if d.Status == state.DecisionOpen {
					filtered = append(filtered, d)
				}
			}
			decisions = filtered
		}

		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			if decisions == nil {
				decisions = []state.DecisionState{}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(decisions)
		}

		if len(decisions) == 0 {
			fmt.Println("No decisions recorded.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "KEY\tSTATUS\tDOC\tQUESTION")
		for _, d := range decisions {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", d.Key, d.Status, orDash(d.Doc), truncate(oneLine(d.Question), 60))
		}
		_ = w.Flush()
		for _, d := range decisions {
			if d.Status == state.DecisionResolved {
				fmt.Printf("\n%s → %s\n", d.Key, d.Answer)
			}
		}
		return nil
	},
}

var recipeDecisionResolveCmd = &cobra.Command{
	Use:   "resolve <recipe-id> <key>",
	Short: "Record the user's answer and unblock the recipe",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		answer, _ := cmd.Flags().GetString("answer")
		if err := state.ResolveDecision(root, args[0], args[1], answer); err != nil {
			return err
		}
		fmt.Printf("Decision %q resolved: %s\n", args[1], answer)

		remaining, err := state.OpenDecisions(root, args[0])
		if err == nil && len(remaining) > 0 {
			fmt.Printf("%d decision(s) still open — %s stays blocked.\n", len(remaining), args[0])
		}
		return nil
	},
}

var recipeDecisionDropCmd = &cobra.Command{
	Use:   "drop <recipe-id> <key>",
	Short: "Retire a question that no longer needs an answer",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		reason, _ := cmd.Flags().GetString("reason")
		if err := state.DropDecision(root, args[0], args[1], reason); err != nil {
			return err
		}
		fmt.Printf("Decision %q dropped: %s\n", args[1], reason)
		return nil
	},
}

// projectRoot resolves the jig project from the working directory.
func projectRoot() (string, error) {
	cwd, _ := os.Getwd()
	root, err := state.FindRoot(cwd)
	if err != nil {
		return "", fmt.Errorf("not a jig project: %w", err)
	}
	return root, nil
}

// resolveRootAndRecipe resolves the project root plus the target recipe,
// falling back to the active recipe when no id is given.
func resolveRootAndRecipe(args []string) (root, recipeID string, err error) {
	root, err = projectRoot()
	if err != nil {
		return "", "", err
	}
	recipeID, err = resolveRecipeID(args, root)
	if err != nil {
		return "", "", err
	}
	return root, recipeID, nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// oneLine flattens a question for table display. Truncation itself is
// recipe.go's truncate.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
