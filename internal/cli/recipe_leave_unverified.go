package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	recipeCmd.AddCommand(recipeLeaveUnverifiedCmd)
	recipeLeaveUnverifiedCmd.Flags().String("doc", "", "The document left unverified (basename or path; required)")
	recipeLeaveUnverifiedCmd.Flags().String("reason", "", "Why it stays unverified for now (required)")
}

var recipeLeaveUnverifiedCmd = &cobra.Command{
	Use:   "leave-unverified [recipe-id] --doc <doc> --reason \"...\"",
	Short: "Record that a document changed since its verification stays unverified for now, and why",
	Long: `Rule 3 blocks a turn end while a verified document has changed since its
last verification.  When it must stay unverified for a while, for example
because another agent is about to rewrite part of it and a round now would
be spent twice, record that here and tell the user why.

The Stop hook then lets the turn end, naming the document and the reason,
until the document, or a document its review read, changes again, or a
round is logged for it.  Nothing else changes: <bts>DONE</bts> still needs
the verify round.`,
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
		reason, _ := cmd.Flags().GetString("reason")
		rec, err := leaveUnverified(root, recipeID, doc, reason, os.Getenv("CLAUDE_CODE_SESSION_ID"))
		if err != nil {
			return err
		}
		fmt.Printf("%s left unverified: %s\n"+
			"The Stop hook lets the turn end until %s or a document its review read changes again, or a round is logged for it.  "+
			"<bts>DONE</bts> still needs the round.  Tell the user.\n", rec.Doc, rec.Reason, rec.Doc)
		return nil
	},
}

// leaveUnverified validates and records a leave-unverified request: the
// document must be one rule 3 holds stale now, and the reason non-empty.
func leaveUnverified(root, recipeID, doc, reason, sessionID string) (state.LeftUnverified, error) {
	reason = strings.TrimSpace(reason)
	if strings.TrimSpace(doc) == "" || reason == "" {
		return state.LeftUnverified{}, fmt.Errorf("--doc and --reason are both required")
	}
	base := filepath.Base(doc)
	dirty, err := state.DirtyVerifiedDocs(root, recipeID)
	if err != nil {
		return state.LeftUnverified{}, fmt.Errorf("read the verify log: %w", err)
	}
	if !slices.Contains(dirty, base) {
		return state.LeftUnverified{}, fmt.Errorf("%s has not changed since its last verification, so there is nothing to leave unverified", base)
	}
	return state.RecordLeftUnverified(root, recipeID, base, reason, sessionID)
}
