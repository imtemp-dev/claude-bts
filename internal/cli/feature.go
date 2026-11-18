package cli

import (
	"encoding/json"
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

var featureCmd = &cobra.Command{
	Use:   "feature",
	Short: "Plan a feature as several items, each worked as a recipe in its own worktree",
	Long: `A feature sits between the project and its recipes.  It lives in a store outside every
worktree ($BTS_FEATURE_STORE, else ~/.bts/features), found by id from anywhere.

The manager writes the plan and the decisions (create, item, confirm, close);
a child takes an item with "bts recipe create --feature <f> --item <i>" and its
recipe is synced into the store on every phase change.  Item progress is derived
from what the children wrote.`,
	GroupID: "recipe",
}

func init() {
	rootCmd.AddCommand(featureCmd)

	featureCreateCmd.Flags().String("title", "", "What the feature delivers")
	featureCreateCmd.Flags().String("ticket", "", "A ticket id (e.g. CU-…); a second create with the same ticket returns the open feature")
	featureCreateCmd.Flags().String("repo", "", "The repository's remote URL, when not run inside it")
	featureCreateCmd.Flags().String("base", "", "Base branch (default: origin/HEAD, else main)")
	featureCreateCmd.Flags().String("setup", "", "Command that makes a fresh checkout runnable (a dependency install); every item's copies run it")
	featureListCmd.Flags().Bool("json", false, "JSON output")
	featureStatusCmd.Flags().Bool("json", false, "JSON output (derived states, for review pages)")
	featureNextCmd.Flags().Bool("json", false, "JSON output")
	featureConfirmCmd.Flags().String("answer", "", "The user's words confirming the intent, contracts and plan")
	featureConfirmCmd.Flags().String("via", "", "review: the user confirmed on the feature's review page; needs --hash")
	featureConfirmCmd.Flags().String("hash", "", "With --via review: the fingerprint on the confirm row the user pressed")
	featureCloseCmd.Flags().String("answer", "", "The user's words")
	featureCancelCmd.Flags().String("answer", "", "The user's words")
	featureContextCmd.Flags().String("recipe", "", "Also write the context into this recipe's directory (default: the active recipe bound to the item)")

	for _, c := range []*cobra.Command{featureItemAddCmd, featureItemEditCmd} {
		c.Flags().String("title", "", "Item title (becomes the recipe topic)")
		c.Flags().String("type", "", "Recipe type: "+strings.Join(feature.ItemTypes, ", "))
		c.Flags().Bool("express", false, "Express mode (build only)")
		c.Flags().StringSlice("depends-on", nil, "Items this one needs done first (comma-separated)")
	}
	for _, c := range []*cobra.Command{featureItemSkipCmd, featureItemDeferCmd, featureItemReleaseCmd, featureItemRestoreCmd} {
		c.Flags().String("answer", "", "The user's words")
	}
	featureItemCmd.AddCommand(featureItemAddCmd, featureItemEditCmd, featureItemRemoveCmd, featureItemSkipCmd, featureItemDeferCmd, featureItemReleaseCmd, featureItemRestoreCmd)
	featureCmd.AddCommand(featureCreateCmd, featureListCmd, featureStatusCmd, featurePathCmd, featureItemCmd,
		featureConfirmCmd, featureNextCmd, featureContextCmd, featureHarvestCmd, featureReconcileCmd,
		featureCloseCmd, featureCancelCmd, featureSetupCmd)
	featureSetupCmd.Flags().Bool("clear", false, "Remove the feature's setup")
}

func printJSON(v interface{}) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// cwdRoot is the bts project of the working directory, or "".
func cwdRoot() string {
	cwd, _ := os.Getwd()
	root, err := state.FindRoot(cwd)
	if err != nil {
		return ""
	}
	return root
}

// repoForCreate finds the repository a new feature belongs to.
func repoForCreate(flag string) (*feature.Repo, error) {
	if flag != "" {
		key := feature.RepoKey(flag)
		if key == "" {
			key = strings.ToLower(strings.Trim(flag, "/"))
		}
		if !feature.ValidKey(key) {
			return nil, fmt.Errorf("--repo %q is neither a remote URL nor a host/owner/name key", flag)
		}
		return &feature.Repo{URL: flag, Key: key}, nil
	}
	cwd, _ := os.Getwd()
	for _, d := range []string{cwdRoot(), cwd} {
		if d == "" {
			continue
		}
		if r, err := feature.RepoAt(d); err == nil {
			return r, nil
		}
	}
	return nil, fmt.Errorf("not inside a git repository: run in the repository or pass --repo <remote URL>")
}

var featureCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a draft feature and print its id",
	RunE: func(cmd *cobra.Command, args []string) error {
		title, _ := cmd.Flags().GetString("title")
		ticket, _ := cmd.Flags().GetString("ticket")
		repoFlag, _ := cmd.Flags().GetString("repo")
		base, _ := cmd.Flags().GetString("base")
		setup, _ := cmd.Flags().GetString("setup")
		repo, err := repoForCreate(repoFlag)
		if err != nil {
			return err
		}
		if base == "" {
			cwd, _ := os.Getwd()
			base = feature.DefaultBase(cwd)
		}
		dir, f, existed, err := feature.Create(feature.CreateOptions{Title: title, Ticket: ticket, Repo: repo, Base: base, Setup: setup})
		if err != nil {
			return err
		}
		if existed {
			fmt.Fprintf(os.Stderr, "[bts] ticket %s already has feature %s; returning it\n", ticket, f.ID)
		} else {
			fmt.Fprintf(os.Stderr, "[bts] feature %s in %s: write %s and %s there, add items, then confirm with the user\n", f.ID, dir, feature.IntentFile, feature.ContractsFile)
		}
		fmt.Println(f.ID)
		return nil
	},
}

