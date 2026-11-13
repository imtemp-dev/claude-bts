package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/review"
	"github.com/spf13/cobra"
)

// `bts review` keeps the record of a recipe's review page: the page the
// user reviews intent and scope on (/bts-review-page).  bts cannot read the
// page's data; the session exports it with ArtifactData and these commands
// record what it holds and what was done with it.

func init() {
	rootCmd.AddCommand(reviewCmd)
	reviewCmd.AddCommand(reviewPageCmd, reviewAttachCmd, reviewImportCmd, reviewDisposeCmd,
		reviewProposeCmd, reviewApplyCmd, reviewDoneCmd, reviewStatusCmd)

	reviewPageCmd.Flags().String("point", "", "Review point the page now carries: intent or scope")
	reviewPageCmd.Flags().Bool("json", false, "Emit the brief as JSON")
	reviewAttachCmd.Flags().String("url", "", "The page's claude.ai link")
	reviewAttachCmd.Flags().String("point", "", "Review point the published page carries: intent or scope")
	reviewImportCmd.Flags().String("dir", "", "ArtifactData out_dir holding the page's rounds/ and notes/ exports")
	reviewImportCmd.Flags().Int("round", 0, "Import only this round (default: every round still waiting)")
	reviewDisposeCmd.Flags().Int("round", 0, "Round the note belongs to")
	reviewDisposeCmd.Flags().String("note", "", "Note id")
	reviewDisposeCmd.Flags().String("as", "", "answered, page (the page's wording changed, not the plan) or declined")
	reviewDisposeCmd.Flags().String("why", "", "One line on what was done; required for declined")
	reviewProposeCmd.Flags().Int("round", 0, "Round the note belongs to")
	reviewProposeCmd.Flags().String("note", "", "Note id the proposal comes from")
	reviewProposeCmd.Flags().String("target", "", "Plan document it changes: intent.md or scope.md")
	reviewProposeCmd.Flags().String("kind", "edit", "edit, add, remove or move (a move draws the item in another lane)")
	reviewProposeCmd.Flags().String("summary", "", "What changes, in one sentence the user reads")
	reviewProposeCmd.Flags().String("before", "", "The text as it stands")
	reviewProposeCmd.Flags().String("after", "", "The text as it would read")
	reviewProposeCmd.Flags().String("why", "", "Why this change answers the note")
	reviewProposeCmd.Flags().String("to-lane", "", "move: the data-note label of the lane the item moves to")
	reviewProposeCmd.Flags().String("to-label", "", "move: the short name shown on the badge (e.g. PR 3)")
	reviewApplyCmd.Flags().Int("round", 0, "The apply round (the user pressed Update plan)")
	reviewDoneCmd.Flags().Int("round", 0, "Round to mark processed")
}

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Record a recipe's review page: rounds of notes, proposals, document versions",
	Long: `A review page is an artifact the user reviews intent and scope on: they pin
notes on what they read, send them in rounds, accept or reject the proposals
Claude makes, update the plan and confirm a fingerprinted version
(/bts-review-page).  These commands keep the record in the recipe:
review.json and feedback/round-N.json.`,
}

func intFlag(cmd *cobra.Command, name string) (int, error) {
	n, _ := cmd.Flags().GetInt(name)
	if n <= 0 {
		return 0, fmt.Errorf("--%s is required", name)
	}
	return n, nil
}

var reviewPageCmd = &cobra.Command{
	Use:          "page [recipe-id] --point intent|scope",
	Short:        "Print what the review page must show, with the kit, capabilities and document versions",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _, root, err := resolveRecipe(args)
		if err != nil {
			return err
		}
		set, err := engine.LoadSettings(root)
		if err != nil {
			return err
		}
		if set.Review.Surface == engine.ReviewSurfaceAsk {
			return errors.New("review.surface is \"ask\" in .bts/config/settings.yaml: review with AskUserQuestion instead")
		}
		point, _ := cmd.Flags().GetString("point")
		b, err := review.PageBrief(root, id, point)
		if err != nil {
			return err
		}
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			out, _ := json.MarshalIndent(b, "", "  ")
			fmt.Println(string(out))
			return nil
		}
		printBrief(b)
		return nil
	},
}

