package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/metrics"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(recipeCmd)
	recipeCmd.AddCommand(
		recipeStatusCmd, recipeListCmd, recipeLogCmd,
		recipeCancelCmd, recipeCreateCmd, recipeReconcileCmd,
		recipeVerifyFocusCmd,
	)
	recipeCreateCmd.Flags().String("type", "blueprint", "Recipe type (blueprint, design, analyze, fix, debug)")
	recipeCreateCmd.Flags().String("topic", "", "Recipe topic description")
	_ = recipeCreateCmd.MarkFlagRequired("topic")
	recipeReconcileCmd.Flags().Bool("dry-run", false, "Print the plan without writing")
	recipeReconcileCmd.Flags().Bool("force", false, "Bypass blueprint-phase whitelist (implement-phase still protected)")
}

var recipeCmd = &cobra.Command{
	Use:     "recipe",
	Short:   "Manage recipe execution state",
	GroupID: "recipe",
}

var recipeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active recipe status",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project: %w", err)
		}

		recipe, err := state.GetActiveRecipe(root)
		if err != nil {
			return fmt.Errorf("read state: %w", err)
		}

		if recipe == nil {
			recipe, _ = state.GetFinalizedRecipe(root)
		}

		if recipe == nil {
			fmt.Println("No active recipe.")
			return nil
		}

		label := "Active recipe"
		if recipe.Phase == "finalize" {
			label = "Finalized recipe (ready for implementation)"
		}
		fmt.Printf("%s: %s\n", label, recipe.ID)
		fmt.Printf("  Type:         %s\n", recipe.Type)
		fmt.Printf("  Topic:        %s\n", recipe.Topic)
		fmt.Printf("  Phase:        %s\n", recipe.Phase)
		fmt.Printf("  Iteration:    %d\n", recipe.Iteration)
		if recipe.DraftVersion > 0 {
			fmt.Printf("  Draft:        v%d\n", recipe.DraftVersion)
		}
		fmt.Printf("  Level:        %.1f\n", recipe.Level)
		fmt.Printf("  Started:      %s\n", recipe.StartedAt)
		fmt.Printf("  Updated:      %s\n", recipe.UpdatedAt)
		return nil
	},
}

var recipeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all recipes",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project: %w", err)
		}

		recipes, err := state.ListRecipes(root)
		if err != nil {
			return fmt.Errorf("list: %w", err)
		}

		if len(recipes) == 0 {
			fmt.Println("No recipes found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tType\tTopic\tPhase\tIteration\tUpdated")
		for _, r := range recipes {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n",
				r.ID, r.Type, truncate(r.Topic, 30), r.Phase, r.Iteration, r.UpdatedAt)
		}
		w.Flush()
		return nil
	},
}