var featureListCmd = &cobra.Command{
	Use:   "list",
	Short: "List features in the store",
	RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		dirs, err := feature.All()
		if err != nil {
			return err
		}
		var all []*feature.Status
		for _, d := range dirs {
			f, err := feature.Load(d)
			if err != nil {
				continue
			}
			s, err := feature.Derive(d, f)
			if err != nil {
				continue
			}
			all = append(all, s)
		}
		if asJSON {
			return printJSON(all)
		}
		if len(all) == 0 {
			fmt.Println("No features.")
			return nil
		}
		for _, s := range all {
			done := 0
			for _, it := range s.Items {
				if it.State == feature.StateDone || it.State == feature.StateSkipped {
					done++
				}
			}
			fmt.Printf("%-28s %-17s %d/%d items  %s  (%s)\n", s.ID, s.State, done, len(s.Items), s.Title, s.Repo.Key)
		}
		return nil
	},
}

// featureFromArgs resolves the feature named in args[0], or the one the
// working directory's recipe is bound to.
func featureFromArgs(args []string) (string, *feature.Feature, error) {
	id := ""
	if len(args) > 0 {
		id = args[0]
	} else if root := cwdRoot(); root != "" {
		if r, _ := state.GetActiveRecipe(root); r != nil && r.Feature != "" {
			id = r.Feature
		}
	}
	if id == "" {
		return "", nil, fmt.Errorf("name a feature (bts feature list shows them)")
	}
	dir, err := feature.Find(id)
	if err != nil {
		return "", nil, err
	}
	f, err := feature.Load(dir)
	return dir, f, err
}

