package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/feature"
	"github.com/imtemp-dev/claude-bts/internal/orca"
	"github.com/imtemp-dev/claude-bts/internal/review"
	"github.com/spf13/cobra"
)

// `bts review` keeps the record of a review page: the page the user
// reviews a recipe's intent, scope and spec on, or a feature's intent,
// contracts and plan (/bts-review-page).  bts cannot read the page's data;
// the session exports it with ArtifactData and these commands record what
// it holds and what was done with it.

func init() {
	rootCmd.AddCommand(reviewCmd)
	reviewCmd.AddCommand(reviewPageCmd, reviewAttachCmd, reviewImportCmd, reviewDisposeCmd,
		reviewProposeCmd, reviewApplyCmd, reviewDoneCmd, reviewStatusCmd)

	reviewCmd.PersistentFlags().String("feature", "", "Review a feature's intent, contracts and plan instead of a recipe (run inside the session's bts project)")
	reviewPageCmd.Flags().String("point", "", "Review point the page now carries: intent, scope or spec (final.md, or a fix recipe's fix-spec.md); a feature's page carries feature")
	reviewPageCmd.Flags().Bool("json", false, "Emit the brief as JSON")
	reviewAttachCmd.Flags().String("url", "", "The page's claude.ai link")
	reviewAttachCmd.Flags().String("point", "", "Review point the published page carries: intent, scope, spec, or feature")
	reviewImportCmd.Flags().String("dir", "", "ArtifactData out_dir holding the page's rounds/ and notes/ exports")
	reviewImportCmd.Flags().Int("round", 0, "Import only this round (default: every round still waiting)")
	reviewDisposeCmd.Flags().Int("round", 0, "Round the note belongs to")
	reviewDisposeCmd.Flags().String("note", "", "Note id")
	reviewDisposeCmd.Flags().String("as", "", "answered, page (the page's wording changed, not the plan) or declined")
	reviewDisposeCmd.Flags().String("why", "", "One line on what was done; required for declined")
	reviewDisposeCmd.Flags().String("reply", "", "The answer shown on the note's pin, one or two sentences in the user's language")
	reviewProposeCmd.Flags().String("reply", "", "What the pin says about the proposal, in the user's language")
	reviewDoneCmd.Flags().String("summary", "", "One line under the round on the page, in the user's language")
	reviewProposeCmd.Flags().Int("round", 0, "Round the note belongs to")
	reviewProposeCmd.Flags().String("note", "", "Note id the proposal comes from")
	reviewProposeCmd.Flags().String("target", "", "Plan document it changes: intent.md, scope.md or the spec; for a feature intent.md, contracts.md or plan")
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
	Short: "Record a review page: rounds of notes, proposals, document versions",
	Long: `A review page is an artifact the user reviews a recipe's intent, scope and
spec (final.md, or a fix recipe's fix-spec.md) on, or with --feature a
feature's intent, contracts and plan: they pin notes on what they read, send
them in rounds, accept or reject the proposals Claude makes, update the plan
and confirm a fingerprinted version (/bts-review-page).  These commands keep
the record: review.json and feedback/round-N.json, in the recipe's directory
or the feature's.`,
}

// resolveSubject is the review a command works on: the feature --feature
// names, else the recipe (the argument, or the active recipe).
func resolveSubject(cmd *cobra.Command, args []string) (review.Subject, error) {
	if ref, _ := cmd.Flags().GetString("feature"); strings.TrimSpace(ref) != "" {
		if len(args) > 0 {
			return review.Subject{}, errors.New("--feature names the review; drop the recipe id")
		}
		return featureSubject(strings.TrimSpace(ref))
	}
	id, _, root, err := resolveRecipe(args)
	if err != nil {
		return review.Subject{}, err
	}
	return review.RecipeSubject(root, id), nil
}

// featureSubject is a feature's review, run from the bts project the
// session works in: the page, its uploads and the review kit live there,
// the record beside the feature's documents in the store.
func featureSubject(ref string) (review.Subject, error) {
	dir, err := feature.Find(ref)
	if err != nil {
		return review.Subject{}, err
	}
	root := cwdRoot()
	if root == "" {
		return review.Subject{}, errors.New("run this inside the bts project the session works in: a feature's review page and the review kit live there")
	}
	return feature.ReviewSubject(root, dir)
}

func intFlag(cmd *cobra.Command, name string) (int, error) {
	n, _ := cmd.Flags().GetInt(name)
	if n <= 0 {
		return 0, fmt.Errorf("--%s is required", name)
	}
	return n, nil
}