var recipeLogCmd = &cobra.Command{
	Use:   "log <recipe-id>",
	Short: "Record an action or verify iteration (called by skills via Bash)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project: %w", err)
		}

		recipeID := args[0]
		action, _ := cmd.Flags().GetString("action")
		phase, _ := cmd.Flags().GetString("phase")

		// Update phase if specified (independent of action/iteration mode)
		if phase != "" {
			recipe, err := state.LoadRecipeState(root, recipeID)
			if err != nil {
				return fmt.Errorf("load recipe: %w", err)
			}

			// Pre-condition checks for phase transition
			force, _ := cmd.Flags().GetBool("force")
			if err := checkPhasePreConditions(root, recipe, phase, force); err != nil {
				return err
			}

			previousPhase := recipe.Phase
			recipe.Phase = phase
			if err := state.SaveRecipeState(root, recipe); err != nil {
				return fmt.Errorf("save recipe: %w", err)
			}
			_ = metrics.Append(root, &metrics.MetricsEvent{
				Kind:          metrics.KindPhaseChange,
				RecipeID:      recipeID,
				Phase:         phase,
				PreviousPhase: previousPhase,
			})
			fmt.Printf("Phase → %s\n", phase)
		}

		if action != "" {
			// Changelog mode: log an action
			output, _ := cmd.Flags().GetString("output")
			basedOn, _ := cmd.Flags().GetString("based-on")
			docType, _ := cmd.Flags().GetString("doc-type")
			result, _ := cmd.Flags().GetString("result")
			gaps, _ := cmd.Flags().GetInt("gaps")

			entry := &state.ChangelogEntry{
				Action: action,
				Output: output,
				Result: result,
			}
			if basedOn != "" {
				entry.BasedOn = []string{basedOn}
			}
			if gaps > 0 {
				entry.Result = fmt.Sprintf("%d gaps found", gaps)
			}

			if err := state.AppendChangelog(root, recipeID, entry); err != nil {
				return fmt.Errorf("changelog: %w", err)
			}

			// Update manifest if output specified
			if output != "" {
				manifest, _ := state.LoadManifest(root, recipeID)
				var deps []string
				if basedOn != "" {
					deps = []string{basedOn}
				}
				// Use explicit doc-type if given, otherwise infer from action
				manifestType := docType
				if manifestType == "" {
					manifestType = actionToDocType(action)
				}
				manifest.AddDocument(output, manifestType, deps)
				if err := state.SaveManifest(root, recipeID, manifest); err != nil {
					fmt.Fprintf(os.Stderr, "warning: save manifest: %v\n", err)
				}
			}

			fmt.Printf("Logged action: %s → %s\n", action, output)
		} else if phase == "" {
			// Verify-log mode: log an iteration result.
			// Preferred: --from-verification parses the <bts-findings>
			// block so counts can never drift from verification.md.
			// Fallback: --minor-resolvable / --minor-deferred (split form).
			// --minor is accepted for legacy callers and mapped to resolvable.
			iteration, _ := cmd.Flags().GetInt("iteration")
			critical, _ := cmd.Flags().GetInt("critical")
			major, _ := cmd.Flags().GetInt("major")
			minor, _ := cmd.Flags().GetInt("minor")
			minorR, _ := cmd.Flags().GetInt("minor-resolvable")
			minorD, _ := cmd.Flags().GetInt("minor-deferred")
			infoCt, _ := cmd.Flags().GetInt("info")
			doc, _ := cmd.Flags().GetString("doc")
			scope, _ := cmd.Flags().GetString("scope")

			if scope != "full" && scope != "delta" {
				return fmt.Errorf("--scope must be full or delta, got %q", scope)
			}
			docBase := ""
			if doc != "" {
				docBase = filepath.Base(doc)
			}

			var reported []state.ReportedFinding
			haveFindingsArray := false

			if fromVerification, _ := cmd.Flags().GetString("from-verification"); fromVerification != "" {
				data, err := os.ReadFile(fromVerification)
				if err != nil {
					return fmt.Errorf("--from-verification: read %s: %w", fromVerification, err)
				}
				counts, err := engine.ParseFindingsBlock(data)
				if err != nil {
					return fmt.Errorf("--from-verification: %s: %w", fromVerification, err)
				}
				critical = counts.Critical
				major = counts.Major
				minorR = counts.MinorResolvable
				minorD = counts.MinorDeferred
				infoCt = counts.Info
				minor = 0
				reported = counts.Findings
				haveFindingsArray = len(counts.Findings) > 0 || counts.Total() == 0
				if iteration == 0 {
					// Iteration numbering follows the document's own
					// history when one is recorded — a wireframe round
					// must not advance the draft's counter.
					var last *state.VerifyLogEntry
					if docBase != "" {
						if e, derr := state.LastVerifyEntryForDoc(root, recipeID, docBase); derr == nil {
							last = e
						}
					}
					if last == nil {
						if e, lerr := state.LastVerifyEntry(root, recipeID); lerr == nil {
							last = e
						}
					}
					if last != nil {
						iteration = last.Iteration + 1
					} else {
						iteration = 1
					}
				}
			}

			// Legacy fallback: caller passed --minor but not the split flags.
			// Treat as resolvable (the strict, conservative interpretation).
			if minor > 0 && minorR == 0 && minorD == 0 {
				minorR = minor
			}

			// Converged requires critical=0, major=0, and no resolvable minors.
			// [deferred] minors do not block — they are runtime watch-items.
			status := "continue"
			if critical == 0 && major == 0 && minorR == 0 {
				status = "converged"
			}

			entry := &state.VerifyLogEntry{
				Iteration:       iteration,
				Critical:        critical,
				Major:           major,
				Minor:           minor,
				MinorResolvable: minorR,
				MinorDeferred:   minorD,
				Info:            infoCt,
				Doc:             docBase,
				FullPass:        scope == "full",
				Status:          status,
			}

			// Convergence budget (bts-verification-protocol.md § Convergence).
			// Evaluated on this document's history INCLUDING the round being
			// logged, so the streak the operator sees is the real one.
			settings, serr := engine.LoadSettings(root)
			if serr != nil {
				return fmt.Errorf("load settings: %w", serr)
			}
			// Stamp the budget this round is judged under. The verdict below
			// is recomputed over the whole history from CURRENT settings, so
			// without this stamp the log cannot say which regime produced a
			// given Status — see state.VerifyLogEntry.Budget.
			entry.Budget = settings.Verify.MaxIterations
			history, herr := state.ReadVerifyLog(root, recipeID)
			if herr != nil {
				fmt.Fprintf(os.Stderr, "warning: read verify-log for convergence check: %v\n", herr)
			}
			// Narrow to this document's history, then append the round
			// being logged. Copy rather than append in place: the slice
			// may alias `history`'s backing array, and filepath.Base("")
			// is "." — an unscoped round must fall back to the whole
			// stream, not match a document literally named ".".
			priorRounds := history
			if docBase != "" {
				priorRounds = state.VerifyEntriesForDoc(history, docBase)
			}
			label := docBase
			if label == "" {
				label = "(unscoped)"
			}
			// Tie this record to a fork actually having run. Evidence, not
			// a gate — see state.VerifyLogEntry.AgentEvidence.
			entry.AgentEvidence = agentEvidenceSince(root, recipeID, priorRounds)

			// Record what was verified, by content. Both rule-3 gates read
			// these instead of mtimes and gitignored snapshots, so they
			// behave the same in a worktree as on the branch the recipe
			// started on — see state.VerifyLogEntry.DocHash.
			stampContentHashes(root, recipeID, doc, entry)

			scopedHistory := make([]state.VerifyLogEntry, 0, len(priorRounds)+1)
			scopedHistory = append(scopedHistory, priorRounds...)
			scopedHistory = append(scopedHistory, *entry)
			verdict := engine.EvaluateConvergence(scopedHistory, settings.Verify.MaxIterations)

			// A budget change re-judges this document's whole history on the
			// next evaluation. That is a legitimate operator action, but it
			// must not happen silently: an earlier round's stored "failed"
			// was decided under the old budget and would not be reproduced
			// under the new one.
			if prev, drifted := state.BudgetDrift(priorRounds, settings.Verify.MaxIterations); drifted {
				fmt.Fprintf(os.Stderr,
					"[bts] note: verify.max_iterations changed %d → %d since the last round of %s. "+
						"Earlier rounds were judged under the old budget; the convergence verdict is "+
						"recomputed from the current one.\n",
					prev, settings.Verify.MaxIterations, label)
			}

			// Ledger sync gives findings identity across rounds, which is
			// what makes the stagnation half of the rule computable.
			var sync *state.SyncResult
			if haveFindingsArray && docBase != "" {
				s, serr := state.SyncFindings(root, recipeID, docBase, iteration, reported, settings.Verify.MaxIterations)
				if serr != nil {
					fmt.Fprintf(os.Stderr, "warning: findings ledger sync: %v\n", serr)
				} else {
					sync = s
					verdict.Stagnant = s.Stagnant
				}
			}

			if verdict.Exceeded {
				entry.Status = "failed"
				status = "failed"
			}

			if err := state.AppendVerifyLog(root, recipeID, entry); err != nil {
				return fmt.Errorf("log: %w", err)
			}

			// Also log to changelog with the split fields for downstream validators.
			if err := state.AppendChangelog(root, recipeID, &state.ChangelogEntry{
				Action: "verify",
				Result: fmt.Sprintf("critical=%d major=%d minor_resolvable=%d minor_deferred=%d → %s",
					critical, major, minorR, minorD, status),
			}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: append changelog: %v\n", err)
			}

			// Snapshot the just-verified doc revision so the next
			// verify round can diff against it (verify-focus). Only full
			// passes update the snapshot: a delta round verified part of
			// the document, so the next round's focus diff must still
			// carry everything not covered since the last full pass.
			if doc != "" && scope == "full" {
				if err := state.SaveVerifySnapshot(root, recipeID, doc); err != nil {
					fmt.Fprintf(os.Stderr, "warning: verify snapshot: %v\n", err)
				}
			}

			fmt.Printf("Logged iteration %d [%s, %s pass]: critical=%d major=%d minor_resolvable=%d minor_deferred=%d → %s (budget=%d)\n",
				iteration, label, scope, critical, major, minorR, minorD, status, entry.Budget)

			if sync != nil {
				fmt.Printf("Findings ledger: %d new, %d carried, %d fixed, %d reopened\n",
					len(sync.New), len(sync.Carried), len(sync.Fixed), len(sync.Reopened))
				if len(sync.Reopened) > 0 {
					fmt.Printf("  reopened (previously fixed or dismissed): %v\n", sync.Reopened)
				}
			} else if docBase == "" {
				fmt.Fprintln(os.Stderr,
					"[bts] note: no --doc given — verify state stays unscoped and the findings ledger is skipped.")
			} else if !haveFindingsArray {
				fmt.Fprintln(os.Stderr,
					"[bts] note: <bts-findings> has no \"findings\" array — ledger skipped, so stagnation detection is unavailable this round.")
			}

			if verdict.Exceeded {
				// The round WAS logged; this is a loop-control outcome,
				// not a misuse of flags, so suppress cobra's usage dump —
				// it would bury the report the operator needs to read.
				cmd.SilenceUsage = true
				fmt.Fprintln(os.Stderr, verdict.Message(label))
				return fmt.Errorf("convergence budget exhausted after %d rounds without progress", verdict.Streak)
			}
			if verdict.Budget > 0 && verdict.Streak > 0 {
				fmt.Printf("Convergence: %d/%d rounds without progress (best so far: %s)\n",
					verdict.Streak, verdict.Budget, verdict.Best)
			}
		}

		return nil
	},
}