var featureStatusCmd = &cobra.Command{
	Use:   "status [feature]",
	Short: "Show a feature's derived state: items, stages, attempts",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		dir, f, err := featureFromArgs(args)
		if err != nil {
			return err
		}
		s, err := feature.Derive(dir, f)
		if err != nil {
			return err
		}
		if asJSON {
			return printJSON(s)
		}
		fmt.Printf("%s: %s\n", s.ID, s.Title)
		fmt.Printf("  State: %s   Confirmation: %d   Base: %s   Repo: %s\n", s.State, s.Confirmation, s.BaseBranch, s.Repo.Key)
		if len(s.Pending) > 0 {
			fmt.Printf("  Pending (not confirmed): %s\n", strings.Join(s.Pending, ", "))
		}
		for _, it := range s.Items {
			mark := " "
			if !it.InConfirmed {
				mark = "*"
			}
			line := fmt.Sprintf("  %s%-4s stage %d  %-10s %-9s %s", mark, it.ID, it.Stage, it.State, it.Type, it.Title)
			if len(it.DependsOn) > 0 {
				line += "  ← " + strings.Join(it.DependsOn, ",")
			}
			fmt.Println(line)
			if l := it.Latest; l != nil {
				detail := fmt.Sprintf("        a%d %s", l.N, l.RecipeID)
				if l.Phase != "" {
					detail += " phase " + l.Phase
				}
				if l.Released {
					detail += " (released)"
				}
				if l.Worktree != "" {
					detail += "  in " + l.Worktree
				}
				fmt.Println(detail)
			}
			if p := it.PR; p != nil {
				fmt.Printf("        draft %s (%s → %s), opened by a%d\n", p.URL, p.Branch, p.Base, p.Attempt)
			}
			if it.Note != "" {
				fmt.Printf("        note: %s\n", it.Note)
			}
		}
		if root := cwdRoot(); root != "" {
			recipes, _ := state.ListRecipes(root)
			for _, r := range recipes {
				if r.Feature == f.ID && feature.Behind(root, r) {
					fmt.Printf("  This worktree's %s (%s) is not synced to the store yet: bts feature harvest\n", r.ID, r.Phase)
				}
			}
		}
		fmt.Println("  (* not in the latest confirmation)")
		return nil
	},
}

var featurePathCmd = &cobra.Command{
	Use:   "path <feature>",
	Short: "Print a feature's directory in the store",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := feature.Find(args[0])
		if err != nil {
			return err
		}
		fmt.Println(dir)
		return nil
	},
}