var reviewPageCmd = &cobra.Command{
	Use:          "page [recipe-id] --point intent|scope|spec  |  page --feature <feature>",
	Short:        "Print what the review page must show, with the kit, capabilities and document versions",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		sub, err := resolveSubject(cmd, args)
		if err != nil {
			return err
		}
		set, err := engine.LoadSettings(sub.Root)
		if err != nil {
			return err
		}
		if set.Review.Surface == engine.ReviewSurfaceAsk {
			return errors.New("review.surface is \"ask\" in .bts/config/settings.yaml: review with AskUserQuestion instead")
		}
		point, _ := cmd.Flags().GetString("point")
		b, err := review.PageBrief(sub, point)
		if err != nil {
			return err
		}
		local := set.Review.Surface == engine.ReviewSurfaceLocal
		if local {
			b.Capabilities = nil
		}
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			out, _ := json.MarshalIndent(b, "", "  ")
			fmt.Println(string(out))
			return nil
		}
		printBrief(b, local)
		return nil
	},
}

func printBrief(b *review.Brief, local bool) {
	fmt.Printf("[bts] review page for %s %s, point: %s\n", b.Kind, b.Recipe, b.Point)
	switch {
	case local && b.URL != "":
		fmt.Printf("Page: %s (served from this machine; rewrite page.html and it reloads)\n", b.URL)
	case local:
		fmt.Println("Page: not served yet (review.surface: local)")
	case b.URL != "":
		fmt.Printf("Page: %s (republish to this URL)\n", b.URL)
	default:
		fmt.Println("Page: not published yet")
	}
	fmt.Printf("Write the page to: %s\n", b.Page)
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
	cfg, _ := json.Marshal(b.Config)
	if local {
		fmt.Printf("Kit v%s: link it from the page by base name (<link rel=\"stylesheet\" href=\"bts-review-kit.css\">, <script src=\"bts-review-kit.js\"></script>); the server serves it\n", b.KitVersion)
		fmt.Printf("Config block (in the page, read by the kit; the server adds the local transport):\n  <script type=\"application/json\" id=\"bts-review-config\">%s</script>\n", cfg)
		fmt.Printf("Next: write the page, then `bts review open %s --point %s`.\n", b.Arg, b.Point)
		return
	}
	fmt.Printf("Kit v%s: publish these beside the page under their base names (Artifact files):\n", b.KitVersion)
	for name, path := range b.Kit {
		fmt.Printf("  %s  %s\n", name, path)
	}
	caps, _ := json.Marshal(b.Capabilities)
	fmt.Printf("Capabilities (pass exactly): %s\n", caps)
	fmt.Printf("Config block (in the page, read by the kit):\n  <script type=\"application/json\" id=\"bts-review-config\">%s</script>\n", cfg)
	fmt.Printf("Next: build and publish the page (/bts-review-page), then `bts review attach %s --url <link> --point %s`.\n", b.Arg, b.Point)
}

var reviewAttachCmd = &cobra.Command{
	Use:          "attach [recipe-id] --url <link> --point intent|scope|spec  |  attach --feature <feature> --url <link>",
	Short:        "Record the published review page's link and the point it carries",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		sub, err := resolveSubject(cmd, args)
		if err != nil {
			return err
		}
		url, _ := cmd.Flags().GetString("url")
		point, _ := cmd.Flags().GetString("point")
		if point == "" && sub.Kind == review.KindFeature {
			point = review.FeaturePoint
		}
		if point != "" && !containsStr(sub.Points(), point) {
			return fmt.Errorf("--point takes %s, got %q", strings.Join(sub.Points(), " or "), point)
		}
		if set, err := engine.LoadSettings(sub.Root); err == nil && set.Review.Surface == engine.ReviewSurfaceLocal {
			p := point
			if p == "" {
				p = "<point>"
			}
			open := sub.Arg() + " --point " + p
			if sub.Kind == review.KindFeature {
				open = sub.Arg()
			}
			return fmt.Errorf("review.surface is local in .bts/config/settings.yaml: the review page is served by bts, not attached as a claude.ai link; run `bts review open %s`", open)
		}
		prev := ""
		if old, err := review.Load(sub); err == nil {
			prev = old.URL
		}
		s, err := review.Attach(sub, url, point)
		if err != nil {
			return err
		}
		fmt.Printf("[bts] review page for %s %s: %s (points: %s)\n", sub.Kind, sub.ID, s.URL, strings.Join(s.Points, ", "))
		if s.URL != prev {
			openReviewInOrca(sub.Root, s.URL)
		}
		return nil
	},
}

