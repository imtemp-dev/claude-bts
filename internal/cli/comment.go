package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/imtemp-dev/claude-bts/internal/comment"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(commentCmd)
	commentCmd.AddCommand(commentPreviewCmd, commentListCmd, commentApplyCmd)

	commentPreviewCmd.Flags().Bool("include-freeform", false,
		"Also surface free-form added lines (git diff HEAD) outside callouts")
	commentPreviewCmd.Flags().String("doc", "",
		"Filter to a single doc (e.g., draft.md)")

	commentListCmd.Flags().Bool("json", false, "Output JSON")
	commentListCmd.Flags().Bool("include-freeform", false,
		"Also surface free-form added lines (git diff HEAD) outside callouts")

	commentApplyCmd.Flags().Bool("finalize", false,
		"Internal — invoked by /bts-comment-apply after edits land. "+
			"Recounts, updates manifest, appends changelog, removes pending file.")
	commentApplyCmd.Flags().Bool("dry-run", false,
		"Print the pending-comments handoff that would be written, then exit")
	commentApplyCmd.Flags().Bool("include-freeform", false,
		"Also include free-form added lines in the handoff")
}

var commentCmd = &cobra.Command{
	Use:     "comment",
	Short:   "Inspect and apply BTS review comments embedded in recipe docs",
	GroupID: "tools",
	Long: `Surface inline BTS review comments (GitHub Flavored Markdown alerts of
the form '> [!BTS-COMMENT]', '> [!BTS-BLOCK]', '> [!BTS-Q]') and hand
them to /bts-comment-apply for incorporation.

Add a comment from VS Code by typing 'btsc<Tab>' (suggestion),
'btscb<Tab>' (blocking) or 'btscq<Tab>' (question) inside any .md
file. The .vscode/markdown.code-snippets file is deployed by 'bts init'.`,
}

var commentPreviewCmd = &cobra.Command{
	Use:   "preview [recipe-id]",
	Short: "Show all detected BTS comments for a recipe (read-only, grouped by doc)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runCommentPreview,
}

var commentListCmd = &cobra.Command{
	Use:   "list [recipe-id]",
	Short: "Flat list of open comments (use --json for tooling)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runCommentList,
}

var commentApplyCmd = &cobra.Command{
	Use:   "apply [recipe-id]",
	Short: "Hand off comments to /bts-comment-apply for incorporation",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runCommentApply,
}

// resolveRecipe resolves the recipe ID from the positional arg or the
// active recipe. Returns recipeID, recipeDir, root, and error.
func resolveRecipe(args []string) (recipeID, recipeDir, root string, err error) {
	cwd, _ := os.Getwd()
	root, err = state.FindRoot(cwd)
	if err != nil {
		return "", "", "", fmt.Errorf("not a bts project: %w", err)
	}
	if len(args) == 1 {
		recipeID = args[0]
	} else {
		active, _ := state.GetActiveRecipe(root)
		if active == nil {
			active, _ = state.GetFinalizedRecipe(root)
		}
		if active == nil {
			return "", "", "", fmt.Errorf("no active recipe; pass <recipe-id> explicitly")
		}
		recipeID = active.ID
	}
	recipeDir = state.RecipeDir(root, recipeID)
	if _, err := os.Stat(recipeDir); err != nil {
		return "", "", "", fmt.Errorf("recipe dir not found: %s", recipeDir)
	}
	return recipeID, recipeDir, root, nil
}

// gatherComments parses all BTS callouts plus optionally free-form diff hunks.
// Filtered by --doc when provided.
func gatherComments(recipeDir string, includeFreeForm bool, docFilter string) ([]comment.Comment, error) {
	cs, err := comment.ParseRecipe(recipeDir)
	if err != nil {
		return nil, fmt.Errorf("parse recipe: %w", err)
	}
	if includeFreeForm {
		ff, _ := comment.ExtractFreeFormFromDiff(recipeDir)
		cs = append(cs, ff...)
	}
	if docFilter != "" {
		filtered := cs[:0]
		for _, c := range cs {
			if c.File == docFilter {
				filtered = append(filtered, c)
			}
		}
		cs = filtered
	}
	// Stable order: file then line.
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].File != cs[j].File {
			return cs[i].File < cs[j].File
		}
		return cs[i].Line < cs[j].Line
	})
	return cs, nil
}

