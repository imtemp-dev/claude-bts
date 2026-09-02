package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	recipeCmd.AddCommand(recipeFindingsCmd, recipeAssessPrecheckCmd)
	recipeFindingsCmd.AddCommand(
		recipeFindingsListCmd, recipeFindingsCarryCmd, recipeFindingsDismissCmd,
		recipeFindingsDefendBatchCmd,
	)
	recipeFindingsDefendBatchCmd.Flags().String("doc", "draft.md", "Document whose open findings to defend (basename)")
	recipeFindingsDefendBatchCmd.Flags().Int("limit", -1, "Findings per pass; default 2 x simulate.finding_batch, 0 = no cap")
	recipeFindingsDefendBatchCmd.Flags().Bool("json", false, "Emit JSON")
	recipeFindingsListCmd.Flags().String("doc", "", "Narrow to one document (basename)")
	recipeFindingsListCmd.Flags().Bool("open", false, "Only findings currently open")
	recipeFindingsListCmd.Flags().Bool("json", false, "Emit JSON")
	recipeFindingsCarryCmd.Flags().String("doc", "", "Document whose adjudicated findings to carry forward")
	recipeFindingsDismissCmd.Flags().String("reason", "", "Why this finding is not a defect")
	_ = recipeFindingsDismissCmd.MarkFlagRequired("reason")
	recipeAssessPrecheckCmd.Flags().String("doc", "", "Document to check (default: the recipe's draft.md)")
}

// resolveRecipeID returns the argument if given, else the active recipe.
func resolveRecipeID(args []string, root string) (string, error) {
	if len(args) > 0 && args[0] != "" {
		return args[0], nil
	}
	rs, err := state.GetActiveRecipe(root)
	if err != nil || rs == nil {
		return "", fmt.Errorf("no recipe id given and no active recipe found")
	}
	return rs.ID, nil
}

var recipeFindingsCmd = &cobra.Command{
	Use:   "findings",
	Short: "Inspect the cross-round findings ledger",
	Long: `The findings ledger (findings.jsonl) gives verification findings a stable
identity across rounds. verification.md is overwritten every cycle and numbers
its findings positionally, so without the ledger "#4" in one round and "#4" in
the next are unrelated — settled points get re-litigated and the stagnation
rule in bts-verification-protocol.md has nothing to detect with.

The ledger is written automatically by
  bts recipe log <id> --from-verification <path> --doc <doc>
whenever the <bts-findings> block carries a "findings" array.`,
}

var recipeFindingsListCmd = &cobra.Command{
	Use:   "list [recipe-id]",
	Short: "List findings with their cross-round history",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project: %w", err)
		}
		recipeID, err := resolveRecipeID(args, root)
		if err != nil {
			return err
		}
		doc, _ := cmd.Flags().GetString("doc")
		onlyOpen, _ := cmd.Flags().GetBool("open")
		asJSON, _ := cmd.Flags().GetBool("json")

		states, err := state.LoadFindings(root, recipeID, doc)
		if err != nil {
			return fmt.Errorf("load findings: %w", err)
		}
		if onlyOpen {
			// "Open" means "still owed", which includes unreported —
			// a finding that merely stopped being mentioned was never
			// confirmed fixed, and hiding it here is exactly how false
			// closures became invisible progress.
			var f []*state.FindingState
			for _, st := range states {
				if state.NotClosed(st.Status) {
					f = append(f, st)
				}
			}
			states = f
		}

		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(states)
		}
		if len(states) == 0 {
			fmt.Println("No findings recorded. The ledger fills in as verify rounds are logged with --doc.")
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSTATUS\tSEVERITY\tROUNDS\tREOPEN\tDOC\tTITLE")
		for _, st := range states {
			title := st.Title
			if len(title) > 64 {
				title = title[:61] + "..."
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
				st.ID, st.Status, st.Severity, st.OpenRounds, st.Reopened, st.Doc, title)
		}
		return w.Flush()
	},
}