// recipeVerifyFocusCmd prints changes since the last verified snapshot
// of a document. /bts-verify prepends this to the verifier prompt as
// focus hints — full re-verification still applies; this only directs
// extra scrutiny at changed sections and their ripple effects.
var recipeVerifyFocusCmd = &cobra.Command{
	Use:   "verify-focus <doc-path>",
	Short: "Print changes since the doc's last verified snapshot (focus hints for /bts-verify)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project: %w", err)
		}

		docPath := args[0]
		recipeID := state.RecipeIDFromDocPath(docPath)
		if recipeID == "" {
			fmt.Println("Not a recipe document (no recipes/<id>/ in path) — no focus hints. Full verification only.")
			return nil
		}

		current, err := os.ReadFile(docPath)
		if err != nil {
			return fmt.Errorf("read doc: %w", err)
		}

		snap, ok, err := state.LoadVerifySnapshot(root, recipeID, filepath.Base(docPath))
		if err != nil {
			return fmt.Errorf("load snapshot: %w", err)
		}
		if !ok {
			fmt.Printf("FIRST VERIFICATION of %s — no previous verified snapshot. Full verification only; no focus hints.\n", filepath.Base(docPath))
			return nil
		}

		diff, changed := engine.UnifiedLineDiff(string(snap), string(current))
		if !changed {
			fmt.Println("No changes since the last verified snapshot.")
			return nil
		}
		fmt.Println("## Changes since last verified revision (focus hints — full verification still required)")
		fmt.Println()
		fmt.Print(diff)
		return nil
	},
}

var recipeCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new recipe and output its ID",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project: %w", err)
		}

		recipeType, _ := cmd.Flags().GetString("type")
		topic, _ := cmd.Flags().GetString("topic")

		id := state.NewRecipeID(root, topic)

		// Initial phase depends on recipe type
		initialPhase := "discovery" // blueprint: intent discovery first
		switch recipeType {
		case "fix", "debug", "analyze", "design":
			initialPhase = "research"
		}

		recipe := &state.RecipeState{
			ID:        id,
			Type:      recipeType,
			Topic:     topic,
			Phase:     initialPhase,
			StartedAt: time.Now().UTC().Format(time.RFC3339),
		}

		if err := state.SaveRecipeState(root, recipe); err != nil {
			return fmt.Errorf("create recipe: %w", err)
		}

		// Create empty manifest.json
		manifest := &state.Manifest{
			Documents: make(map[string]state.DocumentEntry),
		}
		if err := state.SaveManifest(root, id, manifest); err != nil {
			return fmt.Errorf("create manifest: %w", err)
		}

		// Output ID only (for skill capture via Bash)
		fmt.Println(id)
		return nil
	},
}

var recipeCancelCmd = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel the active recipe",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		root, err := state.FindRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a bts project: %w", err)
		}

		recipe, err := state.GetActiveRecipe(root)
		if err != nil || recipe == nil {
			fmt.Println("No active recipe to cancel.")
			return nil
		}

		recipe.Phase = "cancelled"
		if err := state.SaveRecipeState(root, recipe); err != nil {
			return fmt.Errorf("save: %w", err)
		}

		fmt.Printf("Recipe %s cancelled.\n", recipe.ID)
		return nil
	},
}