func runCommentPreview(cmd *cobra.Command, args []string) error {
	_, recipeDir, _, err := resolveRecipe(args)
	if err != nil {
		return err
	}
	includeFF, _ := cmd.Flags().GetBool("include-freeform")
	docFilter, _ := cmd.Flags().GetString("doc")
	cs, err := gatherComments(recipeDir, includeFF, docFilter)
	if err != nil {
		return err
	}
	comment.RenderPreview(os.Stdout, cs, comment.ColorEnabled())
	return nil
}

// listEntry shapes the JSON output of `bts comment list --json`. Adds the
// classification fields on top of Comment so consumers can sort/filter
// without re-running heuristics.
type listEntry struct {
	comment.Comment
	Classification comment.Classification `json:"classification"`
}

func runCommentList(cmd *cobra.Command, args []string) error {
	_, recipeDir, _, err := resolveRecipe(args)
	if err != nil {
		return err
	}
	includeFF, _ := cmd.Flags().GetBool("include-freeform")
	cs, err := gatherComments(recipeDir, includeFF, "")
	if err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")

	entries := make([]listEntry, 0, len(cs))
	for _, c := range cs {
		entries = append(entries, listEntry{Comment: c, Classification: comment.Classify(c)})
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}

	for _, e := range entries {
		fmt.Printf("%s  %s  %s:L%d  %s\n",
			e.ID,
			string(e.Kind),
			e.File,
			e.Line,
			singleLine(e.Body),
		)
	}
	return nil
}

// pendingHandoff is the JSON written to .bts/local/recipes/<id>/pending-comments.json.
// /bts-comment-apply reads this and runs Pass A/B/C.
type pendingHandoff struct {
	RecipeID    string         `json:"recipe_id"`
	GeneratedAt string         `json:"generated_at"`
	Comments    []listEntry    `json:"comments"`
	Summary     pendingSummary `json:"summary"`
}

type pendingSummary struct {
	Total    int            `json:"total"`
	Blocking int            `json:"blocking"`
	ByKind   map[string]int `json:"by_kind"`
	ByDoc    map[string]int `json:"by_doc"`
}