var recipeFindingsCarryCmd = &cobra.Command{
	Use:   "carry-forward [recipe-id]",
	Short: "Print the adjudicated-findings block for the next verifier prompt",
	Long: `Renders the previous rounds' findings as a prompt section telling the
verifier which points are already settled. /bts-verify appends this to the
verifier prompt so a fresh agent does not re-derive the whole document and
re-raise findings that were already fixed or dismissed.

Prints nothing when the ledger is empty (first round).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project: %w", err)
		}
		recipeID, err := resolveRecipeID(args, root)
		if err != nil {
			return err
		}
		doc, _ := cmd.Flags().GetString("doc")
		states, err := state.LoadFindings(root, recipeID, doc)
		if err != nil {
			return fmt.Errorf("load findings: %w", err)
		}
		block := state.CarryForwardBlock(states)
		if block == "" {
			fmt.Println("No previous findings on this document — nothing to carry forward.")
			return nil
		}
		fmt.Print(block)
		return nil
	},
}

// recipeFindingsDefendBatchCmd prints what /bts-defend argues against in
// one pass. The selection and the cap live in state.DefendBatch, in Go,
// because the defender is a Skill fork that reads the ledger itself: no
// spawn prompt carries its batch, so the pre-tool-use hook that refuses
// over-large Agent-tool batches never sees it. The measured failure this
// bounds — validators handed 10–28 findings writing 80–110K output tokens
// and three abandoned at the 64K limit — is one the agent's own reading
// of a limit cannot prevent; what the command prints can.
var recipeFindingsDefendBatchCmd = &cobra.Command{
	Use:   "defend-batch [recipe-id]",
	Short: "Print the open critical/major findings for one /bts-defend pass, capped by simulate.finding_batch",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project: %w", err)
		}
		recipeID, err := resolveRecipeID(args, root)
		if err != nil {
			return err
		}
		doc, _ := cmd.Flags().GetString("doc")
		limit, _ := cmd.Flags().GetInt("limit")
		asJSON, _ := cmd.Flags().GetBool("json")
		if limit < 0 {
			limit = 12
			if s, serr := engine.LoadSettings(root); serr == nil && s.Simulate.FindingBatch > 0 {
				limit = 2 * s.Simulate.FindingBatch
			}
		}

		states, err := state.LoadFindings(root, recipeID, doc)
		if err != nil {
			return fmt.Errorf("load findings: %w", err)
		}
		batch, rest := state.DefendBatch(states, limit)

		if asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{
				"doc": doc, "limit": limit, "batch": batch, "undefended": rest,
			})
		}
		if len(batch) == 0 {
			fmt.Printf("No open critical or major findings on %s — nothing to defend.\n", doc)
			return nil
		}
		fmt.Printf("Defend batch for %s (%d of %d open critical/major; limit %d):\n", doc, len(batch), len(batch)+len(rest), limit)
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSEVERITY\tSTATUS\tROUNDS\tANCHOR\tTITLE")
		for _, st := range batch {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
				st.ID, st.Severity, st.Status, st.OpenRounds, st.Anchor, st.Title)
		}
		if err := w.Flush(); err != nil {
			return err
		}
		if len(rest) == 0 {
			fmt.Println("Undefended: none")
			return nil
		}
		undefended := make([]string, 0, len(rest))
		for _, st := range rest {
			undefended = append(undefended, st.ID)
		}
		fmt.Printf("Undefended (%d, over the batch): %s — run /bts-defend again after recording this pass\n",
			len(rest), strings.Join(undefended, " "))
		return nil
	},
}

var recipeFindingsDismissCmd = &cobra.Command{
	Use:   "dismiss <recipe-id> <finding-id>",
	Short: "Record that a finding is not a defect so later rounds stop raising it",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project: %w", err)
		}
		reason, _ := cmd.Flags().GetString("reason")
		if err := state.DismissFinding(root, args[0], args[1], reason); err != nil {
			return err
		}
		fmt.Printf("Dismissed %s: %s\n", args[1], reason)
		return nil
	},
}

// recipeAssessPrecheckCmd answers, deterministically, whether the loop
// still needs an LLM assessment round.
//
// Measured recipes ran verification rounds on documents that were already
// at critical=0/major=0/minor_resolvable=0 and unchanged since — 18 such
// rounds found nothing at all. Every one of those cost a full assess plus
// a full verify. This check reads verify-log and the verify snapshot and
// answers from state alone.
var recipeAssessPrecheckCmd = &cobra.Command{
	Use:   "assess-precheck [recipe-id]",
	Short: "Emit a <bts-decision> from recorded state when no LLM assessment is needed",
	Long: `Prints a <bts-decision> block when the next action is determined by state
alone, letting the loop skip the /bts-assess round entirely:

  FINALIZE                 the doc's verify history satisfies the completion
                           contract (clean, full pass, every dimension,
                           replicated on the recorded revision) and the doc is
                           unchanged since — nothing left to assess
  HALT_CONVERGENCE_FAILED  the convergence budget is exhausted
  VERIFY                   the doc changed since its last verification

Exits 10 with no decision when the situation needs judgement; callers should
fall through to /bts-assess in that case.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project: %w", err)
		}
		recipeID, err := resolveRecipeID(args, root)
		if err != nil {
			return err
		}
		doc, _ := cmd.Flags().GetString("doc")
		if doc == "" {
			doc = filepath.Join(state.RecipeDir(root, recipeID), "draft.md")
		}
		docBase := filepath.Base(doc)

		settings, err := engine.LoadSettings(root)
		if err != nil {
			return fmt.Errorf("load settings: %w", err)
		}
		history, err := state.ReadVerifyLog(root, recipeID)
		if err != nil {
			return fmt.Errorf("read verify-log: %w", err)
		}
		scoped := state.VerifyEntriesForDoc(history, docBase)
		if len(scoped) == 0 {
			return precheckUndecided("no verification history for " + docBase)
		}

		verdict := engine.EvaluateConvergenceWithCap(scoped,
			settings.Verify.MaxIterations, settings.Verify.MaxRounds)
		if verdict.CapHit {
			emitDecision("HALT_CONVERGENCE_FAILED", "verify", verdict.Latest.String(),
				fmt.Sprintf("round cap reached: %d rounds recorded (verify.max_rounds=%d). "+
					"Move the open findings to Known Uncertainties with the command that would "+
					"settle each, then implement.",
					verdict.Rounds, verdict.RoundCap))
			return nil
		}
		if verdict.Exceeded {
			emitDecision("HALT_CONVERGENCE_FAILED", "verify", verdict.Latest.String(),
				fmt.Sprintf("%d consecutive rounds without progress (budget %d); best reached %s",
					verdict.Streak, verdict.Budget, verdict.Best))
			return nil
		}

		last := scoped[len(scoped)-1]

		// A document modified after its last verification must be
		// re-verified before anything else can be decided (rule 3).
		dirty, derr := state.DirtyVerifiedDocs(root, recipeID)
		if derr == nil {
			for _, d := range dirty {
				if d == docBase {
					emitDecision("VERIFY", "verify", verdict.Latest.String(),
						docBase+" changed since its last verification — rule 3 requires /bts-verify before assessing")
					return nil
				}
			}
		}

		if !verdict.Latest.Clean() {
			return precheckUndecided(fmt.Sprintf(
				"%s still has %s — the next action depends on the findings", docBase, verdict.Latest))
		}
		// The completion contract lives in ONE place. This precheck used
		// to carry its own copy — clean plus a full pass — which stopped
		// agreeing with the stop hook the moment the hook started asking
		// for dimensions, a recorded revision and replication. The loop
		// then had two oracles: the precheck said FINALIZE, the hook
		// refused DONE, and the recipe bounced between them.
		if ev := engine.EvaluateCompletionEvidence(scoped, settings.Verify.ConfirmPasses); !ev.Confirmed {
			emitDecision("VERIFY", "verify", verdict.Latest.String(),
				fmt.Sprintf("%s is clean but cannot be finalized on the evidence recorded: %s. %s",
					docBase, ev.Reason, ev.Remedy))
			return nil
		}
		if len(dirty) > 0 {
			return precheckUndecided(fmt.Sprintf(
				"%s is clean but %s still modified since last verification",
				docBase, strings.Join(dirty, ", ")))
		}

		reason := docBase + " converged on a replicated full pass across every dimension and is unchanged since"
		if last.MinorDeferred > 0 {
			reason += fmt.Sprintf("; %d [deferred] minor(s) carry into /bts-implement as watch-items", last.MinorDeferred)
		}
		emitDecision("FINALIZE", "finalize", verdict.Latest.String(), reason)
		return nil
	},
}

// emitDecision prints the machine-readable block the loop already parses.
func emitDecision(action, phase, findings, reason string) {
	payload := map[string]any{
		"level":        3.0,
		"action":       action,
		"phase":        phase,
		"reason":       reason,
		"findings_ref": findings,
		"source":       "assess-precheck (deterministic, no LLM round)",
	}
	if action != "FINALIZE" {
		delete(payload, "level")
	}
	b, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Printf("<bts-decision>\n%s\n</bts-decision>\n", b)
}

// precheckUndecided reports that judgement is required. Exit code 10
// distinguishes "needs /bts-assess" from a real failure.
func precheckUndecided(why string) error {
	fmt.Printf("UNDECIDED: %s\nRun /bts-assess for a judgement round.\n", why)
	os.Exit(10)
	return nil
}