func init() {
	// Verify-log flags
	recipeLogCmd.Flags().Int("iteration", 0, "Iteration number")
	recipeLogCmd.Flags().Int("critical", 0, "Critical finding count")
	recipeLogCmd.Flags().Int("major", 0, "Major finding count")
	recipeLogCmd.Flags().Int("minor", 0, "Legacy: undifferentiated minor count (maps to --minor-resolvable)")
	recipeLogCmd.Flags().Int("minor-resolvable", 0, "Minor [resolvable] count — fixable in spec, blocks completion")
	recipeLogCmd.Flags().Int("minor-deferred", 0, "Minor [deferred] count — runtime-observable, does not block")
	recipeLogCmd.Flags().Int("info", 0, "Info suggestion count")
	recipeLogCmd.Flags().String("from-verification", "", "Parse counts from a verification.md <bts-findings> block (atomic; iteration auto-increments unless --iteration given)")
	// No backticks in flag usage strings: cobra reads the first
	// backtick-quoted word as the value placeholder name.
	recipeLogCmd.Flags().String("doc", "", "Path of the verified document — scopes the verify state and findings ledger to it, and snapshots the revision for the next verify-focus diff")
	recipeLogCmd.Flags().String("scope", "full", "Verification scope of this round: full (whole document) or delta (changed sections + reference closure). Only a full pass may satisfy the completion gate.")
	// Changelog flags
	recipeLogCmd.Flags().String("action", "", "Action type (research, improve, verify, debate, simulate, audit, assess, implement, test, sync, status)")
	recipeLogCmd.Flags().String("output", "", "Output file path")
	recipeLogCmd.Flags().String("based-on", "", "Dependency document path")
	recipeLogCmd.Flags().String("doc-type", "", "Manifest document type (overrides auto-detection from action)")
	recipeLogCmd.Flags().String("result", "", "Summary of outcome")
	recipeLogCmd.Flags().Int("gaps", 0, "Number of gaps found (for simulate)")
	// Phase flag
	recipeLogCmd.Flags().String("phase", "", "Update recipe phase (implement, test, sync, status, etc.)")
	recipeLogCmd.Flags().Bool("force", false, "Force protected phase transitions (complete, finalize)")
}

// actionToDocType maps changelog action names to manifest document types.
func actionToDocType(action string) string {
	switch action {
	case "research":
		return "research"
	case "draft", "improve":
		return "draft"
	case "debate":
		return "debate"
	case "simulate":
		return "simulation"
	case "verify", "audit", "assess", "sync-check":
		return "verification"
	case "implement":
		return "implementation"
	case "test":
		return "test-result"
	case "sync":
		return "deviation"
	case "adjudicate":
		return "verification"
	case "domain-model":
		return "domain"
	case "wireframe":
		return "wireframe"
	case "architect":
		return "architect-decision"
	case "discover":
		return "discover"
	case "resolve-uncertainties":
		return "verification"
	case "review":
		return "review"
	case "finalize":
		return "final"
	default:
		return action
	}
}