// openReviewInOrca shows a newly attached review page in Orca's own
// browser, in a tab beside the session's terminal, when the session runs
// in Orca and the project has not turned orca.launch off.  Claude Code
// opens a published artifact in the system browser, away from the Orca
// window the user works in.  A failure is a notice, never an error: the
// link is printed above either way.
func openReviewInOrca(root, url string) {
	if !orca.InOrca() {
		return
	}
	openInOrcaTab(root, url, "[bts] opened in Orca's browser beside this terminal (sign in to claude.ai there once if it asks)")
}

// openInOrcaTab opens url in a tab of Orca's browser unless the project
// turned orca.launch off, printing done on success.  It reports whether
// the tab opened.
func openInOrcaTab(root, url, done string) bool {
	if s, err := engine.LoadSettings(root); err == nil && s.Orca.LaunchPolicy() == engine.OrcaLaunchOff {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := orca.OpenTab(ctx, root, url); err != nil {
		fmt.Fprintf(os.Stderr, "[bts] could not open the page in Orca's browser: %v\n", err)
		return false
	}
	fmt.Println(done)
	return true
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// confirmNext is the command that records a confirmation the user made on
// the page.
func confirmNext(sub review.Subject, it review.Item) string {
	if sub.Kind == review.KindFeature {
		return fmt.Sprintf("bts feature confirm %s --via review --hash %s --answer \"<the user's words>\"", sub.ID, it.Value)
	}
	return fmt.Sprintf("bts recipe confirm %s %s --via review --hash %s --answer \"<the user's words>\"", sub.ID, it.Doc, it.Value)
}

var reviewImportCmd = &cobra.Command{
	Use:          "import [recipe-id] --dir <out_dir> [--round N]",
	Short:        "Record the rounds a review page holds, from an ArtifactData export of rounds and notes",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		sub, err := resolveSubject(cmd, args)
		if err != nil {
			return err
		}
		// A local page needs no --dir: Import reads its data directly.
		dir, _ := cmd.Flags().GetString("dir")
		only, _ := cmd.Flags().GetInt("round")
		rounds, err := review.Import(sub, dir, only)
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
			fmt.Printf("[bts] round %d (%s) recorded: %s\n", r.Round, label, review.RoundPath(sub, r.Round))
			switch r.Kind {
			case review.KindApply:
				fmt.Printf("  accepted: %s; rejected: %s\n", joinOr(r.Accepted), joinOr(r.Rejected))
				fmt.Printf("  next: edit the plan for the accepted proposals, then `bts review apply %s --round %d`\n", sub.Arg(), r.Round)
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
							fmt.Printf("  next: `%s`; then `bts review done %s --round %d`\n", confirmNext(sub, it), sub.Arg(), r.Round)
						}
					}
					continue
				}
				fmt.Printf("  next: per note, `bts review dispose` (answered, page, declined) or `bts review propose`; then `bts review done %s --round %d`\n", sub.Arg(), r.Round)
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
		sub, err := resolveSubject(cmd, args)
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
		reply, _ := cmd.Flags().GetString("reply")
		if err := review.Dispose(sub, n, note, as, why); err != nil {
			return err
		}
		fmt.Printf("[bts] round %d note %s: %s\n", n, note, as)
		if strings.TrimSpace(reply) != "" || review.IsLocal(sub) {
			if strings.TrimSpace(reply) == "" && as == review.Declined {
				reply = why
			}
			path, err := review.ReplyNote(sub, note, as, reply)
			if err != nil {
				return err
			}
			printNoteWrite(sub, note, path)
		}
		return nil
	},
}

