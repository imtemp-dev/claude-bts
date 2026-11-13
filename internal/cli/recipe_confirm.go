package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/review"
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
	recipeConfirmCmd.Flags().String("by", "user", "Who confirms: user (the default; --answer is their words); feature (intent of a feature item: the feature's confirmation is the user's, recorded with its number and words); manager (a feature item: the feature manager's words, within the feature's confirmation)")
	recipeConfirmCmd.Flags().String("via", "", "review: the user confirmed on the review page; needs --hash, the fingerprint they confirmed")
	recipeConfirmCmd.Flags().String("hash", "", "With --via review: the document fingerprint on the confirm row the user pressed")
}

// reviewConfirmAnswer checks a confirmation made on the review page and
// returns the answer to record: the fingerprint must be the document's
// current one and the page may hold no open work on it.
func reviewConfirmAnswer(root, recipeID, which, by, hash, answer string) (string, error) {
	if by != "" && by != "user" {
		return "", errors.New("--via review is the user's confirmation; it does not combine with --by " + by)
	}
	if strings.TrimSpace(answer) == "" {
		return "", errors.New("--answer is required: the user's words on the confirm row")
	}
	doc := which + ".md"
	if err := review.ConfirmCheck(root, recipeID, doc, hash); err != nil {
		return "", err
	}
	s, err := review.Load(root, recipeID)
	if err != nil {
		return "", err
	}
	v := 0
	if d := s.Docs[doc]; d != nil {
		v = d.Version
	}
	return fmt.Sprintf("Confirmed on the review page, %s v%d (%s): %s", doc, v, strings.TrimSpace(hash), strings.TrimSpace(answer)), nil
}

// confirmAnswer turns --by and --answer into the answer recorded, with where
// it came from.  Only the user's own words are recorded bare.
func confirmAnswer(root, recipeID, which, by, answer string) (string, error) {
	answer = strings.TrimSpace(answer)
	switch by {
	case "", "user":
		if answer == "" {
			return "", errors.New("--answer is required: the user's confirmation, in their words")
		}
		return answer, nil
	case "feature", "manager":
	default:
		return "", fmt.Errorf("--by takes user, feature or manager, got %q", by)
	}
	r, err := state.LoadRecipeState(root, recipeID)
	if err != nil {
		return "", err
	}
	if r.Feature == "" {
		return "", fmt.Errorf("--by %s is for a feature item's recipe; %s is bound to no feature", by, recipeID)
	}
	dir, err := feature.Find(r.Feature)
	if err != nil {
		return "", err
	}
	f, err := feature.Load(dir)
	if err != nil {
		return "", err
	}
	snap, err := feature.ReadConfirmed(dir, f)
	if err != nil {
		return "", err
	}
	if snap == nil {
		return "", fmt.Errorf("feature %s is not confirmed", f.ID)
	}
	inPlan := false
	for _, p := range snap.Plan {
		inPlan = inPlan || p.ID == r.FeatureItem
	}
	if !inPlan {
		return "", fmt.Errorf("item %s is not in confirmation %d of %s", r.FeatureItem, snap.N, f.ID)
	}
	c := f.Confirmations[len(f.Confirmations)-1]
	if by == "feature" {
		if which != "intent" {
			return "", errors.New("--by feature confirms intent only: the feature's confirmation covers the item, not the scope its recipe works out")
		}
		if answer != "" {
			return "", errors.New("--by feature records the feature's confirmation; drop --answer")
		}
		return fmt.Sprintf("Confirmed through feature %s, confirmation %d (%s), item %s: %s", f.ID, c.N, c.At, r.FeatureItem, c.Answer), nil
	}
	if answer == "" {
		return "", errors.New("--by manager needs --answer: the manager's words")
	}
	return fmt.Sprintf("Confirmed by the feature manager within feature %s, confirmation %d: %s", f.ID, c.N, answer), nil
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
		by, _ := cmd.Flags().GetString("by")
		via, _ := cmd.Flags().GetString("via")
		hash, _ := cmd.Flags().GetString("hash")
		which := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(args[1])), ".md")
		switch via {
		case "":
			if hash != "" {
				return errors.New("--hash goes with --via review")
			}
			answer, err = confirmAnswer(root, args[0], which, by, answer)
		case "review":
			if which != "intent" && which != "scope" {
				return fmt.Errorf("confirm takes intent or scope, got %q", which)
			}
			answer, err = reviewConfirmAnswer(root, args[0], which, by, hash, answer)
		default:
			return fmt.Errorf("--via takes review, got %q", via)
		}
		if err != nil {
			return err
		}
		key, err := executeRecipeConfirm(root, args[0], args[1], answer)
		if err != nil {
			return err
		}
		source := "the user's words are"
		if by == "feature" || by == "manager" {
			source = "its source (" + by + ") is recorded in"
		}
		fmt.Printf("[bts] %s.md CONFIRMED; %s decision %s\n", which, source, key)
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