// checkPhasePreConditions warns about missing prerequisites for a phase transition.
// Warnings go to stderr; phase transition always proceeds (warn, not block).
func checkPhasePreConditions(root string, recipe *state.RecipeState, newPhase string, force bool) error {
	recipeDir := state.RecipeDir(root, recipe.ID)
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(recipeDir, name))
		return err == nil
	}
	stateExists := func(name string) bool {
		_, err := os.Stat(filepath.Join(state.SpecsPath(root), name))
		return err == nil
	}
	warn := func(msg string) {
		fmt.Fprintf(os.Stderr, "⚠ %s\n", msg)
	}

	switch newPhase {
	case "complete", "finalize":
		if !force {
			fmt.Fprintf(os.Stderr, "✗ Phase '%s' is protected — set automatically by completion gates.\n", newPhase)
			fmt.Fprintf(os.Stderr, "  Use --force to override (e.g., for legacy recipes).\n")
			return fmt.Errorf("phase '%s' is protected", newPhase)
		}
		fmt.Fprintf(os.Stderr, "⚠ Force-completing recipe (bypassing completion gates)\n")

	case "research":
		if recipe.Type == "blueprint" && !stateExists("project-map.md") {
			warn("project-map.md not found — scan codebase to create it")
		}

	case "wireframe":
		// domain.md is a strict precondition for wireframe in blueprint/design
		// recipes. See bts-recipe-blueprint § "Entering the Adaptive Loop".
		// The intent: wireframe components must honor the invariant owners
		// declared in domain.md, so domain.md must exist first.
		if (recipe.Type == "blueprint" || recipe.Type == "design") && !exists("domain.md") && !force {
			fmt.Fprintf(os.Stderr, "✗ domain.md not found — run /bts-domain-model before /bts-wireframe.\n")
			fmt.Fprintf(os.Stderr, "  Use --force to override (e.g., for legacy recipes).\n")
			return fmt.Errorf("domain.md required before phase 'wireframe'")
		}

	case "draft":
		if recipe.Type == "blueprint" && !exists("wireframe.md") {
			warn("wireframe.md not found — run /bts-wireframe to design structure first")
		}
		if (recipe.Type == "blueprint" || recipe.Type == "design") && !exists("domain.md") && !force {
			// Same rationale as wireframe — draft references domain entities.
			fmt.Fprintf(os.Stderr, "✗ domain.md not found — run /bts-domain-model before drafting.\n")
			fmt.Fprintf(os.Stderr, "  Use --force to override (e.g., for legacy recipes).\n")
			return fmt.Errorf("domain.md required before phase 'draft'")
		}

	case "implement":
		if !exists("final.md") {
			warn("final.md not found — complete spec before implementing")
		}

	case "test":
		if recipe.Type != "fix" && !exists("tasks.json") {
			warn("tasks.json not found — run /bts-implement to decompose tasks")
		}

	case "review":
		if exists("test-results.json") {
			data, _ := os.ReadFile(filepath.Join(recipeDir, "test-results.json"))
			var tr state.TestResults
			if json.Unmarshal(data, &tr) == nil && tr.Status != "pass" {
				warn("tests not passing — fix before review")
			}
		}
		simsDir := filepath.Join(recipeDir, "simulations")
		if entries, err := os.ReadDir(simsDir); err != nil || countNonHidden(entries) == 0 {
			warn("no code simulation found — run /bts-simulate code first")
		}

	case "sync":
		if !exists("review.md") {
			warn("review.md not found — run /bts-review first")
		}

	case "status":
		if !exists("deviation.md") {
			warn("deviation.md not found — run /bts-sync first")
		}
	}

	return nil
}

func countNonHidden(entries []os.DirEntry) int {
	count := 0
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			count++
		}
	}
	return count
}

// agentEvidenceSince classifies whether a subagent finished between the
// previous recorded round for this document and now. Returns "" when the
// signal cannot be read at all, so an unreadable metrics log records no
// claim rather than a false "none".
func agentEvidenceSince(root, recipeID string, priorRounds []state.VerifyLogEntry) string {
	var since time.Time
	for i := len(priorRounds) - 1; i >= 0; i-- {
		if ts, err := time.Parse(time.RFC3339, priorRounds[i].Timestamp); err == nil {
			since = ts
			break
		}
	}
	active, ok := metrics.SubagentActivitySince(root, recipeID, since)
	if !ok {
		return ""
	}
	if active {
		return state.AgentEvidenceObserved
	}
	return state.AgentEvidenceNone
}

// stampContentHashes records the content identity of what this round
// verified: the document itself, and the recipe's verification.md.
//
// verification.md is always taken from the recipe directory rather than
// from whatever path --from-verification named, because that is the file
// the unrecorded-verification gate inspects. Hashing anything else would
// leave the gate comparing against a document it never reads.
//
// A hash that cannot be computed is left empty and warned about: the
// gates fall back rather than treat a missing hash as a mismatch, so a
// read failure degrades coverage instead of manufacturing a block.
func stampContentHashes(root, recipeID, docPath string, entry *state.VerifyLogEntry) {
	if docPath != "" {
		h, ok, err := state.FileContentHash(docPath)
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "warning: hash %s: %v\n", docPath, err)
		case ok:
			entry.DocHash = h
		}
	}
	vpath := filepath.Join(state.RecipeDir(root, recipeID), "verification.md")
	h, ok, err := state.FileContentHash(vpath)
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "warning: hash verification.md: %v\n", err)
	case ok:
		entry.VerificationHash = h
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// ===== Sprint 9 P21 — `bts recipe reconcile` =========================

