package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/imtemp-dev/claude-jig/internal/engine"
	"github.com/imtemp-dev/claude-jig/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	recipeCmd.AddCommand(recipeOverrideCmd)
	recipeOverrideCmd.AddCommand(
		recipeOverrideGrantCmd, recipeOverrideListCmd, recipeOverrideRevokeCmd,
	)

	recipeOverrideGrantCmd.Flags().String("gate", "", "Gate to bypass (see `jig recipe override list --gates`)")
	recipeOverrideGrantCmd.Flags().String("reason", "", "Why proceeding past this gate is the right call")
	recipeOverrideGrantCmd.Flags().String("doc", "", "Document the override applies to — also pins the revision it was granted on")
	recipeOverrideGrantCmd.Flags().StringArray("finding", nil, "Finding ID this override excuses (repeatable). Required unless --no-findings.")
	recipeOverrideGrantCmd.Flags().Bool("no-findings", false, "This gate is not about findings (e.g. a missing full pass)")
	_ = recipeOverrideGrantCmd.MarkFlagRequired("gate")
	_ = recipeOverrideGrantCmd.MarkFlagRequired("reason")

	recipeOverrideListCmd.Flags().Bool("json", false, "Emit JSON")
	recipeOverrideListCmd.Flags().Bool("all", false, "Include revoked and superseded records")
	recipeOverrideListCmd.Flags().Bool("gates", false, "List the gate IDs an override can name")

	recipeOverrideRevokeCmd.Flags().String("gate", "", "Gate whose override is being taken back")
	recipeOverrideRevokeCmd.Flags().String("doc", "", "Document the revoked override applied to")
	recipeOverrideRevokeCmd.Flags().String("reason", "", "Why it no longer applies")
	_ = recipeOverrideRevokeCmd.MarkFlagRequired("gate")
	_ = recipeOverrideRevokeCmd.MarkFlagRequired("reason")
}

var recipeOverrideCmd = &cobra.Command{
	Use:   "override",
	Short: "Record a deliberate decision to proceed past a hard gate",
	Long: `A hard gate the operator disagrees with does not stop them — it stops the
recorded path and leaves the unrecorded one open. A measured recipe finalized
with seven majors open and a verify round marked failed: the completion gate
refused DONE, final.md got written from draft.md anyway, and the two real
decisions behind that lived only as prose in changelog.jsonl. status, doctor
and stats all went on reporting an ordinary finalized recipe.

This makes that bypass explicit and narrow:

  jig recipe override grant <id> --gate replicated_clean_pass \
      --doc draft.md --finding F-1a2b3c4d --finding F-5e6f7a8b \
      --reason "both majors are false claims in justification prose; neither
                changes a line of code or a test assertion"

An override names ONE gate, enumerates the findings it excuses, and pins the
revision it was granted on — edit the document and it goes stale, because the
judgement was about that text. It lives in tracked state and shows up in
status, doctor and stats for the life of the recipe.`,
}

var recipeOverrideGrantCmd = &cobra.Command{
	Use:   "grant [recipe-id]",
	Short: "Record an override of one hard gate",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a jig project: %w", err)
		}
		recipeID, err := resolveRecipeID(args, root)
		if err != nil {
			return err
		}
		gate, _ := cmd.Flags().GetString("gate")
		reason, _ := cmd.Flags().GetString("reason")
		doc, _ := cmd.Flags().GetString("doc")
		findings, _ := cmd.Flags().GetStringArray("finding")
		noFindings, _ := cmd.Flags().GetBool("no-findings")

		if !engine.IsOverridableGate(gate) {
			return fmt.Errorf("unknown gate %q — run `jig recipe override list --gates` for the IDs that can be overridden", gate)
		}
		if len(findings) == 0 && !noFindings {
			return fmt.Errorf("name the findings this override excuses with --finding, or pass --no-findings if the gate is not about findings.\n" +
				"An override without an enumerated set is a blanket pass, which is the thing this command exists to prevent")
		}
		if engine.GateIsDocumentScoped(gate) && doc == "" {
			return fmt.Errorf("--doc is required for %s: it is a judgement about one document's text.\n"+
				"Without it the record matches every document at every revision and never goes stale, "+
				"which is a permanent project-wide bypass", gate)
		}

		rec := &state.OverrideRecord{
			Gate:     gate,
			Reason:   reason,
			Findings: findings,
		}
		if doc != "" {
			rec.Doc = filepath.Base(doc)
			h, ok, herr := state.FileContentHash(resolveDocPath(root, recipeID, doc))
			switch {
			case herr != nil || !ok:
				// An unpinnable override is a blanket pass for that
				// document — it survives every later edit to the text the
				// operator actually weighed. Refuse rather than record one
				// behind a warning nobody reads.
				return fmt.Errorf("could not read %s to pin this override to a revision "+
					"(looked in %s).\nPass a --doc path that resolves from here or from the recipe directory; "+
					"an override that is not pinned keeps applying after the text it excused has changed",
					doc, resolveDocPath(root, recipeID, doc))
			default:
				rec.DocHash = h
			}
			if last, lerr := state.LastVerifyEntryForDoc(root, recipeID, rec.Doc); lerr == nil && last != nil {
				rec.Iteration = last.Iteration
			}
		}

		// Warn — do not refuse — when a named finding is not in the
		// ledger. A typo should be visible; a legitimately hand-named
		// item should not be blocked.
		if states, ferr := state.LoadFindings(root, recipeID, rec.Doc); ferr == nil {
			known := map[string]bool{}
			for _, st := range states {
				known[st.ID] = true
			}
			var unknown []string
			for _, f := range findings {
				if !known[f] {
					unknown = append(unknown, f)
				}
			}
			if len(unknown) > 0 {
				fmt.Fprintf(os.Stderr,
					"[jig] warning: %s not found in the findings ledger for %s — check the IDs with `jig recipe findings list %s --open`\n",
					strings.Join(unknown, ", "), rec.Doc, recipeID)
			}
		}

		if err := state.AppendOverride(root, recipeID, rec); err != nil {
			return fmt.Errorf("record override: %w", err)
		}
		fmt.Printf("Override recorded: %s", gate)
		if rec.Doc != "" {
			fmt.Printf(" on %s", rec.Doc)
		}
		if len(findings) > 0 {
			fmt.Printf(" excusing %d finding(s)", len(findings))
		}
		fmt.Println()
		if rec.DocHash == "" {
			fmt.Println("  NOT pinned to a revision — it will keep applying after the document changes.")
		} else {
			fmt.Println("  Pinned to the current revision. Editing the document makes it stale.")
		}
		fmt.Printf("  This recipe now reports as overridden in `jig recipe status`, `jig doctor` and `jig stats`.\n")
		return nil
	},
}

