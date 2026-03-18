package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jlim/bts/internal/engine"
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

	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}

	if !filepath.IsAbs(docPath) {
		docPath = filepath.Join(projectRoot, docPath)
	}

	// If --no-code, skip code reference checks
	root := projectRoot
	if noCode {
		root = ""
	}

	result, err := engine.VerifyDocument(docPath, root)
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
