package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/imtemp-dev/jig/internal/engine"
	"github.com/imtemp-dev/jig/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(verifyCmd)
	verifyCmd.Flags().Bool("no-code", false, "Skip code reference checks (for from-scratch specs)")
}

var verifyCmd = &cobra.Command{
	Use:     "verify <file>",
	Short:   "Check document consistency, assess level, and verify references",
	Args:    cobra.ExactArgs(1),
	GroupID: "tools",
	RunE:    runVerify,
}

func runVerify(cmd *cobra.Command, args []string) error {
	docPath := args[0]
	noCode, _ := cmd.Flags().GetBool("no-code")

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}

	if !filepath.IsAbs(docPath) {
		docPath = filepath.Join(cwd, docPath)
	}

	// Settings belong to the PROJECT, not to wherever the command was
	// typed. Reading them from the cwd meant `jig verify` run from a
	// subdirectory silently fell back to built-in defaults, so
	// verify.max_section_lines only applied when the operator happened
	// to be standing in the project root.
	projectRoot := cwd
	if r, rerr := state.FindRoot(cwd); rerr == nil {
		projectRoot = r
	}

	result, err := engine.VerifyDocument(docPath, projectRoot, !noCode)
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}

	output, err := engine.FormatResult(result)
	if err != nil {
		return fmt.Errorf("format: %w", err)
	}

	fmt.Println(output)

	if result.Summary.Critical > 0 || result.Summary.Major > 0 {
		os.Exit(1)
	}

	return nil
}