var featureSetupCmd = &cobra.Command{
	Use:   "setup <feature> [command]",
	Short: "Print or set the command that makes a fresh checkout of the feature's repository runnable",
	Long: `The setup is a dependency install, such as "cd agent_server && uv sync -q --extra dev".
An item's worktree starts from a fresh checkout, and bts runs the setup in every copy it makes
for that item (adversary copies, interrogate reruns, mutations, pins) unless the project's
implement.setup_cmd or a --setup flag says otherwise.  With no command it prints the setup.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := feature.Find(args[0])
		if err != nil {
			return err
		}
		clear, _ := cmd.Flags().GetBool("clear")
		if len(args) == 1 && !clear {
			f, err := feature.Load(dir)
			if err != nil {
				return err
			}
			fmt.Println(f.Setup)
			return nil
		}
		setup := ""
		if len(args) == 2 {
			if clear {
				return fmt.Errorf("give a command or --clear, not both")
			}
			if setup = strings.TrimSpace(args[1]); setup == "" {
				return fmt.Errorf("the setup command is empty; use --clear to remove it")
			}
		}
		return feature.SetSetup(dir, setup)
	},
}

var featureItemCmd = &cobra.Command{
	Use:   "item",
	Short: "Change a feature's plan (manager)",
}

func itemSpec(cmd *cobra.Command) feature.ItemSpec {
	var s feature.ItemSpec
	if cmd.Flags().Changed("title") {
		v, _ := cmd.Flags().GetString("title")
		s.Title = &v
	}
	if cmd.Flags().Changed("type") {
		v, _ := cmd.Flags().GetString("type")
		s.Type = &v
	}
	if cmd.Flags().Changed("express") {
		v, _ := cmd.Flags().GetBool("express")
		s.Express = &v
	}
	if cmd.Flags().Changed("depends-on") {
		v, _ := cmd.Flags().GetStringSlice("depends-on")
		s.DependsOn = &v
	}
	return s
}

func pendingNote(dir string) {
	if f, err := feature.Load(dir); err == nil && len(f.Confirmations) > 0 {
		if p := feature.Pending(dir, f); len(p) > 0 {
			fmt.Fprintf(os.Stderr, "[bts] the %s changed since confirmation %d; it takes effect when the user confirms again (bts feature confirm)\n", strings.Join(p, ", "), len(f.Confirmations))
		}
	}
}

var featureItemAddCmd = &cobra.Command{
	Use:   "add <feature>",
	Short: "Add an item and print its id",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := feature.Find(args[0])
		if err != nil {
			return err
		}
		id, err := feature.AddItem(dir, itemSpec(cmd))
		if err != nil {
			return err
		}
		pendingNote(dir)
		fmt.Println(id)
		return nil
	},
}

var featureItemEditCmd = &cobra.Command{
	Use:   "edit <feature> <item>",
	Short: "Change an item's title, type, mode or dependencies",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := feature.Find(args[0])
		if err != nil {
			return err
		}
		if err := feature.EditItem(dir, args[1], itemSpec(cmd)); err != nil {
			return err
		}
		pendingNote(dir)
		return nil
	},
}

var featureItemRemoveCmd = &cobra.Command{
	Use:   "remove <feature> <item>",
	Short: "Remove an item nobody has started and nothing depends on",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := feature.Find(args[0])
		if err != nil {
			return err
		}
		if err := feature.RemoveItem(dir, args[1]); err != nil {
			return err
		}
		pendingNote(dir)
		return nil
	},
}

func decideCmd(kind, short string) *cobra.Command {
	return &cobra.Command{
		Use:   kind + " <feature> <item>",
		Short: short,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := feature.Find(args[0])
			if err != nil {
				return err
			}
			answer, _ := cmd.Flags().GetString("answer")
			return feature.DecideItem(dir, args[1], kind, answer)
		},
	}
}

var featureItemSkipCmd = decideCmd("skip", "Record the user's decision that an item is not needed")
var featureItemDeferCmd = decideCmd("defer", "Record the user's decision to do an item later, outside this feature")

var featureItemRestoreCmd = &cobra.Command{
	Use:   "restore <feature> <item>",
	Short: "Undo a skip or defer (the user's decision)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := feature.Find(args[0])
		if err != nil {
			return err
		}
		answer, _ := cmd.Flags().GetString("answer")
		return feature.RestoreItem(dir, args[1], answer)
	},
}

var featureItemReleaseCmd = &cobra.Command{
	Use:   "release <feature> <item>",
	Short: "Free an item's open attempt whose worktree or run is gone, so it can be claimed again",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := feature.Find(args[0])
		if err != nil {
			return err
		}
		answer, _ := cmd.Flags().GetString("answer")
		n, err := feature.ReleaseItem(dir, args[1], answer)
		if err != nil {
			return err
		}
		fmt.Printf("Released %s a%d.\n", args[1], n)
		return nil
	},
}

var featureConfirmCmd = &cobra.Command{
	Use:   "confirm <feature>",
	Short: "Record the user's confirmation of the intent, contracts and plan, and pin them",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := feature.Find(args[0])
		if err != nil {
			return err
		}
		answer, _ := cmd.Flags().GetString("answer")
		via, _ := cmd.Flags().GetString("via")
		hash, _ := cmd.Flags().GetString("hash")
		switch via {
		case "":
			if strings.TrimSpace(hash) != "" {
				return errors.New("--hash goes with --via review")
			}
		case "review":
			cmd.SilenceUsage = true
			if answer, err = featureReviewAnswer(args[0], hash, answer); err != nil {
				return err
			}
		default:
			return fmt.Errorf("--via takes review, got %q", via)
		}
		n, err := feature.Confirm(dir, answer)
		if err != nil {
			cmd.SilenceUsage = true
			return err
		}
		fmt.Printf("Confirmation %d recorded; %s/%s/%d holds what was confirmed.\n", n, dir, feature.ConfirmedDir, n)
		return nil
	},
}

// featureReviewAnswer checks a confirmation made on the feature's review
// page and returns the answer to record: the fingerprint must be the
// current one of the intent, contracts and plan together, and the page may
// hold no open notes or proposals.
func featureReviewAnswer(ref, hash, answer string) (string, error) {
	if strings.TrimSpace(answer) == "" {
		return "", errors.New("--answer is required: the user's words on the confirm row")
	}
	sub, err := featureSubject(ref)
	if err != nil {
		return "", err
	}
	if err := review.ConfirmCheck(sub, sub.Bundle, hash); err != nil {
		return "", err
	}
	s, err := review.Load(sub)
	if err != nil {
		return "", err
	}
	v := 0
	if d := s.Docs[sub.Bundle]; d != nil {
		v = d.Version
	}
	return fmt.Sprintf("Confirmed on the review page, feature v%d (%s): %s", v, strings.TrimSpace(hash), strings.TrimSpace(answer)), nil
}

var featureNextCmd = &cobra.Command{
	Use:   "next <feature>",
	Short: "List the confirmed items ready to start",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		dir, err := feature.Find(args[0])
		if err != nil {
			return err
		}
		f, err := feature.Load(dir)
		if err != nil {
			return err
		}
		ready, s, err := feature.Next(dir, f)
		if err != nil {
			return err
		}
		if asJSON {
			return printJSON(ready)
		}
		if s.Confirmation == 0 {
			fmt.Printf("%s is not confirmed yet.\n", f.ID)
			return nil
		}
		if len(s.Pending) > 0 {
			fmt.Fprintf(os.Stderr, "[bts] the %s changed since confirmation %d; only the confirmed plan is offered\n", strings.Join(s.Pending, ", "), s.Confirmation)
		}
		if len(ready) == 0 {
			fmt.Printf("Nothing ready (feature %s).\n", s.State)
			return nil
		}
		for _, it := range ready {
			fmt.Printf("%s  stage %d  %s  %s\n    start it in a worktree: bts recipe create --feature %s --item %s\n", it.ID, it.Stage, it.Type, it.Title, f.ID, it.ID)
		}
		return nil
	},
}

var featureContextCmd = &cobra.Command{
	Use:   "context <feature> <item>",
	Short: "Print what a child needs for an item: the confirmed intent and contracts, and earlier items' results",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		root := cwdRoot()
		wt := root
		if wt == "" {
			wt, _ = os.Getwd()
		}
		doc, served, err := feature.Context(args[0], args[1], wt)
		if err != nil {
			return err
		}
		fmt.Print(doc)
		recipeID, _ := cmd.Flags().GetString("recipe")
		if recipeID == "" && root != "" {
			if r, _ := state.GetActiveRecipe(root); r != nil && r.FeatureItem == args[1] && feature.SameID(args[0], r.Feature) {
				recipeID = r.ID
			}
		}
		if recipeID != "" && root != "" {
			rdir := state.RecipeDir(root, recipeID)
			if err := os.WriteFile(filepath.Join(rdir, "feature-context.md"), []byte(doc), 0644); err != nil {
				return err
			}
			if err := state.WriteJSON(filepath.Join(rdir, "feature-context.json"), served); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "[bts] context written to %s (feature-context.md, feature-context.json)\n", rdir)
		}
		return nil
	},
}

var featureHarvestCmd = &cobra.Command{
	Use:   "harvest [recipe-id]",
	Short: "Sync this worktree's feature-bound recipes into the store now (it also happens on every phase change)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root := cwdRoot()
		if root == "" {
			return fmt.Errorf("not a bts project")
		}
		if len(args) == 1 {
			r, err := state.LoadRecipeState(root, args[0])
			if err != nil {
				return err
			}
			if r.Feature == "" {
				return fmt.Errorf("%s is bound to no feature; nothing to harvest", args[0])
			}
			if err := feature.Sync(root, args[0]); err != nil {
				return err
			}
			fmt.Printf("Synced %s.\n", args[0])
			return nil
		}
		synced, err := feature.SyncPending(root)
		for _, id := range synced {
			fmt.Printf("Synced %s.\n", id)
		}
		if err != nil {
			return err
		}
		if len(synced) == 0 {
			fmt.Println("Nothing behind.")
		}
		return nil
	},
}

var featureReconcileCmd = &cobra.Command{
	Use:   "reconcile <feature>",
	Short: "Show what changed in each item since the manager last looked, and move the cursor",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := feature.Find(args[0])
		if err != nil {
			return err
		}
		changes, err := feature.Reconcile(dir)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			fmt.Println("No change since the last reconcile.")
		}
		for _, c := range changes {
			fmt.Printf("%s: %s\n", c.Item, c.Text)
		}
		return nil
	},
}

func closeCmd(kind, use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " <feature>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := feature.Find(args[0])
			if err != nil {
				return err
			}
			answer, _ := cmd.Flags().GetString("answer")
			return feature.Close(dir, kind, answer)
		},
	}
}

var featureCloseCmd = closeCmd("done", "close", "Close a feature whose items are all done, skipped or deferred (the user's decision)")
var featureCancelCmd = closeCmd("cancelled", "cancel", "Cancel a feature (the user's decision)")