var recipeOverrideListCmd = &cobra.Command{
	Use:   "list [recipe-id]",
	Short: "Show the overrides in force on a recipe",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if gates, _ := cmd.Flags().GetBool("gates"); gates {
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "GATE\tWHAT IT ENFORCES")
			for _, g := range engine.OverridableGates() {
				fmt.Fprintf(w, "%s\t%s\n", g.ID, state.TruncateRunes(g.Summary, 90))
			}
			return w.Flush()
		}

		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a jig project: %w", err)
		}
		recipeID, err := resolveRecipeID(args, root)
		if err != nil {
			return err
		}
		records, err := state.ReadOverrides(root, recipeID)
		if err != nil {
			return fmt.Errorf("read overrides: %w", err)
		}
		all, _ := cmd.Flags().GetBool("all")
		shown := records
		if !all {
			shown = state.LiveOverrides(records, state.CurrentDocHashes(root, recipeID, records))
		}
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(shown)
		}
		if len(shown) == 0 {
			fmt.Println("No gate overrides on this recipe.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "GATE\tDOC\tFINDINGS\tPINNED\tWHEN\tREASON")
		for _, r := range shown {
			pinned := "no"
			if r.DocHash != "" {
				pinned = "yes"
			}
			revoked := ""
			if r.Revoked {
				revoked = " (revoked)"
			}
			fmt.Fprintf(w, "%s%s\t%s\t%d\t%s\t%s\t%s\n",
				r.Gate, revoked, r.Doc, len(r.Findings), pinned, r.Timestamp,
				state.TruncateRunes(strings.ReplaceAll(r.Reason, "\n", " "), 70))
		}
		return w.Flush()
	},
}

var recipeOverrideRevokeCmd = &cobra.Command{
	Use:   "revoke [recipe-id]",
	Short: "Take back an override",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a jig project: %w", err)
		}
		recipeID, err := resolveRecipeID(args, root)
		if err != nil {
			return err
		}
		gate, _ := cmd.Flags().GetString("gate")
		reason, _ := cmd.Flags().GetString("reason")
		doc, _ := cmd.Flags().GetString("doc")

		// grant validates the gate name; revoke did not, and reported
		// success either way. A typo therefore filed a revocation that
		// matched no grant while telling the operator the gate was
		// enforced again — the one message they must be able to trust.
		if !engine.IsOverridableGate(gate) {
			return fmt.Errorf("unknown gate %q — run `jig recipe override list --gates` for the IDs that can be overridden", gate)
		}

		rec := &state.OverrideRecord{Gate: gate, Reason: reason, Revoked: true}
		if doc != "" {
			rec.Doc = filepath.Base(doc)
		}

		// Say what was actually taken back. Revoking nothing is not an
		// error — an operator tidying up should not be blocked — but it
		// must not read as having restored a gate.
		var matched []state.OverrideRecord
		if records, rerr := state.ReadOverrides(root, recipeID); rerr == nil {
			for _, live := range state.LiveOverrides(records, nil) {
				if live.Gate == gate && (rec.Doc == "" || live.Doc == "" || live.Doc == rec.Doc) {
					matched = append(matched, live)
				}
			}
		}

		if err := state.AppendOverride(root, recipeID, rec); err != nil {
			return fmt.Errorf("record revocation: %w", err)
		}
		if len(matched) == 0 {
			fmt.Printf("Revocation recorded for %s, but no override of that gate was in force%s — nothing changed.\n",
				gate, docSuffix(rec.Doc))
			return nil
		}
		fmt.Printf("Override revoked: %s%s (%d record(s)). The gate is enforced again.\n",
			gate, docSuffix(rec.Doc), len(matched))
		return nil
	},
}

// resolveDocPath turns a --doc value into something readable from the
// project root, accepting both a bare basename and a full path.
func docSuffix(doc string) string {
	if doc == "" {
		return ""
	}
	return " on " + doc
}

func resolveDocPath(root, recipeID, doc string) string {
	if filepath.IsAbs(doc) {
		return doc
	}
	if strings.Contains(doc, string(filepath.Separator)) {
		return filepath.Join(root, doc)
	}
	return filepath.Join(state.RecipeDir(root, recipeID), doc)
}