// reconcileEligiblePhases lists the blueprint-side phases from which
// reconcile may promote a recipe to `finalize`. Implementation-lifecycle
// phases are NEVER eligible — they have their own completion gates via
// handleImplementDone and reconciling past them would corrupt progress
// tracking. Kept in a single var so tests and the --force path share
// one source.
var reconcileEligiblePhases = map[string]bool{
	"discovery":     true,
	"scoping":       true,
	"domain-model":  true,
	"wireframe":     true,
	"architect":     true,
	"research":      true,
	"draft":         true,
	"assess":        true,
	"improve":       true,
	"verify":        true,
	"debate":        true,
	"simulate":      true,
	"audit":         true,
	"sync-check":    true, // sync-check is still blueprint-side
}

// Sentinel errors callers (and tests) match on for specific remediation.
var (
	ErrNoVerifyLog    = fmt.Errorf("no verify-log entries")
	ErrNotConverged   = fmt.Errorf("verify-log not converged")
	ErrProtectedPhase = fmt.Errorf("phase protected from reconcile")
	ErrAlreadyFinal   = fmt.Errorf("recipe already finalized")
)

// reconcileOpts carries CLI flags into the pure reconcile function so
// the function stays testable without spinning a cobra command.
type reconcileOpts struct {
	dryRun bool
	force  bool
}

// reconcilePlan is what the command prints (and, unless dryRun, writes).
// Callers inspect it to show a before/after diff.
type reconcilePlan struct {
	FromPhase     string
	FromLevel     float64
	FromIteration int
	ToPhase       string
	ToLevel       float64
	ToIteration   int
	Reason        string
}

