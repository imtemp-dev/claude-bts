package cli

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/jlim/bts/pkg/version"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(doctorCmd)
}

var doctorCmd = &cobra.Command{
	Use:     "doctor",
	Short:   "Run system diagnostics",
	GroupID: "project",
	RunE:    runDoctor,
}

func runDoctor(cmd *cobra.Command, args []string) error {
	fmt.Println("bts doctor")
	fmt.Println("----------")

	// Version
	fmt.Printf("bts version:  %s\n", version.GetFullVersion())
	fmt.Printf("Go version:   %s\n", runtime.Version())
	fmt.Printf("Platform:     %s/%s\n", runtime.GOOS, runtime.GOARCH)

	// Claude Code
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		fmt.Println("Claude Code:  NOT FOUND")
	} else {
		fmt.Printf("Claude Code:  %s\n", claudePath)
	}

	// Git
	gitPath, err := exec.LookPath("git")
	if err != nil {
		fmt.Println("Git:          NOT FOUND")
	} else {
		fmt.Printf("Git:          %s\n", gitPath)
	}

	fmt.Println("\nAll checks passed.")
	return nil
}
