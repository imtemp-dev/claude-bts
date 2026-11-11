package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

// `bts recipe confirm` records the user's confirmation of intent.md or
// scope.md as a resolved decision and sets the document's status line.
// The skills used to say "update intent.md Status to CONFIRMED", so agents
// flipped the line by hand: `sed -i` was denied twice on true-shark and
// once on swift-eagle, and on neat-wolf it went through and a revised
// scope.md read CONFIRMED with nothing showing who confirmed it (trial
// problem #88).  Now the status changes only with the user's words beside
// it.

func init() {
	recipeCmd.AddCommand(recipeConfirmCmd)
	recipeConfirmCmd.Flags().String("answer", "", "The user's confirmation, in their words and in the project's record language")
	_ = recipeConfirmCmd.MarkFlagRequired("answer")
}

var recipeConfirmCmd = &cobra.Command{
	Use:          "confirm <recipe-id> <intent|scope>",
	Short:        "Record the user's confirmation of intent.md or scope.md and set its Status line to CONFIRMED",
	Args:         cobra.ExactArgs(2),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		answer, _ := cmd.Flags().GetString("answer")
		key, err := executeRecipeConfirm(root, args[0], args[1], answer)
		if err != nil {
			return err
		}
		which := strings.TrimSuffix(strings.ToLower(args[1]), ".md")
		fmt.Printf("[bts] %s.md CONFIRMED; the user's words are decision %s\n", which, key)
		// A small scope on a full build recipe is the first point where
		// express can be weighed (feature #132).
		if which == "scope" {
			q, err := suggestExpress(root, args[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "[bts] warning: the express question was not held: %v\n", err)
			} else if q != "" {
				printModeQuestion(args[0], expressModeKey, q)
			}
		}
		return nil
	},
}

// executeRecipeConfirm returns the decision key it recorded.  A document
// confirmed again after a revision gets a new key (scope-confirm-2), since
// a resolved decision is history and is never reopened.
func executeRecipeConfirm(root, recipeID, which, answer string) (string, error) {
	which = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(which)), ".md")
	if which != "intent" && which != "scope" {
		return "", fmt.Errorf("confirm takes intent or scope, got %q", which)
	}
	if strings.TrimSpace(answer) == "" {
		return "", errors.New("--answer is required: the user's confirmation, in their words")
	}
	path := filepath.Join(state.RecipeDir(root, recipeID), which+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	decisions, err := state.LoadDecisions(root, recipeID)
	if err != nil {
		return "", err
	}
	base := which + "-confirm"
	// A confirmation already held open (the user answers later, in
	// another session) is the one this answer resolves.
	key := ""
	for _, d := range decisions {
		if d.Status == state.DecisionOpen && (d.Key == base || strings.HasPrefix(d.Key, base+"-")) {
			key = d.Key
			break
		}
	}
	if key == "" {
		key = base
		for n := 2; ; n++ {
			taken := false
			for _, d := range decisions {
				if d.Key == key {
					taken = true
					break
				}
			}
			if !taken {
				break
			}
			key = fmt.Sprintf("%s-%d", base, n)
		}
		if _, err := state.HoldDecision(root, recipeID, &state.DecisionEvent{
			Key: key, Doc: which + ".md", Question: fmt.Sprintf("Confirm %s.md as written?", which),
		}); err != nil {
			return "", err
		}
	}
	if err := state.ResolveDecision(root, recipeID, key, strings.TrimSpace(answer)); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(state.SetDocStatus(string(data), "CONFIRMED")), 0644); err != nil {
		return "", err
	}
	_ = state.AppendChangelog(root, recipeID, &state.ChangelogEntry{
		Action: "confirm",
		Output: which + ".md",
		Result: fmt.Sprintf("CONFIRMED (%s): %s", key, state.TruncateRunes(strings.TrimSpace(answer), 200)),
	})
	return key, nil
}