func printBrief(b *review.Brief) {
	fmt.Printf("[bts] review page for %s, point: %s\n", b.Recipe, b.Point)
	if b.URL != "" {
		fmt.Printf("Page: %s (republish to this URL)\n", b.URL)
	} else {
		fmt.Println("Page: not published yet")
	}
	fmt.Println("Documents (show these versions and fingerprints on the confirm rows):")
	for _, d := range b.Docs {
		fmt.Printf("  %s %s %s\n", d.File, d.Version, d.Hash)
	}
	for _, r := range b.Revisions {
		fmt.Printf("  recorded now: %s v%d (%s → %s), %s\n", r.File, r.Version, r.From, r.To, r.Note)
	}
	fmt.Println("Sections the page shows (earlier ones stay):")
	for _, s := range b.Sections {
		fmt.Printf("  - %s\n", s)
	}
	fmt.Printf("Kit v%s: publish these beside the page under their base names (Artifact files):\n", b.KitVersion)
	for name, path := range b.Kit {
		fmt.Printf("  %s  %s\n", name, path)
	}
	caps, _ := json.Marshal(b.Capabilities)
	fmt.Printf("Capabilities (pass exactly): %s\n", caps)
	cfg, _ := json.Marshal(b.Config)
	fmt.Printf("Config block (in the page, read by the kit):\n  <script type=\"application/json\" id=\"bts-review-config\">%s</script>\n", cfg)
	fmt.Printf("Next: build and publish the page (/bts-review-page), then `bts review attach %s --url <link> --point %s`.\n", b.Recipe, b.Point)
}

var reviewAttachCmd = &cobra.Command{
	Use:          "attach [recipe-id] --url <link> --point intent|scope",
	Short:        "Record the published review page's link and the point it carries",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _, root, err := resolveRecipe(args)
		if err != nil {
			return err
		}
		url, _ := cmd.Flags().GetString("url")
		point, _ := cmd.Flags().GetString("point")
		if point != "" && !containsStr(review.Points, point) {
			return fmt.Errorf("--point takes %s, got %q", strings.Join(review.Points, " or "), point)
		}
		s, err := review.Attach(root, id, url, point)
		if err != nil {
			return err
		}
		fmt.Printf("[bts] review page for %s: %s (points: %s)\n", id, s.URL, strings.Join(s.Points, ", "))
		return nil
	},
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

var reviewImportCmd = &cobra.Command{
	Use:          "import [recipe-id] --dir <out_dir> [--round N]",
	Short:        "Record the rounds a review page holds, from an ArtifactData export of rounds and notes",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _, root, err := resolveRecipe(args)
		if err != nil {
			return err
		}
		dir, _ := cmd.Flags().GetString("dir")
		if strings.TrimSpace(dir) == "" {
			return errors.New("--dir is required: the ArtifactData out_dir the page's rounds and notes were exported to")
		}
		only, _ := cmd.Flags().GetInt("round")
		rounds, err := review.Import(root, id, dir, only)
		if err != nil {
			return err
		}
		if len(rounds) == 0 {
			fmt.Println("[bts] no round is waiting on the page")
			return nil
		}
		for _, r := range rounds {
			label := r.Kind
			if r.Page != "" {
				label += ", " + r.Page
			}
			fmt.Printf("[bts] round %d (%s) recorded: %s\n", r.Round, label, review.RoundPath(root, id, r.Round))
			switch r.Kind {
			case review.KindApply:
				fmt.Printf("  accepted: %s; rejected: %s\n", joinOr(r.Accepted), joinOr(r.Rejected))
				fmt.Printf("  next: edit the plan files for the accepted proposals, then `bts review apply %s --round %d`\n", id, r.Round)
			default:
				for _, it := range r.Items {
					d := it.Disposition
					if d == "" {
						d = "open"
					}
					what := it.Kind
					switch it.Kind {
					case "decision":
						what = "pick " + it.Decision + " = " + it.Value
					case review.KindConfirm:
						what = "confirm " + it.Doc + " " + it.Value
					}
					fmt.Printf("  %s [%s] (%s) %s: %s\n", it.Note, d, what, it.Anchor, it.Text)
				}
				if r.Kind == review.KindConfirm {
					for _, it := range r.Items {
						if it.Kind == review.KindConfirm {
							fmt.Printf("  next: `bts recipe confirm %s %s --via review --hash %s --answer \"<the user's words>\"`; then `bts review done %s --round %d`\n", id, it.Doc, it.Value, id, r.Round)
						}
					}
					continue
				}
				fmt.Printf("  next: per note, `bts review dispose` (answered, page, declined) or `bts review propose`; then `bts review done %s --round %d`\n", id, r.Round)
			}
		}
		return nil
	},
}

func joinOr(list []string) string {
	if len(list) == 0 {
		return "none"
	}
	return strings.Join(list, ", ")
}