func runCommentApply(cmd *cobra.Command, args []string) error {
	recipeID, recipeDir, root, err := resolveRecipe(args)
	if err != nil {
		return err
	}
	includeFF, _ := cmd.Flags().GetBool("include-freeform")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	finalize, _ := cmd.Flags().GetBool("finalize")

	if finalize {
		return runCommentApplyFinalize(root, recipeID, recipeDir, dryRun)
	}

	// Phase guard: implementation-lifecycle phases shouldn't accept new
	// spec comments — the spec is locked once final.md is sealed.
	if recipe, err := state.LoadRecipeState(root, recipeID); err == nil {
		if state.IsImplementPhase(recipe.Phase) {
			fmt.Fprintf(os.Stderr,
				"⚠ Recipe is in implementation-lifecycle phase '%s'. "+
					"Spec comments here will not affect final.md.\n", recipe.Phase)
		}
	}

	cs, err := gatherComments(recipeDir, includeFF, "")
	if err != nil {
		return err
	}
	if len(cs) == 0 {
		fmt.Println("No BTS comments to apply.")
		return nil
	}

	summary := comment.Summarize(cs)
	byKind := map[string]int{}
	for k, v := range summary.ByKind {
		byKind[string(k)] = v
	}

	entries := make([]listEntry, 0, len(cs))
	for _, c := range cs {
		entries = append(entries, listEntry{Comment: c, Classification: comment.Classify(c)})
	}

	handoff := pendingHandoff{
		RecipeID:    recipeID,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Comments:    entries,
		Summary: pendingSummary{
			Total:    summary.TotalOpen,
			Blocking: summary.TotalBlocking,
			ByKind:   byKind,
			ByDoc:    summary.OpenByDoc,
		},
	}

	if dryRun {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(handoff)
	}

	pendingDir := filepath.Join(state.LocalPath(root), "recipes", recipeID)
	if err := os.MkdirAll(pendingDir, 0755); err != nil {
		return fmt.Errorf("mkdir pending: %w", err)
	}
	pendingPath := filepath.Join(pendingDir, "pending-comments.json")
	data, err := json.MarshalIndent(handoff, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(pendingPath, data, 0644); err != nil {
		return fmt.Errorf("write pending: %w", err)
	}

	fmt.Printf("Wrote handoff: %s\n", pendingPath)
	fmt.Printf("  %d comments  (%d blocking)\n", summary.TotalOpen, summary.TotalBlocking)
	fmt.Println()
	fmt.Printf("Next: run /bts-comment-apply %s in Claude Code\n", recipeID)
	return nil
}

// runCommentApplyFinalize is invoked by /bts-comment-apply after Claude
// has edited the docs and removed resolved markers. It re-parses (callouts
// should now be mostly gone), updates manifest counts, appends a
// changelog entry, and deletes the pending file.
func runCommentApplyFinalize(root, recipeID, recipeDir string, dryRun bool) error {
	cs, err := comment.ParseRecipe(recipeDir)
	if err != nil {
		return fmt.Errorf("re-parse: %w", err)
	}
	summary := comment.Summarize(cs)

	// Compare to pre-apply count for the changelog summary.
	pendingPath := filepath.Join(state.LocalPath(root), "recipes", recipeID, "pending-comments.json")
	prevTotal, prevBlocking := 0, 0
	pendingFound := false
	if data, err := os.ReadFile(pendingPath); err == nil {
		var prev pendingHandoff
		if json.Unmarshal(data, &prev) == nil {
			prevTotal = prev.Summary.Total
			prevBlocking = prev.Summary.Blocking
			pendingFound = true
		}
	}
	if !pendingFound {
		fmt.Fprintf(os.Stderr,
			"warning: no pending-comments.json at %s — `applied` count in changelog will be 0 (no baseline to compare against). "+
				"Run `bts comment apply %s` first if you want accurate diff counts.\n",
			pendingPath, recipeID)
	}
	applied := prevTotal - summary.TotalOpen
	if applied < 0 {
		applied = 0
	}
	deferred := summary.TotalOpen
	resolvedBlocking := prevBlocking - summary.TotalBlocking
	if resolvedBlocking < 0 {
		resolvedBlocking = 0
	}

	if dryRun {
		fmt.Printf("DRY RUN — would update manifest to:\n")
		fmt.Printf("  open_comments:     %v\n", summary.OpenByDoc)
		fmt.Printf("  blocking_comments: %v\n", summary.BlockingByDoc)
		fmt.Printf("  applied=%d, deferred=%d, resolved_blocking=%d\n",
			applied, deferred, resolvedBlocking)
		return nil
	}

	manifest, err := state.LoadManifest(root, recipeID)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}
	manifest.OpenComments = summary.OpenByDoc
	manifest.BlockingComments = summary.BlockingByDoc
	if err := state.SaveManifest(root, recipeID, manifest); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	if err := state.AppendChangelog(root, recipeID, &state.ChangelogEntry{
		Action: "comment-apply",
		Result: fmt.Sprintf(
			"applied=%d deferred=%d resolved_blocking=%d remaining_blocking=%d",
			applied, deferred, resolvedBlocking, summary.TotalBlocking,
		),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: append changelog: %v\n", err)
	}

	// Drop the pending file — it's now stale.
	_ = os.Remove(pendingPath)

	fmt.Printf("Finalized: applied=%d, deferred=%d, blocking_remaining=%d\n",
		applied, deferred, summary.TotalBlocking)
	return nil
}

// singleLine compacts whitespace and rune-truncates for one-line CLI output.
// Rune-truncates rather than byte-truncates so non-ASCII bodies (Korean,
// Japanese, emoji) never get cut mid-character.
func singleLine(s string) string {
	const maxRunes = 80
	one := compact(s)
	if utf8.RuneCountInString(one) <= maxRunes {
		return one
	}
	rs := []rune(one)
	return string(rs[:maxRunes-1]) + "…"
}

// compact collapses any run of whitespace runes (including tabs and
// newlines) to a single ASCII space. Uses strings.Builder + WriteRune so
// non-ASCII characters survive the round-trip — the previous version
// cast each rune to byte() which silently truncated anything above U+00FF
// and emitted mojibake for Korean/Japanese/emoji bodies.
func compact(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return b.String()
}