var reviewProposeCmd = &cobra.Command{
	Use:          "propose [recipe-id] --round N --note <id> --target <doc> --summary <text>",
	Short:        "Record a proposed change to a plan document, made from a note",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		sub, err := resolveSubject(cmd, args)
		if err != nil {
			return err
		}
		n, err := intFlag(cmd, "round")
		if err != nil {
			return err
		}
		get := func(k string) string { v, _ := cmd.Flags().GetString(k); return strings.TrimSpace(v) }
		p, path, err := review.Propose(sub, n, review.Proposal{
			Note: get("note"), Target: get("target"), Kind: get("kind"), Summary: get("summary"),
			Before: get("before"), After: get("after"), Why: get("why"), ToLane: get("to-lane"), ToLabel: get("to-label"),
		})
		if err != nil {
			return err
		}
		fmt.Printf("[bts] proposal %s on %s recorded\n", p.ID, p.Target)
		if review.IsLocal(sub) {
			fmt.Printf("  on the page: proposal %s, waiting for Accept or Reject\n", p.ID)
		} else {
			fmt.Printf("  upload: ArtifactData set, collection proposals, doc_id %s, file_path %s\n", p.ID, path)
		}
		if reply := get("reply"); reply != "" || review.IsLocal(sub) {
			npath, err := review.ReplyNote(sub, p.Note, review.Proposed, reply)
			if err != nil {
				return err
			}
			printNoteWrite(sub, p.Note, npath)
		}
		return nil
	},
}

var reviewApplyCmd = &cobra.Command{
	Use:          "apply [recipe-id] --round N",
	Short:        "Record the new document versions an apply round produced, once the plan is edited",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		sub, err := resolveSubject(cmd, args)
		if err != nil {
			return err
		}
		n, err := intFlag(cmd, "round")
		if err != nil {
			return err
		}
		revs, uploads, err := review.Apply(sub, n)
		if err != nil {
			return err
		}
		for _, r := range revs {
			fmt.Printf("[bts] %s v%d: %s → %s (from %s)\n", r.File, r.Version, r.From, r.To, strings.Join(r.Proposals, ", "))
		}
		if len(revs) == 0 {
			fmt.Println("[bts] nothing accepted: no document changed")
		}
		local := review.IsLocal(sub)
		for _, u := range uploads {
			if local {
				fmt.Printf("  on the page: %s\n", strings.TrimSuffix(filepath.Base(u), ".json"))
			} else {
				fmt.Printf("  upload: ArtifactData update, collection proposals, file_path %s\n", u)
			}
		}
		point := "<point>"
		if sub.Kind == review.KindFeature {
			point = review.FeaturePoint
		}
		again := "republish"
		if local {
			again = "rewrite page.html"
		}
		fmt.Printf("  next: show the new versions on the page (`bts review page %s --point %s` prints them), %s, then `bts review done %s --round %d`\n", sub.Arg(), point, again, sub.Arg(), n)
		return nil
	},
}

var reviewDoneCmd = &cobra.Command{
	Use:          "done [recipe-id] --round N",
	Short:        "Mark a round processed once every note has a disposition",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		sub, err := resolveSubject(cmd, args)
		if err != nil {
			return err
		}
		n, err := intFlag(cmd, "round")
		if err != nil {
			return err
		}
		summary, _ := cmd.Flags().GetString("summary")
		path, err := review.DoneWith(sub, n, summary)
		if err != nil {
			return err
		}
		fmt.Printf("[bts] round %d processed\n", n)
		if review.IsLocal(sub) {
			fmt.Printf("  on the page: round %d marked answered\n", n)
			fmt.Printf("  next: run `bts review wait %s` in the background again for the next round\n", sub.Arg())
		} else {
			fmt.Printf("  upload: ArtifactData update, collection rounds, doc_id r-%d, file_path %s\n", n, path)
		}
		return nil
	},
}

var reviewStatusCmd = &cobra.Command{
	Use:          "status [recipe-id]",
	Short:        "Show the review page, document versions, rounds and what is still open",
	Args:         cobra.MaximumNArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		sub, err := resolveSubject(cmd, args)
		if err != nil {
			return err
		}
		s, err := review.Load(sub)
		if err != nil {
			return err
		}
		url := s.URL
		if url == "" {
			url = "not published"
		}
		fmt.Printf("Review page: %s (kit v%s, points: %s)\n", url, s.Kit, joinOr(s.Points))
		docs := sub.Docs
		if sub.Bundle != "" {
			docs = append(append([]string{}, docs...), sub.Bundle)
		}
		for _, doc := range docs {
			if d := s.Docs[doc]; d != nil {
				cur, _ := review.CurrentHash(sub, doc)
				drift := ""
				if cur != d.Hash {
					drift = fmt.Sprintf(" (now %s: run `bts review page`)", cur)
				}
				fmt.Printf("  %s v%d %s%s\n", doc, d.Version, d.Hash, drift)
			}
		}
		rounds, err := review.Rounds(sub)
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
		open, err := review.OpenWork(sub, "")
		if err != nil {
			return err
		}
		for _, o := range open {
			fmt.Printf("  open: %s\n", o)
		}
		return nil
	},
}