var recipeReconcileCmd = &cobra.Command{
	Use:   "reconcile [recipe-id]",
	Short: "Normalize recipe.json state from verify-log (recover from missed <bts>DONE</bts>)",
	Long: `When a session ends without the <bts>DONE</bts> marker being emitted,
the stop hook never runs and recipe.json stays in a mid-blueprint phase
(e.g. phase=simulate, level=0, iteration=0) while verify-log.jsonl
already shows 'converged'. Reconcile inspects verify-log and, when the
last entry is converged with critical=0, major=0, resolvable=0,
updates recipe.json to:

  phase     = finalize
  level     = 3.0
  iteration = max(recipe.iteration, last_verify_entry.iteration)

SAFETY:
  - Only blueprint-lifecycle phases are eligible by default (discovery
    through audit, plus sync-check). Implementation-lifecycle phases
    (implement, test, review, sync, status, complete, finalize,
    cancelled) are NEVER touched — even with --force.
  - If verify-log.jsonl is missing or its last entry is not converged,
    reconcile refuses.
  - recipe.json.bak is written before any change.
  - --dry-run prints the plan without writing.
  - --force bypasses only the blueprint-phase whitelist. It does not
    re-enable implement-phase reconciliation.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRecipeReconcile,
}

func runRecipeReconcile(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	root, err := state.FindRoot(cwd)
	if err != nil {
		return fmt.Errorf("not a bts project: %w", err)
	}

	recipeID := ""
	if len(args) == 1 {
		recipeID = args[0]
	} else {
		active, _ := state.GetActiveRecipe(root)
		if active == nil {
			return fmt.Errorf("no active recipe and no id given")
		}
		recipeID = active.ID
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")

	plan, err := reconcileRecipe(root, recipeID, reconcileOpts{dryRun: dryRun, force: force})
	if err != nil {
		return err
	}

	verb := "Reconciled"
	if dryRun {
		verb = "DRY RUN — would update"
	}
	fmt.Printf("%s %s:\n", verb, recipeID)
	fmt.Printf("  phase:     %s → %s\n", plan.FromPhase, plan.ToPhase)
	fmt.Printf("  level:     %.1f → %.1f\n", plan.FromLevel, plan.ToLevel)
	fmt.Printf("  iteration: %d → %d\n", plan.FromIteration, plan.ToIteration)
	fmt.Printf("  reason:    %s\n", plan.Reason)
	if !dryRun {
		fmt.Printf("  backup:    recipe.json.bak\n")
	}
	return nil
}

// reconcileRecipe is the pure function driving the CLI. Separated so
// unit tests can drive it without setting up a cobra invocation.
func reconcileRecipe(root, recipeID string, opts reconcileOpts) (*reconcilePlan, error) {
	recipe, err := state.LoadRecipeState(root, recipeID)
	if err != nil {
		return nil, fmt.Errorf("load recipe: %w", err)
	}

	// Already-final short-circuit. Force cannot re-finalize (nothing
	// would change) — this is a refusal even with --force because it
	// signals operator confusion.
	if recipe.Phase == "finalize" || recipe.Phase == "complete" {
		return nil, fmt.Errorf("%w: recipe already in phase %q", ErrAlreadyFinal, recipe.Phase)
	}

	// Implement-lifecycle phases are NEVER reconciled. This is the
	// hardcoded safety — --force does not bypass. handleImplementDone
	// owns these phases; reconcile touching them would corrupt tasks/
	// test/sync progress tracking.
	if state.IsImplementPhase(recipe.Phase) {
		return nil, fmt.Errorf("%w: recipe is in implement-lifecycle phase %q — reconcile is blueprint-only",
			ErrProtectedPhase, recipe.Phase)
	}

	// Blueprint-phase whitelist. --force bypasses.
	if !opts.force && !reconcileEligiblePhases[recipe.Phase] {
		return nil, fmt.Errorf("%w: phase %q not in reconcile-eligible set; use --force to override",
			ErrProtectedPhase, recipe.Phase)
	}

	lastEntry, err := state.LastVerifyEntry(root, recipeID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoVerifyLog, err)
	}

	// Convergence pre-check mirrors handleSpecDone's gate. If any
	// count is non-zero or status is not "converged", reconcile
	// refuses.
	resolvable := lastEntry.EffectiveResolvable()
	if lastEntry.Critical > 0 || lastEntry.Major > 0 || resolvable > 0 ||
		lastEntry.Status != "converged" {
		return nil, fmt.Errorf("%w: last entry has critical=%d major=%d resolvable=%d status=%s",
			ErrNotConverged, lastEntry.Critical, lastEntry.Major, resolvable, lastEntry.Status)
	}

	nextIter := recipe.Iteration
	if lastEntry.Iteration > nextIter {
		nextIter = lastEntry.Iteration
	}
	plan := &reconcilePlan{
		FromPhase:     recipe.Phase,
		FromLevel:     recipe.Level,
		FromIteration: recipe.Iteration,
		ToPhase:       "finalize",
		ToLevel:       3.0,
		ToIteration:   nextIter,
		Reason:        fmt.Sprintf("verify-log last entry (iter=%d) is converged", lastEntry.Iteration),
	}

	if opts.dryRun {
		return plan, nil
	}

	// Backup recipe.json before overwrite. Non-fatal if no existing
	// file (brand-new recipes won't have one) but any IO error stops us.
	recipePath := filepath.Join(state.RecipeDir(root, recipeID), "recipe.json")
	if err := backupRecipeJSON(recipePath); err != nil {
		return nil, fmt.Errorf("backup recipe.json: %w", err)
	}

	recipe.Phase = plan.ToPhase
	recipe.Level = plan.ToLevel
	recipe.Iteration = plan.ToIteration
	if err := state.SaveRecipeState(root, recipe); err != nil {
		return nil, fmt.Errorf("save: %w", err)
	}

	// Audit: emit a metrics event + a changelog entry so operators can
	// trace post-hoc which reconciles fired and why.
	_ = metrics.Append(root, &metrics.MetricsEvent{
		Kind:          metrics.KindPhaseChange,
		RecipeID:      recipeID,
		Phase:         plan.ToPhase,
		PreviousPhase: plan.FromPhase,
	})
	_ = state.AppendChangelog(root, recipeID, &state.ChangelogEntry{
		Action: "finalize",
		Result: fmt.Sprintf(
			"reconciled from phase=%s level=%.1f iter=%d (force=%v)",
			plan.FromPhase, plan.FromLevel, plan.FromIteration, opts.force,
		),
	})
	return plan, nil
}

func backupRecipeJSON(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.WriteFile(path+".bak", data, 0644)
}
