package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/imtemp-dev/claude-bts/internal/engine"
	"github.com/imtemp-dev/claude-bts/internal/state"
	"github.com/spf13/cobra"
)

// bts check generated: rerun the generators the wireframe's `generate` rows
// name and record what they wrote, so IMPLEMENT DONE can require generated
// files that match the code (gate generated_current, trial problem #66).
//
// The commands come from the wireframe, not from a --cmd flag: a flag would
// let any command that leaves the files alone (`true`) pass the check, and
// the wireframe is the reviewed record of which generator writes what.

func init() {
	checkCmd.AddCommand(checkGeneratedCmd)
	checkGeneratedCmd.Flags().Int("timeout", 600, "Seconds before each generator run is killed")
}

var checkGeneratedCmd = &cobra.Command{
	Use:   "generated <recipe-id>",
	Short: "Rerun the generators the wireframe's generate rows name and record what they wrote",
	Long: `Runs each distinct generator named by a ` + "`generate`" + ` row of wireframe.md's
File Structure table (` + "`generator: `" + ` and the command in a code span, in the
row's Responsibility cell), in row order at the project root, and records the
runs, the code hash and a fingerprint of the generated files in
build-evidence.json.

IMPLEMENT DONE requires a passing run on the current code whose generated
files are unchanged since. Run it after the last code change; it lists the
files the generators rewrote, so their diff can be reviewed.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := projectRoot()
		if err != nil {
			return err
		}
		timeout, _ := cmd.Flags().GetInt("timeout")
		g, err := executeGeneratedCheck(root, args[0], timeout, os.Stdout)
		if err != nil {
			return err
		}
		fmt.Println("\n[bts] generators:")
		for _, r := range g.Runs {
			fmt.Printf("  exit=%-3d %s\n", r.ExitCode, r.Command)
		}
		if len(g.Changed) > 0 {
			fmt.Printf("[bts] rewrote: %s (review the diff: the code they come from changed)\n", strings.Join(g.Changed, ", "))
		}
		if len(g.Missing) > 0 {
			fmt.Printf("[bts] not written: %s\n", strings.Join(g.Missing, ", "))
		}
		if !g.Passed() {
			fmt.Println("[bts] generated check failed → build-evidence.json")
			os.Exit(1)
		}
		fmt.Printf("[bts] generated check recorded: %d file(s) current → build-evidence.json\n", len(g.Files))
		return nil
	},
}

func executeGeneratedCheck(root, recipeID string, timeoutSec int, sink io.Writer) (*state.GeneratedRun, error) {
	if _, err := state.LoadRecipeState(root, recipeID); err != nil {
		return nil, fmt.Errorf("load recipe %s: %w", recipeID, err)
	}
	rows := engine.WireframeGenerateRows(root, recipeID)
	if len(rows) == 0 {
		return nil, errors.New("wireframe.md's File Structure has no `generate` rows, so there is nothing to regenerate")
	}
	if bare := engine.GenerateRowsWithoutGenerator(rows); len(bare) > 0 {
		return nil, fmt.Errorf("generate row(s) %s name no generator: write `generator:` and the command, in backticks, in the row's Responsibility cell (bts-wireframe Step 4)",
			strings.Join(bare, ", "))
	}
	paths := engine.GenerateRowPaths(rows)
	before, _, err := engine.GeneratedFiles(root, paths)
	if err != nil {
		return nil, err
	}

	g := &state.GeneratedRun{Files: paths}
	for i, c := range engine.GenerateRowCommands(rows) {
		fmt.Fprintf(sink, "\n[bts] generator: %s\n", c)
		run, err := runRecorded(root, recipeID, c, timeoutSec, fmt.Sprintf("generated-%d.log", i+1), sink)
		if err != nil {
			return nil, err
		}
		g.Runs = append(g.Runs, run)
		g.CodeHash = run.CodeHash
		// A later generator usually reads an earlier one's output (types
		// from the OpenAPI document), so a failure stops the chain.
		if !run.Passed() {
			break
		}
	}

	after, filesHash, err := engine.GeneratedFiles(root, paths)
	if err != nil {
		return nil, err
	}
	g.FilesHash = filesHash
	for _, p := range paths {
		switch {
		case after[p] == "absent":
			g.Missing = append(g.Missing, p)
		case after[p] != before[p]:
			g.Changed = append(g.Changed, p)
		}
	}
	g.RunAt = time.Now().UTC().Format(time.RFC3339)

	ev, err := state.LoadBuildEvidence(root, recipeID)
	if err != nil {
		return nil, fmt.Errorf("load build evidence: %w", err)
	}
	ev.Generated = g
	if err := state.SaveBuildEvidence(root, recipeID, ev); err != nil {
		return nil, fmt.Errorf("save build evidence: %w", err)
	}
	result := fmt.Sprintf("%d generator(s), passed=%t, rewrote %d of %d", len(g.Runs), g.Passed(), len(g.Changed), len(paths))
	appendEvidenceChangelog(root, recipeID, "generated", result, g.CodeHash)
	return g, nil
}