var reviewDisposeCmd = &cobra.Command{
	Use:          "dispose [recipe-id] --round N --note <id> --as answered|page|declined",
	Short:        "Record what was done with a note that changes no plan document",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _, root, err := resolveRecipe(args)
		if err != nil {
			return err
		}
		n, err := intFlag(cmd, "round")
		if err != nil {
			return err
		}
		note, _ := cmd.Flags().GetString("note")
		as, _ := cmd.Flags().GetString("as")
		why, _ := cmd.Flags().GetString("why")
		if err := review.Dispose(root, id, n, note, as, why); err != nil {
			return err
		}
		fmt.Printf("[bts] round %d note %s: %s\n", n, note, as)
		return nil
	},
}

var reviewProposeCmd = &cobra.Command{
	Use:          "propose [recipe-id] --round N --note <id> --target <doc> --summary <text>",
	Short:        "Record a proposed change to a plan document, made from a note",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _, root, err := resolveRecipe(args)
		if err != nil {
			return err
		}
		n, err := intFlag(cmd, "round")
		if err != nil {
			return err
		}
		get := func(k string) string { v, _ := cmd.Flags().GetString(k); return strings.TrimSpace(v) }
		p, path, err := review.Propose(root, id, n, review.Proposal{
			Note: get("note"), Target: get("target"), Kind: get("kind"), Summary: get("summary"),
			Before: get("before"), After: get("after"), Why: get("why"), ToLane: get("to-lane"), ToLabel: get("to-label"),
		})
		if err != nil {
			return err
		}
		fmt.Printf("[bts] proposal %s on %s recorded\n", p.ID, p.Target)
		fmt.Printf("  upload: ArtifactData set, collection proposals, doc_id %s, file_path %s\n", p.ID, path)
		return nil
	},
}

var reviewApplyCmd = &cobra.Command{
	Use:          "apply [recipe-id] --round N",
	Short:        "Record the new document versions an apply round produced, once the files are edited",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _, root, err := resolveRecipe(args)
		if err != nil {
			return err
		}
		n, err := intFlag(cmd, "round")
		if err != nil {
			return err
		}
		revs, uploads, err := review.Apply(root, id, n)
		if err != nil {
			return err
		}
		for _, r := range revs {
			fmt.Printf("[bts] %s v%d: %s → %s (from %s)\n", r.File, r.Version, r.From, r.To, strings.Join(r.Proposals, ", "))
		}
		if len(revs) == 0 {
			fmt.Println("[bts] nothing accepted: no document changed")
		}
		for _, u := range uploads {
			fmt.Printf("  upload: ArtifactData update, collection proposals, file_path %s\n", u)
		}
		fmt.Printf("  next: show the new versions on the page (`bts review page %s --point <point>` prints them), republish, then `bts review done %s --round %d`\n", id, id, n)
		return nil
	},
}

var reviewDoneCmd = &cobra.Command{
	Use:          "done [recipe-id] --round N",
	Short:        "Mark a round processed once every note has a disposition",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _, root, err := resolveRecipe(args)
		if err != nil {
			return err
		}
		n, err := intFlag(cmd, "round")
		if err != nil {
			return err
		}
		path, err := review.Done(root, id, n)
		if err != nil {
			return err
		}
		fmt.Printf("[bts] round %d processed\n", n)
		fmt.Printf("  upload: ArtifactData update, collection rounds, doc_id r-%d, file_path %s\n", n, path)
		return nil
	},
}

var reviewStatusCmd = &cobra.Command{
	Use:          "status [recipe-id]",
	Short:        "Show the review page, document versions, rounds and what is still open",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _, root, err := resolveRecipe(args)
		if err != nil {
			return err
		}
		s, err := review.Load(root, id)
		if err != nil {
			return err
		}
		url := s.URL
		if url == "" {
			url = "not published"
		}
		fmt.Printf("Review page: %s (kit v%s, points: %s)\n", url, s.Kit, joinOr(s.Points))
		for _, doc := range review.PlanDocs {
			if d := s.Docs[doc]; d != nil {
				cur, _ := review.FileFingerprint(root, id, doc)
				drift := ""
				if cur != d.Hash {
					drift = fmt.Sprintf(" (file now %s: run `bts review page`)", cur)
				}
				fmt.Printf("  %s v%d %s%s\n", doc, d.Version, d.Hash, drift)
			}
		}
		rounds, err := review.Rounds(root, id)
		if err != nil {
			return err
		}
		for _, r := range rounds {
			st := "processed"
			if r.Processed == "" {
				st = "open"
			}
			fmt.Printf("  round %d %s, %s: %d notes, %d proposals, %d revisions\n", r.Round, r.Kind, st, len(r.Items), len(r.Proposals), len(r.Revisions))
		}
		open, err := review.OpenWork(root, id, "")
		if err != nil {
			return err
		}
		for _, o := range open {
			fmt.Printf("  open: %s\n", o)
		}
		return nil
	},
}
